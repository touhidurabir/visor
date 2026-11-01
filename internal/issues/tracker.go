package issues

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/touhidurabir/visor/internal/monitor"
	"github.com/touhidurabir/visor/internal/supervisor"
)

// ErrNotOpen is returned when resolving or dismissing an unknown or already
// closed issue.
var ErrNotOpen = errors.New("issue is not open")

const (
	fatalFallback   = "Exited too quickly (process log may have details)"
	restartedByDmn  = "Exited and was restarted by supervisord"
	noteRemoved     = "program removed"
	noteReapplied   = "config applied"
	noteByUser      = "by you"
	noteRestarted   = "Restarted by supervisord"
	noteRestartedBy = "Restarted by you"
	noteOnRequest   = "Restarted (stopped on request)"
)

// Programs answers questions about the program behind a process
// ("group:name"). A nil func means supervisord's default.
type Programs struct {
	// ConfigOf returns the config file that defines the process.
	ConfigOf func(process string) string
	// ExpectedExit returns the program's exitcodes= (default [0]).
	ExpectedExit func(process string) []int
	// Autorestart returns the program's autorestart= value as written
	// ("true", "unexpected", "no", ...); "" means the default, unexpected.
	Autorestart func(process string) string
	// ExitReasons reads supervisord's main log once and returns a lookup
	// of why processes last exited, or nil when the log cannot be read.
	// The tracker calls it at most once per Apply, on the first restart
	// of a RUNNING process that the user did not ask Visor for, and only
	// then: never once per poll. After a daemon restart every process
	// restarts in the same update, and the log is still read once. A nil
	// func never knows.
	ExitReasons func() ExitLookup
}

// ExitLookup returns why a process last exited (info is its state after
// the restart), as supervisord logged it, or false when that is not known.
type ExitLookup func(process string, info supervisor.ProcessInfo) (ExitReason, bool)

// ExitReason is why a process exited, from supervisord's main log.
type ExitReason struct {
	Desc     string // "exit status 1", "terminated by SIGSEGV (core dumped)"
	Status   int    // exit code; -1 when terminated by a signal
	Expected bool   // the code is in exitcodes= and it ran past startsecs=
	// Deliberate means supervisord stopped the process because someone
	// asked it to ("stopped:" in its log): supervisorctl restart, a stop
	// and start from another client, a daemon restart or reload. Unlike an
	// expected exit, the restart that follows is nobody's failure, so it
	// does not count toward a crash loop either.
	Deliberate bool
}

// Tracker applies monitor updates to the issue list and the history.
type Tracker struct {
	store *Store
	rules Rules
	progs Programs
	now   func() time.Time

	mu           sync.Mutex
	open         map[string]*Issue      // by ID
	runningSince map[string]time.Time   // process -> when it was first seen RUNNING
	restarts     map[string][]time.Time // process -> restart times inside the window
	userClosed   map[closedKey]time.Time
	expects      []*expectation
	failedPolls  int
	seq          int
}

// closedKey is a (process, kind) whose last issue the user closed.
type closedKey struct {
	process string
	kind    Kind
}

// expectation marks changes the user asked for. match is nil for the
// daemon itself, which covers supervisord going away and every process.
type expectation struct {
	match          func(process string) bool
	reapplied      func(process string) bool // see ExpectApply
	until          time.Time
	untilReachable bool // daemon: hold past until while supervisord is down
	sawDown        bool // daemon: supervisord was unreachable while this held
}

func (e *expectation) live(now time.Time) bool {
	return !now.After(e.until) || (e.untilReachable && e.sawDown)
}

// NewTracker loads the issues that are still open in s, and when the user
// last closed an issue for each process and kind (including closed issues
// they have since deleted).
func NewTracker(s *Store, r Rules, p Programs, now func() time.Time) *Tracker {
	t := &Tracker{
		store: s, rules: r, progs: p, now: now,
		open:         map[string]*Issue{},
		runningSince: map[string]time.Time{},
		restarts:     map[string][]time.Time{},
		userClosed:   map[closedKey]time.Time{},
	}
	if list, err := s.Issues(Filter{}); err == nil {
		for _, is := range list {
			switch is.Status {
			case StatusOpen:
				t.open[is.ID] = &is
			case StatusResolvedUser, StatusDismissed:
				t.noteUserClosed(is)
			}
		}
	}
	// Issues the user closed and then deleted still count as decided.
	if sup, err := s.suppressions(); err == nil {
		for _, x := range sup {
			t.noteUserClosed(Issue{Process: x.Process, Kind: x.Kind, ClosedAt: x.ClosedAt})
		}
	}
	return t
}

// Expect treats changes to processes that match as the user's doing until
// until: they are written to history as "by you", do not count toward a
// crash loop, and a restart opens no unexpected exit. An observed EXITED
// with an unexpected code, and FATAL, still open issues: the user starting
// a broken program is a failure. The returned func moves the deadline
// (call it when the action ends).
func (t *Tracker) Expect(match func(process string) bool, until time.Time) func(until time.Time) {
	return t.addExpectation(&expectation{match: match, until: until})
}

// ExpectApply is Expect for applying config changes. A process that
// reapplied matches is in a group being re-added with its new settings
// (stopped, removed and added again): if it goes away meanwhile, its open
// issues close noting the apply, not that the program was removed.
// reapplied may be nil.
func (t *Tracker) ExpectApply(match, reapplied func(process string) bool, until time.Time) func(until time.Time) {
	return t.addExpectation(&expectation{match: match, reapplied: reapplied, until: until})
}

// reapplying reports whether a live ExpectApply re-adds proc.
func (t *Tracker) reapplying(proc string, now time.Time) bool {
	for _, e := range t.expects {
		if e.live(now) && e.reapplied != nil && e.reapplied(proc) {
			return true
		}
	}
	return false
}

// ExpectDaemon treats supervisord going away, and every process change, as
// the user's doing until until (a restart or shutdown they asked for). With
// untilReachable (a shutdown), it also holds past until for as long as
// supervisord stays unreachable, so the outage the user chose never opens
// an unreachable issue; a restart that never comes back does, once until
// passes. The returned func moves the deadline; a zero deadline ends the
// expectation outright (the action failed).
func (t *Tracker) ExpectDaemon(until time.Time, untilReachable bool) func(until time.Time) {
	return t.addExpectation(&expectation{until: until, untilReachable: untilReachable})
}

func (t *Tracker) addExpectation(e *expectation) func(time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.expects = append(t.expects, e)
	return func(until time.Time) {
		t.mu.Lock()
		defer t.mu.Unlock()
		e.until = until
		if until.IsZero() {
			e.sawDown = false
		}
	}
}

// byUser reports whether a live expectation covers proc ("" is the daemon).
func (t *Tracker) byUser(proc string, now time.Time) bool {
	for _, e := range t.expects {
		if e.live(now) && (e.match == nil || (proc != "" && e.match(proc))) {
			return true
		}
	}
	return false
}

// Rules returns the rules in use.
func (t *Tracker) Rules() Rules {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.rules
}

// SetRules changes the rules from the next Apply on. Restarts already
// counted stay counted, judged by the new window.
func (t *Tracker) SetRules(r Rules) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rules = r
}

// Apply records the update's transitions, opens and closes issues, and
// returns what changed. Store write failures do not stop tracking.
func (t *Tracker) Apply(u monitor.Update) (opened, closed []Issue) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	t.expects = slices.DeleteFunc(t.expects, func(e *expectation) bool { return !e.live(now) })

	var trans []Transition
	restarted := map[string]bool{} // crash-loop candidates, not caused by the user
	reasons := t.lazyReasons()
	for _, ev := range u.Events {
		byUser := ev.Process != "" && t.byUser(ev.Process, now)
		switch ev.Kind {
		case monitor.StateChanged:
			// RUNNING, then STARTING or BACKOFF: it exited and supervisord
			// started it again between two polls.
			crashed := ev.From == supervisor.StateRunning && (ev.To == supervisor.StateStarting || ev.To == supervisor.StateBackoff)
			r, known := ExitReason{}, false
			if crashed && !byUser {
				r, known = reasons(ev)
			}
			deliberate := known && r.Deliberate
			note := ev.Info.SpawnErr
			switch {
			case byUser && note == "":
				note = noteByUser
			case byUser:
				note += " (" + noteByUser + ")"
			case deliberate && note == "":
				note = noteOnRequest
			}
			trans = append(trans, Transition{At: ev.At, Process: ev.Process, From: ev.From.String(), To: ev.To.String(), Note: note})
			t.trackRunning(ev.Process, ev.To, now)
			// A start from STOPPED is always someone's: supervisord never
			// starts a stopped process by itself.
			if !byUser && !deliberate && ((ev.To == supervisor.StateStarting && ev.From != supervisor.StateStopped) || crashed) {
				restarted[ev.Process] = true
			}
			if crashed && !byUser && !deliberate {
				opened = append(opened, t.restartedBySupervisord(ev, r, known, now)...)
			}
			opened = append(opened, t.entered(ev, now)...)
		case monitor.Added:
			t.trackRunning(ev.Process, ev.To, now)
			opened = append(opened, t.entered(ev, now)...)
		case monitor.Removed:
			delete(t.runningSince, ev.Process)
			delete(t.restarts, ev.Process)
			// supervisord removes only stopped processes, so one last seen
			// running was stopped between two polls: record that stop.
			switch ev.From {
			case supervisor.StateStarting, supervisor.StateRunning, supervisor.StateBackoff, supervisor.StateStopping:
				stop := noteRemoved
				if byUser {
					stop = noteByUser
				}
				trans = append(trans, Transition{At: ev.At, Process: ev.Process, From: ev.From.String(), To: supervisor.StateStopped.String(), Note: stop})
			}
			// Its fatal, unexpected exit and crash loop issues (the only
			// kinds with a process) can no longer resolve themselves.
			note := noteRemoved
			if t.reapplying(ev.Process, now) {
				note = noteReapplied
			}
			closed = append(closed, t.closeWhere(func(i *Issue) bool {
				return i.Process == ev.Process
			}, StatusResolvedAuto, note, now)...)
		case monitor.Restarted:
			crashed := !byUser && ev.From == supervisor.StateRunning && upOrStarting(ev.To)
			r, known := ExitReason{}, false
			if crashed {
				r, known = reasons(ev)
			}
			deliberate := known && r.Deliberate
			note := noteRestarted
			switch {
			case byUser:
				note = noteRestartedBy
			case deliberate:
				note = noteOnRequest
			}
			trans = append(trans, Transition{At: ev.At, Process: ev.Process, From: ev.From.String(), To: ev.To.String(), Note: note})
			delete(t.runningSince, ev.Process)
			t.trackRunning(ev.Process, ev.To, now)
			// STOPPED to STOPPED is a start and a stop between two polls:
			// someone's doing, like any start from STOPPED.
			if !byUser && !deliberate && ev.From != supervisor.StateStopped {
				restarted[ev.Process] = true
			}
			switch {
			case ev.To == supervisor.StateFatal || ev.To == supervisor.StateExited:
				opened = append(opened, t.entered(ev, now)...)
			case crashed && !deliberate:
				opened = append(opened, t.restartedBySupervisord(ev, r, known, now)...)
			}
			// Startup retries (from STARTING or BACKOFF) and STOPPED (a
			// start and stop between two polls) open nothing: FATAL and
			// the crash loop cover a program that cannot start.
		case monitor.Reconnected:
			closed = append(closed, t.closeWhere(func(i *Issue) bool { return i.Kind == KindUnreachable }, StatusResolvedAuto, "", now)...)
		}
	}
	if err := t.store.addTransitions(trans); err != nil {
		_ = err // history is best effort; issues still track
	}

	// Crash loops: at most one restart per process per update.
	for proc := range restarted {
		times := append(t.restarts[proc], now)
		cutoff := now.Add(-t.rules.CrashLoopWindow)
		times = slices.DeleteFunc(times, func(x time.Time) bool { return x.Before(cutoff) })
		t.restarts[proc] = times
		if len(times) >= t.rules.CrashLoopRestarts && !t.hasOpen(proc, KindCrashLoop) {
			msg := fmt.Sprintf("Restarted %d times in %d minutes", len(times), int(t.rules.CrashLoopWindow.Minutes()))
			opened = append(opened, t.openIssue(KindCrashLoop, proc, msg, now)...)
			t.restarts[proc] = nil
		}
	}

	// Reachability.
	if u.Snapshot.Reachable {
		t.failedPolls = 0
		// First contact after starting unreachable emits no Reconnected.
		closed = append(closed, t.closeWhere(func(i *Issue) bool { return i.Kind == KindUnreachable }, StatusResolvedAuto, "", now)...)
	} else if !u.Snapshot.At.IsZero() && !errors.Is(u.Snapshot.Err, supervisor.ErrShutdownState) {
		// SHUTDOWN_STATE is an answer: supervisord is stopping its processes
		// to restart or shut down, so it neither counts nor resets.
		t.failedPolls++
		if t.failedPolls >= t.rules.UnreachablePolls && !t.byUser("", now) {
			msg := "supervisord is not responding"
			if u.Snapshot.Err != nil {
				msg += ": " + u.Snapshot.Err.Error()
			}
			opened = append(opened, t.openIssue(KindUnreachable, "", msg, now)...)
		}
	}
	if !u.Snapshot.At.IsZero() {
		for _, e := range t.expects {
			if e.match == nil && e.live(now) {
				e.sawDown = !u.Snapshot.Reachable
			}
		}
	}

	// Reconcile RUNNING tracking with the snapshot, then close stable fatals.
	for _, p := range u.Snapshot.Processes {
		t.trackRunning(p.FullName(), p.State, now)
	}
	closed = append(closed, t.closeWhere(func(i *Issue) bool {
		since, ok := t.runningSince[i.Process]
		return i.Kind == KindFatal && ok && now.Sub(since) >= t.rules.StableFor
	}, StatusResolvedAuto, "", now)...)
	return opened, closed
}

// CloseAbsent closes, as resolved automatically with note, the open
// issues of processes not in present (a reachable snapshot), and forgets
// what it counted for them. After Visor switches to another supervisord,
// the old one's processes never show up as Removed, so their issues could
// never close themselves. Issues without a process stay.
func (t *Tracker) CloseAbsent(present []supervisor.ProcessInfo, note string) []Issue {
	t.mu.Lock()
	defer t.mu.Unlock()
	here := make(map[string]bool, len(present))
	for _, p := range present {
		here[p.FullName()] = true
	}
	for proc := range t.restarts {
		if !here[proc] {
			delete(t.restarts, proc)
		}
	}
	for proc := range t.runningSince {
		if !here[proc] {
			delete(t.runningSince, proc)
		}
	}
	return t.closeWhere(func(i *Issue) bool { return i.Process != "" && !here[i.Process] }, StatusResolvedAuto, note, t.now())
}

// Open returns the open issues, newest first (the store's order).
func (t *Tracker) Open() []Issue {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]Issue, 0, len(t.open))
	for _, i := range t.open {
		out = append(out, *i)
	}
	slices.SortFunc(out, newestFirst)
	return out
}

// Resolve closes an open issue as resolved by the user.
func (t *Tracker) Resolve(id string) error { return t.closeByID(id, StatusResolvedUser) }

// Dismiss closes an open issue without resolving it.
func (t *Tracker) Dismiss(id string) error { return t.closeByID(id, StatusDismissed) }

// ResolveMany resolves each open issue in ids like Resolve, so the same
// failure does not reopen them. IDs that are not open are skipped. It
// returns how many it resolved.
func (t *Tracker) ResolveMany(ids []string) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	closed := t.closeWhere(func(i *Issue) bool { return want[i.ID] }, StatusResolvedUser, "", t.now())
	for _, is := range closed {
		t.noteUserClosed(is)
	}
	return len(closed), nil
}

// MarkNotified records that a notification was shown for an open issue.
// It returns ErrNotOpen when the issue is unknown or already closed.
func (t *Tracker) MarkNotified(id string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	is, ok := t.open[id]
	if !ok {
		return fmt.Errorf("%w: %s", ErrNotOpen, id)
	}
	if is.Notified {
		return nil
	}
	is.Notified = true
	return t.store.putIssue(*is)
}

func (t *Tracker) closeByID(id string, st Status) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.open[id]; !ok {
		return fmt.Errorf("%w: %s", ErrNotOpen, id)
	}
	for _, is := range t.closeWhere(func(i *Issue) bool { return i.ID == id }, st, "", t.now()) {
		t.noteUserClosed(is)
	}
	return nil
}

func (t *Tracker) noteUserClosed(is Issue) {
	k := closedKey{is.Process, is.Kind}
	if is.ClosedAt.After(t.userClosed[k]) {
		t.userClosed[k] = is.ClosedAt
	}
}

// closedByUserSince reports whether the user closed a (proc, kind) issue
// after this failure began, i.e. they already decided about it. A failure
// with no known time is never treated as decided. supervisord reports
// whole seconds, so the close must fall in a later second: a failure in
// the same second as the close may have come after it.
func (t *Tracker) closedByUserSince(proc string, k Kind, info supervisor.ProcessInfo) bool {
	began := failedAt(info)
	if began.IsZero() {
		return false
	}
	return t.userClosed[closedKey{proc, k}].Truncate(time.Second).After(began)
}

// failedAt is when a failure began: the later of the last start and the
// last stop. supervisord keeps the stop time across a respawn, so a new
// start after an old stop (e.g. a spawn error) still counts as new.
func failedAt(info supervisor.ProcessInfo) time.Time {
	if info.Stop.After(info.Start) {
		return info.Stop
	}
	return info.Start
}

// entered opens issues for a process that just entered (or appeared in) a
// state, whoever caused it.
func (t *Tracker) entered(ev monitor.Event, now time.Time) []Issue {
	switch ev.To {
	case supervisor.StateFatal:
		if t.closedByUserSince(ev.Process, KindFatal, ev.Info) {
			return nil
		}
		msg := ev.Info.SpawnErr
		if msg == "" {
			msg = fatalFallback
		}
		return t.openIssue(KindFatal, ev.Process, msg, now)
	case supervisor.StateExited:
		if t.expected(ev.Process, ev.Info.ExitStatus) || t.closedByUserSince(ev.Process, KindUnexpectedExit, ev.Info) {
			return nil
		}
		return t.openIssue(KindUnexpectedExit, ev.Process, exitMessage(ev.Info.ExitStatus), now)
	}
	return nil
}

// restartedBySupervisord handles a RUNNING process that restarted without
// the user asking Visor, and not after a stop someone asked supervisord
// for. r (when known) is supervisord's main log's word on the exit: a
// planned exit (e.g. queue:work --max-time exits 0) opens nothing, a crash
// opens an issue whatever autorestart= says. When the log cannot tell,
// autorestart= decides: with unexpected, supervisord restarts only on an
// exit code not in exitcodes, so the restart proves an unexpected exit;
// with true a restart may be planned, so it only counts toward a crash
// loop; with false there is no restart to see.
func (t *Tracker) restartedBySupervisord(ev monitor.Event, r ExitReason, known bool, now time.Time) []Issue {
	if t.hasOpen(ev.Process, KindUnexpectedExit) || t.closedByUserSince(ev.Process, KindUnexpectedExit, ev.Info) {
		return nil
	}
	msg := restartedByDmn
	if known {
		if r.Expected {
			return nil
		}
		msg = restartMessage(r)
	} else if t.autorestart(ev.Process) != "unexpected" {
		return nil
	}
	at := now
	if s := ev.Info.Stop; !s.IsZero() && !s.After(now) {
		at = s // when it exited, not when Visor noticed
	}
	return t.openIssueAt(KindUnexpectedExit, ev.Process, msg, at, now)
}

// lazyReasons returns a lookup of exit reasons for one Apply: the first
// call reads supervisord's log (ExitReasons), later ones reuse what it
// read.
func (t *Tracker) lazyReasons() func(monitor.Event) (ExitReason, bool) {
	var lookup ExitLookup
	read := false
	return func(ev monitor.Event) (ExitReason, bool) {
		if !read {
			read = true
			if t.progs.ExitReasons != nil {
				lookup = t.progs.ExitReasons()
			}
		}
		if lookup == nil {
			return ExitReason{}, false
		}
		return lookup(ev.Process, ev.Info)
	}
}

// restartMessage says how a process exited before supervisord restarted it.
func restartMessage(r ExitReason) string {
	if sig, ok := strings.CutPrefix(r.Desc, "terminated by "); ok {
		sig = strings.TrimSuffix(sig, " (core dumped)")
		return fmt.Sprintf("Was terminated by %s and restarted by supervisord", sig)
	}
	if r.Status >= 0 {
		return fmt.Sprintf("Exited with status %d and was restarted by supervisord", r.Status)
	}
	return restartedByDmn
}

func upOrStarting(st supervisor.ProcessState) bool {
	return st == supervisor.StateRunning || st == supervisor.StateStarting
}

// autorestart normalises the program's autorestart= the way supervisord
// reads it: "true", "false" or "unexpected".
func (t *Tracker) autorestart(proc string) string {
	v := ""
	if t.progs.Autorestart != nil {
		v = strings.ToLower(strings.TrimSpace(t.progs.Autorestart(proc)))
	}
	switch v {
	case "true", "yes", "on", "1":
		return "true"
	case "false", "no", "off", "0":
		return "false"
	}
	return "unexpected"
}

func (t *Tracker) trackRunning(proc string, st supervisor.ProcessState, now time.Time) {
	if st != supervisor.StateRunning {
		delete(t.runningSince, proc)
		return
	}
	if _, ok := t.runningSince[proc]; !ok {
		t.runningSince[proc] = now
	}
}

func (t *Tracker) expected(proc string, status int) bool {
	var codes []int
	if t.progs.ExpectedExit != nil {
		codes = t.progs.ExpectedExit(proc)
	}
	if len(codes) == 0 {
		codes = []int{0}
	}
	return slices.Contains(codes, status)
}

func exitMessage(status int) string { return fmt.Sprintf("Exited with status %d", status) }

// newID is unique within a tracker even when several issues open at one
// instant: time in base36, kind, a counter and the process.
func (t *Tracker) newID(k Kind, proc string, now time.Time) string {
	t.seq++
	return fmt.Sprintf("%s-%s-%d-%s", strconv.FormatInt(now.UnixNano(), 36), k, t.seq, proc)
}

func (t *Tracker) hasOpen(proc string, k Kind) bool {
	for _, i := range t.open {
		if i.Process == proc && i.Kind == k {
			return true
		}
	}
	return false
}

// openIssue opens an issue unless one is already open for (proc, kind).
func (t *Tracker) openIssue(k Kind, proc, msg string, now time.Time) []Issue {
	return t.openIssueAt(k, proc, msg, now, now)
}

// openIssueAt opens an issue that began at at.
func (t *Tracker) openIssueAt(k Kind, proc, msg string, at, now time.Time) []Issue {
	if t.hasOpen(proc, k) {
		return nil
	}
	is := Issue{
		ID:       t.newID(k, proc, now),
		Kind:     k,
		Process:  proc,
		Message:  msg,
		OpenedAt: at,
		Status:   StatusOpen,
	}
	if proc != "" && t.progs.ConfigOf != nil {
		is.Config = t.progs.ConfigOf(proc)
	}
	_ = t.store.putIssue(is)
	t.open[is.ID] = &is
	return []Issue{is}
}

func (t *Tracker) closeWhere(match func(*Issue) bool, st Status, note string, now time.Time) []Issue {
	var out []Issue
	for id, i := range t.open {
		if !match(i) {
			continue
		}
		i.Status, i.ClosedAt, i.Note = st, now, note
		_ = t.store.putIssue(*i)
		if i.Kind == KindCrashLoop {
			t.restarts[i.Process] = nil
		}
		out = append(out, *i)
		delete(t.open, id)
	}
	return out
}

// newestFirst orders issues by when they opened, to the millisecond (a
// paging cursor's precision), then by ID, both descending.
func newestFirst(a, b Issue) int {
	if c := cmp.Compare(b.OpenedAt.UnixMilli(), a.OpenedAt.UnixMilli()); c != 0 {
		return c
	}
	return cmp.Compare(b.ID, a.ID)
}
