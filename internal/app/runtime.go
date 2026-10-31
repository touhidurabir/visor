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
	"github.com/touhidurabir/visor/internal/settings"
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
	// exitReasonTimeout bounds reading supervisord's main log for why a
	// process exited. The tracker waits on it, so it is short.
	exitReasonTimeout = time.Second
	// exitLogTail is how much of the main log's end is read for exit lines.
	exitLogTail = 64 << 10
	// exitLineSlack is how far an exit line's time may be from the
	// process's stop time (whole seconds) and still be that exit.
	exitLineSlack = 2 * time.Second
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
	// ConfigPath is supervisord.conf from VISOR_CONFIG. It overrides
	// Settings.ConfigPath; "" uses the setting (and "" there auto-discovers).
	ConfigPath string
	StateDir   string // where settings, issues, history, loaded.json and watch.json live
	// Settings are the saved preferences; the zero value means the defaults.
	Settings settings.Settings
	// Interval, when > 0, replaces Settings.PollInterval until settings are
	// next applied, without its limits (tests poll faster than 1s).
	Interval time.Duration
	// Keychain holds the supervisord password for a username saved in
	// Settings. nil means no saved password (tests never use the real one).
	Keychain settings.Keychain
	// Emit sends an event to the frontend; nil drops events. WailsEmit in
	// the app, a recorder in tests.
	Emit func(name string, data any)
}

// reconnectWait bounds how long Reconnect waits for the first poll.
const reconnectWait = 5 * time.Second

// Runtime wires discovery, the supervisord client, the monitor, the issue
// tracker and the config watcher together, and turns every monitor update
// into frontend events. Services share one Runtime.
//
// A connection (client, monitor, watcher and their goroutines) is rebuilt
// by Reconnect and by settings that change how Visor connects. The issue
// store and tracker live as long as the Runtime.
//
// Locking: life serialises Start, Stop, Reconnect and ApplySettings, and is
// never held while waiting on a poll. mu is a leaf lock. It is never held
// while calling the tracker, the watcher, the monitor or supervisord,
// because the tracker calls back into the Runtime (configOf, expectedExit,
// autorestart, exitReason) under its own lock.
type Runtime struct {
	opts  RuntimeOptions
	watch *watchStore

	life    sync.Mutex
	started bool
	stopped bool
	parent  context.Context

	mu       sync.Mutex
	prefs    settings.Settings
	cancel   context.CancelFunc // ends the current connection
	ctx      context.Context    // the current connection's context
	first    chan struct{}      // closed when the current connection's first poll is handled
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
	prefs := o.Settings
	if prefs == (settings.Settings{}) {
		prefs = settings.Defaults()
	}
	prefs = prefs.Clamp()
	if o.Interval > 0 {
		prefs.PollInterval = o.Interval
	}
	return &Runtime{opts: o, prefs: prefs, watch: newWatchStore(o.StateDir), ctx: context.Background()}
}

// Start opens the issue store, connects and starts polling. A missing
// config or socket is not an error: Overview reports it, so the app can
// still open and explain the fix. Start may be called once; later calls do
// nothing.
func (r *Runtime) Start(ctx context.Context) error {
	r.life.Lock()
	defer r.life.Unlock()
	if r.started {
		return nil
	}
	r.started, r.parent = true, ctx
	r.openStore()
	r.connect()
	return nil
}

// Stop ends the connection and waits for every goroutine to end. It is
// safe to call more than once, and before Start. Reconnect does nothing
// afterwards.
func (r *Runtime) Stop() {
	r.life.Lock()
	defer r.life.Unlock()
	r.stopped = true
	r.disconnect()
}

// Reconnect drops the connection and builds a new one from the current
// settings: it re-discovers the config, reads the Keychain password, makes
// a new client and monitor, and emits a fresh overview. It then waits for
// the first poll (up to reconnectWait), so the overview it leaves behind
// says whether supervisord answered.
func (r *Runtime) Reconnect() error {
	r.life.Lock()
	if err := r.reconnectLocked(); err != nil {
		r.life.Unlock()
		return err
	}
	r.mu.Lock()
	first, ctx := r.first, r.ctx
	r.mu.Unlock()
	r.life.Unlock()

	r.emitOverview()
	t := time.NewTimer(reconnectWait)
	defer t.Stop()
	select {
	case <-first:
	case <-ctx.Done():
	case <-t.C:
	}
	return nil
}

// reconnectLocked rebuilds the connection. Caller holds life.
func (r *Runtime) reconnectLocked() error {
	switch {
	case r.stopped:
		return errors.New("cannot reconnect: Visor is quitting")
	case !r.started:
		return errors.New("cannot reconnect: Visor has not started yet")
	}
	r.disconnect()
	r.openStore() // retry a store that failed to open
	r.connect()
	return nil
}

// Settings returns the settings in use.
func (r *Runtime) Settings() settings.Settings {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.prefs
}

// ApplySettings puts s (clamped) into use. A change to how Visor connects
// (the config path or username), or reconnect, rebuilds the connection as
// Reconnect does; the poll interval, crash-loop thresholds and retention
// apply in place. Before Start, s is only kept for Start.
func (r *Runtime) ApplySettings(s settings.Settings, reconnect bool) error {
	s = s.Clamp()
	r.life.Lock()
	r.mu.Lock()
	old := r.prefs
	r.prefs = s
	mon, tracker, store := r.mon, r.tracker, r.store
	r.mu.Unlock()
	running := r.started && !r.stopped
	connChanged := r.configPathOf(old) != r.configPathOf(s) || old.Username != s.Username
	if !running || (!reconnect && !connChanged) {
		defer r.life.Unlock()
		if !running {
			return nil
		}
		if mon != nil {
			mon.SetInterval(s.PollInterval)
		}
		if tracker != nil {
			tracker.SetRules(rulesOf(s))
		}
		if store != nil && old.RetentionDays != s.RetentionDays {
			if err := store.Compact(s.Retention()); err != nil {
				return fmt.Errorf("saved, but cannot clear issues older than %d days: %w", s.RetentionDays, err)
			}
		}
		return nil
	}
	r.life.Unlock()
	return r.Reconnect()
}

// configPathOf is the config Visor uses with s: VISOR_CONFIG wins.
func (r *Runtime) configPathOf(s settings.Settings) string {
	if r.opts.ConfigPath != "" {
		return r.opts.ConfigPath
	}
	return s.ConfigPath
}

// Connection is the config in use (or, before discovery succeeds, the one
// asked for) and supervisord's address from it ("" if it has none).
func (r *Runtime) Connection() (configPath, address string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	configPath = r.d.ConfigPath
	if configPath == "" {
		configPath = r.configPathOf(r.prefs)
	}
	return configPath, transportAddress(r.d.Transport)
}

// transportAddress describes where t connects: a socket path or a URL.
func transportAddress(t supervisor.Transport) string {
	switch t := t.(type) {
	case nil:
		return ""
	case supervisor.UnixTransport:
		return t.Path
	case supervisor.TCPTransport:
		return "http://" + t.Addr
	}
	return t.URL()
}

func rulesOf(s settings.Settings) issues.Rules {
	r := issues.DefaultRules
	r.CrashLoopRestarts, r.CrashLoopWindow = s.CrashLoop.Restarts, s.CrashLoop.Window
	return r
}

// openStore opens the issue store if it is not open. Caller holds life.
func (r *Runtime) openStore() {
	r.mu.Lock()
	open, retention := r.store != nil, r.prefs.Retention()
	r.mu.Unlock()
	if open {
		return
	}
	store, err := issues.OpenStore(r.opts.StateDir, retention)
	var tracker *issues.Tracker
	if err != nil {
		err = fmt.Errorf("cannot open the issue history in %s: %w", r.opts.StateDir, err)
	} else {
		// One tracker for the Runtime's life, like the store: what it
		// remembers (user actions in flight, a user's shutdown, restarts
		// counted toward a crash loop, who closed what) must outlive a
		// reconnect, and a Resolve racing a rebuild must not land in a
		// tracker that is about to be dropped.
		r.mu.Lock()
		rules := rulesOf(r.prefs)
		r.mu.Unlock()
		tracker = issues.NewTracker(store, rules, issues.Programs{
			ConfigOf:     r.configOf,
			ExpectedExit: r.expectedExit,
			Autorestart:  r.autorestart,
			ExitReason:   r.exitReason,
		}, time.Now)
	}
	r.mu.Lock()
	r.store, r.tracker, r.storeErr = store, tracker, err
	r.mu.Unlock()
}

// connect builds a connection from the current settings and starts
// polling. Caller holds life, after disconnect (or at Start).
func (r *Runtime) connect() {
	r.mu.Lock()
	prefs, tracker := r.prefs, r.tracker
	ctx, cancel := context.WithCancel(r.parent)
	first := make(chan struct{})
	r.ctx, r.cancel, r.first = ctx, cancel, first
	r.mu.Unlock()

	fail := func(d config.Discovery, err error) {
		r.mu.Lock()
		r.d, r.connErr = d, err
		r.mu.Unlock()
		close(first) // no poll will come
	}
	d, err := config.Resolve(r.configPathOf(prefs))
	if err != nil {
		fail(d, err)
		return
	}
	if d.Transport == nil {
		fail(d, fmt.Errorf("%s defines no socket or port: add an [unix_http_server] section with file= set", d.ConfigPath))
		return
	}
	savedPass := ""
	if prefs.Username != "" && r.opts.Keychain != nil {
		if savedPass, err = settings.Password(r.opts.Keychain, d.ConfigPath); err != nil {
			fail(d, fmt.Errorf("%w. Allow Visor to use the Keychain item, or enter the password again in Settings", err))
			return
		}
	}
	var copts []supervisor.Option
	if user, pass := settings.Credentials(prefs.Username, savedPass, d.Username, d.Password); user != "" {
		copts = append(copts, supervisor.WithBasicAuth(user, pass))
	}
	client := supervisor.New(d.Transport, copts...)
	inv, _ := config.LoadInventory(d)
	watcher := config.NewWatcher(d, r.opts.StateDir)
	if tracker != nil {
		tracker.SetRules(rulesOf(prefs))
	}
	mon := monitor.New(client, prefs.PollInterval)

	r.mu.Lock()
	r.d, r.client, r.inv, r.watcher, r.mon = d, client, inv, watcher, mon
	r.mu.Unlock()

	updates, unsubscribe := mon.Subscribe()
	r.wg.Add(2)
	go func() {
		defer r.wg.Done()
		mon.Run(ctx)
	}()
	go func() {
		defer r.wg.Done()
		defer unsubscribe()
		once := sync.OnceFunc(func() { close(first) })
		defer once()             // Run ended before a poll was handled
		for u := range updates { // closed when Run returns
			r.handle(u)
			once()
		}
	}()
}

// disconnect ends the current connection, waits for its goroutines and
// forgets everything learned from it. Caller holds life.
func (r *Runtime) disconnect() {
	r.mu.Lock()
	cancel, client := r.cancel, r.client
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	r.wg.Wait()
	if client != nil {
		client.Close()
	}
	r.mu.Lock()
	r.cancel, r.client, r.mon, r.watcher = nil, nil, nil, nil // the tracker and store stay
	r.d, r.connErr = config.Discovery{}, nil
	r.inv, r.changes, r.last = config.Inventory{}, config.ChangeSet{}, monitor.Update{}
	r.version, r.failedPolls = "", 0
	r.mu.Unlock()
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

// program returns the [program:x] behind a process full name.
func (r *Runtime) program(process string) (config.Program, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, p, ok := r.inv.Lookup(process)
	return p, ok
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

// exitReason reads why process last exited from supervisord's main log.
// The tracker calls it while applying the update in r.last.
func (r *Runtime) exitReason(_ string, info supervisor.ProcessInfo) (issues.ExitReason, bool) {
	r.mu.Lock()
	base, client, procs := r.ctx, r.client, r.last.Snapshot.Processes
	r.mu.Unlock()
	if client == nil {
		return issues.ExitReason{}, false
	}
	ctx, cancel := context.WithTimeout(base, exitReasonTimeout)
	defer cancel()
	lines, err := client.RecentExits(ctx, exitLogTail)
	if err != nil {
		return issues.ExitReason{}, false
	}
	return exitReasonFrom(lines, info, procs)
}

// exitReasonFrom picks info's last exit from the main log's exit lines:
// the latest line for its name within exitLineSlack of its stop time. Exit
// lines name no group, so a name that procs has in two groups is never
// matched.
func exitReasonFrom(lines []supervisor.ExitLine, info supervisor.ProcessInfo, procs []supervisor.ProcessInfo) (issues.ExitReason, bool) {
	if info.Stop.IsZero() {
		return issues.ExitReason{}, false
	}
	for _, p := range procs {
		if p.Name == info.Name && p.Group != info.Group {
			return issues.ExitReason{}, false
		}
	}
	for _, l := range slices.Backward(lines) {
		if d := l.At.Sub(info.Stop); l.Name == info.Name && d.Abs() <= exitLineSlack {
			return issues.ExitReason{Desc: l.Desc, Status: l.Status, Expected: l.Expected}, true
		}
	}
	return issues.ExitReason{}, false
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
		in.configPath = r.configPathOf(r.prefs)
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

// errReconnecting replaces the bare "context canceled" of a call cut short
// because its connection was rebuilt (Reconnect or a settings change).
var errReconnecting = errors.New("reconnecting to supervisord: try again in a moment")

// reconnecting maps err to errReconnecting when the connection it ran on
// (conn, the context from Runtime.conn) has ended.
func reconnecting(conn context.Context, err error) error {
	if err != nil && conn.Err() != nil {
		return errReconnecting
	}
	return err
}

// actionGrace is how long after a user action ends the changes it caused
// are still the user's: two polls, at least minActionGrace (or e's own
// minimum, e.g. minRestartGrace).
func (r *Runtime) actionGrace(e expectation) time.Duration {
	r.mu.Lock()
	interval := r.prefs.PollInterval
	if r.mon != nil {
		interval = r.mon.Interval()
	}
	r.mu.Unlock()
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
	err = reconnecting(ac.ctx, do(ctx, ac.client))
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
