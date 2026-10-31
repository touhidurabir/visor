package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/touhidurabir/visor/internal/config"
	"github.com/touhidurabir/visor/internal/issues"
	"github.com/touhidurabir/visor/internal/monitor"
	"github.com/touhidurabir/visor/internal/supervisor"
)

// Events sent to the frontend.
const (
	// EventOverview carries an OverviewView after every poll and action.
	EventOverview = "visor:overview"
	// EventIssueOpened carries an IssueView for each newly opened issue.
	EventIssueOpened = "visor:issue-opened"
)

func init() {
	application.RegisterEvent[OverviewView](EventOverview)
	application.RegisterEvent[IssueView](EventIssueOpened)
}

const (
	// issueRetention is how long closed issues and history are kept.
	issueRetention = 90 * 24 * time.Hour
	// actionTimeout bounds one user action, including waiting for
	// processes to start or stop.
	actionTimeout = 2 * time.Minute
	// minActionGrace is the least time after a user action during which
	// the changes it caused are still taken as the user's.
	minActionGrace = 5 * time.Second
	// minRestartGrace is the same for a supervisord restart, which stops
	// every process (up to each one's stopwaitsecs) before coming back.
	minRestartGrace = 30 * time.Second
	// rpcTimeout bounds quick calls made outside the poll loop.
	rpcTimeout = 5 * time.Second
)

// StateDir is where Visor keeps its state: $VISOR_STATE_DIR if set,
// otherwise ~/Library/Application Support/Visor.
func StateDir() (string, error) {
	if d := os.Getenv("VISOR_STATE_DIR"); d != "" {
		return d, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find the state folder: %w", err)
	}
	return filepath.Join(base, "Visor"), nil
}

// WailsEmit sends an event to every window of the running Wails app.
func WailsEmit(name string, data any) {
	if a := application.Get(); a != nil {
		a.Event.Emit(name, data)
	}
}

// RuntimeOptions configures a Runtime.
type RuntimeOptions struct {
	ConfigPath string        // supervisord.conf; "" auto-discovers
	StateDir   string        // where issues, history, loaded.json and watch.json live
	Interval   time.Duration // poll interval; <= 0 means the monitor's default
	// Emit sends an event to the frontend; nil drops events. WailsEmit in
	// the app, a recorder in tests.
	Emit func(name string, data any)
}

// Runtime wires discovery, the supervisord client, the monitor, the issue
// tracker and the config watcher together, and turns every monitor update
// into frontend events. Services share one Runtime.
//
// Locking: mu is a leaf lock. It is never held while calling the tracker,
// the watcher, the monitor or supervisord, because the tracker calls back
// into the Runtime (configOf, expectedExit, autorestart) under its own lock.
type Runtime struct {
	opts  RuntimeOptions
	watch *watchStore

	mu       sync.Mutex
	started  bool
	cancel   context.CancelFunc
	ctx      context.Context
	d        config.Discovery
	connErr  error // no usable connection: no config, or no socket
	storeErr error
	client   *supervisor.Client
	mon      *monitor.Monitor
	store    *issues.Store
	tracker  *issues.Tracker
	watcher  *config.Watcher
	inv      config.Inventory
	changes  config.ChangeSet
	last     monitor.Update
	version  string
	// failedPolls counts consecutive unreachable polls, for health.
	failedPolls int

	wg sync.WaitGroup
}

// NewRuntime returns a Runtime that does nothing until Start.
func NewRuntime(o RuntimeOptions) *Runtime {
	if o.Emit == nil {
		o.Emit = func(string, any) {}
	}
	return &Runtime{opts: o, watch: newWatchStore(o.StateDir), ctx: context.Background()}
}

// Start connects and starts polling. A missing config or socket is not an
// error: Overview reports it, so the app can still open and explain the
// fix. Start may be called once; later calls do nothing.
func (r *Runtime) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started {
		return nil
	}
	r.started = true
	r.ctx, r.cancel = context.WithCancel(ctx)

	r.store, r.storeErr = issues.OpenStore(r.opts.StateDir, issueRetention)
	if r.storeErr != nil {
		r.storeErr = fmt.Errorf("cannot open the issue history in %s: %w", r.opts.StateDir, r.storeErr)
	}

	d, err := config.Resolve(r.opts.ConfigPath)
	if err != nil {
		r.connErr = err
		return nil
	}
	r.d = d
	if d.Transport == nil {
		r.connErr = fmt.Errorf("%s defines no socket or port: add an [unix_http_server] section with file= set", d.ConfigPath)
		return nil
	}
	var copts []supervisor.Option
	if d.Username != "" {
		copts = append(copts, supervisor.WithBasicAuth(d.Username, d.Password))
	}
	r.client = supervisor.New(d.Transport, copts...)
	if inv, err := config.LoadInventory(d); err == nil {
		r.inv = inv
	}
	r.watcher = config.NewWatcher(d, r.opts.StateDir)
	if r.store != nil {
		r.tracker = issues.NewTracker(r.store, issues.DefaultRules, issues.Programs{
			ConfigOf:     r.configOf,
			ExpectedExit: r.expectedExit,
			Autorestart:  r.autorestart,
		}, time.Now)
	}
	r.mon = monitor.New(r.client, r.opts.Interval)

	updates, unsubscribe := r.mon.Subscribe()
	r.wg.Add(2)
	go func() {
		defer r.wg.Done()
		r.mon.Run(r.ctx)
	}()
	go func() {
		defer r.wg.Done()
		defer unsubscribe()
		for u := range updates { // closed when Run returns
			r.handle(u)
		}
	}()
	return nil
}

// Stop cancels polling and waits for every goroutine to end. It is safe to
// call more than once, and before Start.
func (r *Runtime) Stop() {
	r.mu.Lock()
	cancel := r.cancel
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	r.wg.Wait()
}

// handle applies one monitor update and tells the frontend.
func (r *Runtime) handle(u monitor.Update) {
	r.mu.Lock()
	base, d, client, tracker, watcher := r.ctx, r.d, r.client, r.tracker, r.watcher
	needVersion := u.Snapshot.Reachable && r.version == ""
	r.mu.Unlock()

	inv, invErr := config.LoadInventory(d)
	var changes config.ChangeSet
	var changesErr error
	if watcher != nil {
		changes, changesErr = watcher.Check()
	}
	for _, ev := range u.Events {
		if ev.Kind == monitor.Reconnected { // the daemon may have been upgraded
			needVersion = u.Snapshot.Reachable
		}
	}
	version := ""
	if needVersion {
		ctx, cancel := context.WithTimeout(base, rpcTimeout)
		version, _ = client.GetSupervisorVersion(ctx)
		cancel()
	}

	r.mu.Lock()
	if invErr == nil {
		r.inv = inv
	}
	if changesErr == nil {
		r.changes = changes
	}
	if version != "" {
		r.version = version
	}
	switch {
	case u.Snapshot.Reachable:
		r.failedPolls = 0
	case !u.Snapshot.At.IsZero():
		r.failedPolls++
	}
	r.last = u
	r.mu.Unlock()

	var opened []issues.Issue
	if tracker != nil {
		opened, _ = tracker.Apply(u) // after the inventory refresh, so configOf sees new files
	}
	r.emitOverview()
	for _, is := range opened {
		r.opts.Emit(EventIssueOpened, toIssueView(is))
	}
}

func (r *Runtime) configOf(process string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if path, _, ok := r.inv.Lookup(process); ok {
		return path
	}
	if group, _, ok := strings.Cut(process, ":"); ok {
		if path, ok := r.inv.FileForGroup(group); ok {
			return path
		}
	}
	return ""
}

func (r *Runtime) expectedExit(process string) []int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.inv.ExpectedExit(process)
}

func (r *Runtime) autorestart(process string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.inv.Autorestart(process)
}

// Overview builds the dashboard view from the latest handled update.
func (r *Runtime) Overview() OverviewView {
	r.mu.Lock()
	in := overviewInput{
		now:        time.Now(),
		snap:       r.last.Snapshot,
		inv:        r.inv,
		changes:    r.changes,
		watched:    r.watch.watched,
		version:    r.version,
		configPath: r.d.ConfigPath,
		err:        r.connErr,

		failedPolls: r.failedPolls,
	}
	if in.configPath == "" {
		in.configPath = r.opts.ConfigPath
	}
	pidFile, tracker := r.d.PidFile, r.tracker
	r.mu.Unlock()

	if pidFile != "" {
		if fi, err := os.Stat(pidFile); err == nil {
			in.uptime = in.now.Sub(fi.ModTime())
		}
	}
	if tracker != nil {
		in.open = tracker.Open()
	}
	return buildOverview(in)
}

func (r *Runtime) emitOverview() { r.opts.Emit(EventOverview, r.Overview()) }

// actionConn is what a user action needs.
type actionConn struct {
	ctx     context.Context
	client  *supervisor.Client
	mon     *monitor.Monitor
	tracker *issues.Tracker // nil when the issue store could not open
}

// conn returns what an action needs, or why there is no connection.
func (r *Runtime) conn() (actionConn, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch {
	case r.connErr != nil:
		return actionConn{}, r.connErr
	case r.client == nil:
		return actionConn{}, errors.New("not connected to supervisord yet: try again in a moment")
	}
	return actionConn{r.ctx, r.client, r.mon, r.tracker}, nil
}

// actionGrace is how long after a user action ends the changes it caused
// are still the user's: two polls, at least minActionGrace (or e's own
// minimum, e.g. minRestartGrace).
func (r *Runtime) actionGrace(e expectation) time.Duration {
	interval := r.opts.Interval
	if interval <= 0 {
		interval = monitor.DefaultInterval
	}
	return max(2*interval, minActionGrace, e.minGrace)
}

// act runs a user action against supervisord, then pokes the monitor so
// the frontend sees the result straight away (even when the action failed
// part-way). What the action is expected to change is the user's doing
// (no unexpected exit, crash loop or unreachable issue) from the start of
// the action until actionGrace after it ends.
func (r *Runtime) act(e expectation, do func(ctx context.Context, c *supervisor.Client) error) error {
	ac, err := r.conn()
	if err != nil {
		return err
	}
	grace := r.actionGrace(e)
	settle := e.register(ac.tracker, time.Now().Add(actionTimeout+grace))
	ctx, cancel := context.WithTimeout(ac.ctx, actionTimeout)
	defer cancel()
	err = do(ctx, ac.client)
	switch {
	case err != nil && e.daemon:
		// supervisord refused or never got the call: an outage from now
		// on is not the user's doing.
		settle(time.Time{})
	default:
		settle(time.Now().Add(grace))
	}
	ac.mon.Poke()
	return err
}

// expectation is what a user action is about to change: processes that
// match, or the daemon itself (and with it every process). The zero value
// changes no process state (e.g. clearing logs).
type expectation struct {
	match  func(process string) bool
	daemon bool
	// untilReachable holds a daemon expectation while supervisord is down.
	untilReachable bool
	// minGrace raises actionGrace for slow actions.
	minGrace time.Duration
}

// expectProcess covers one process as supervisord names it: "group:name",
// "group:*" (the whole group), or a bare "name" (a process whose group has
// the same name).
func expectProcess(name string) expectation {
	group, proc, ok := strings.Cut(name, ":")
	switch {
	case !ok:
		full := name + ":" + name
		return expectation{match: func(p string) bool { return p == full }}
	case proc == "*":
		return expectGroups(group)
	}
	return expectation{match: func(p string) bool { return p == name }}
}

// expectGroups covers every process in the groups.
func expectGroups(groups ...string) expectation {
	return expectation{match: func(p string) bool {
		g, _, _ := strings.Cut(p, ":")
		return slices.Contains(groups, g)
	}}
}

// expectAll covers every process.
func expectAll() expectation {
	return expectation{match: func(string) bool { return true }}
}

// expectDaemonRestart covers a supervisord restart until actionGrace (at
// least minRestartGrace) after the call: a restart that has not come back
// by then is an outage.
func expectDaemonRestart() expectation {
	return expectation{daemon: true, minGrace: minRestartGrace}
}

// expectDaemonShutdown covers a supervisord shutdown for as long as it
// stays down: the user chose that outage.
func expectDaemonShutdown() expectation {
	return expectation{daemon: true, untilReachable: true}
}

// register hands e to the tracker until until and returns a func that
// moves the deadline.
func (e expectation) register(t *issues.Tracker, until time.Time) func(time.Time) {
	switch {
	case t == nil:
		return func(time.Time) {}
	case e.daemon:
		return t.ExpectDaemon(until, e.untilReachable)
	case e.match != nil:
		return t.Expect(e.match, until)
	}
	return func(time.Time) {}
}

// groupsInFile lists the process groups defined in an include file.
func (r *Runtime) groupsInFile(path string) ([]string, error) {
	r.mu.Lock()
	d := r.d
	r.mu.Unlock()
	inv, err := config.LoadInventory(d)
	if err != nil {
		return nil, err
	}
	for _, f := range inv.Files {
		if f.Path != path {
			continue
		}
		var groups []string
		seen := map[string]bool{}
		for _, p := range f.Programs {
			if !seen[p.Group] {
				seen[p.Group] = true
				groups = append(groups, p.Group)
			}
		}
		return groups, nil
	}
	return nil, fmt.Errorf("%s is not one of supervisord's config files: check the [include] files= setting", path)
}

// markAllLoaded records every include file, and every file supervisord
// had loaded before, as what supervisord now runs (after a daemon restart).
func (r *Runtime) markAllLoaded() error {
	r.mu.Lock()
	d, watcher, old := r.d, r.watcher, r.inv
	r.mu.Unlock()
	if watcher == nil {
		return nil
	}
	inv, err := config.LoadInventory(d)
	if err != nil {
		return err
	}
	var paths []string
	for _, f := range append(old.Files, inv.Files...) {
		paths = append(paths, f.Path)
	}
	return watcher.MarkLoaded(paths...)
}

// loadedSnapshot is the content Visor believes supervisord runs for path.
func (r *Runtime) loadedSnapshot(path string) ([]byte, bool) {
	r.mu.Lock()
	w := r.watcher
	r.mu.Unlock()
	if w == nil {
		return nil, false
	}
	return w.Loaded(path)
}

func (r *Runtime) issueStore() (*issues.Store, *issues.Tracker, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.store == nil {
		if r.storeErr != nil {
			return nil, nil, r.storeErr
		}
		return nil, nil, errors.New("issue history is not open yet: try again in a moment")
	}
	return r.store, r.tracker, nil
}

// watchStore persists per-config Watch toggles in stateDir/watch.json.
// Configs are watched unless turned off.
type watchStore struct {
	path string

	mu  sync.Mutex
	off map[string]bool
}

type watchFile struct {
	Version int             `json:"version"`
	Files   map[string]bool `json:"files"` // path -> watched
}

func newWatchStore(stateDir string) *watchStore {
	w := &watchStore{path: filepath.Join(stateDir, "watch.json"), off: map[string]bool{}}
	if b, err := os.ReadFile(w.path); err == nil {
		var f watchFile
		if json.Unmarshal(b, &f) == nil && f.Version == 1 {
			for p, on := range f.Files {
				if !on {
					w.off[p] = true
				}
			}
		}
	}
	return w
}

func (w *watchStore) watched(path string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return !w.off[path]
}

func (w *watchStore) set(path string, on bool) error {
	if path == "" {
		return errors.New("choose a config file to watch")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	wasOff := w.off[path]
	if on {
		delete(w.off, path)
	} else {
		w.off[path] = true
	}
	if err := w.persist(); err != nil {
		if wasOff { // keep memory in step with the file
			w.off[path] = true
		} else {
			delete(w.off, path)
		}
		return err
	}
	return nil
}

// persist writes watch.json atomically. Caller holds mu.
func (w *watchStore) persist() error {
	f := watchFile{Version: 1, Files: map[string]bool{}}
	for p := range w.off {
		f.Files[p] = false
	}
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	dir := filepath.Dir(w.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("save watch settings: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "watch-*.tmp")
	if err != nil {
		return fmt.Errorf("save watch settings: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("save watch settings: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("save watch settings: %w", err)
	}
	if err := os.Rename(tmp.Name(), w.path); err != nil {
		return fmt.Errorf("save watch settings: %w", err)
	}
	return nil
}
