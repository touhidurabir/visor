package issues

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/touhidurabir/visor/internal/monitor"
	"github.com/touhidurabir/visor/internal/supervisor"
)

// ErrNotOpen is returned when resolving or dismissing an unknown or already
// closed issue.
var ErrNotOpen = errors.New("issue is not open")

const fatalFallback = "Exited too quickly (process log may have details)"

// Tracker applies monitor updates to the issue list and the history.
type Tracker struct {
	store        *Store
	rules        Rules
	configOf     func(string) string
	expectedExit func(string) []int
	now          func() time.Time

	mu           sync.Mutex
	open         map[string]*Issue      // by ID
	runningSince map[string]time.Time   // process -> when it was first seen RUNNING
	restarts     map[string][]time.Time // process -> restart times inside the window
	failedPolls  int
}

// NewTracker loads the issues that are still open in s. expectedExit lists
// the exit statuses that are not failures for a process (nil means 0 only).
func NewTracker(s *Store, r Rules, configOf func(process string) string, expectedExit func(process string) []int, now func() time.Time) *Tracker {
	t := &Tracker{
		store: s, rules: r, configOf: configOf, expectedExit: expectedExit, now: now,
		open:         map[string]*Issue{},
		runningSince: map[string]time.Time{},
		restarts:     map[string][]time.Time{},
	}
	if list, err := s.Issues(Filter{Status: "open"}); err == nil {
		for _, is := range list {
			t.open[is.ID] = &is
		}
	}
	return t
}

// Apply records the update's transitions, opens and closes issues, and
// returns what changed. Store write failures do not stop tracking.
func (t *Tracker) Apply(u monitor.Update) (opened, closed []Issue) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()

	var trans []Transition
	restarted := map[string]bool{}
	for _, ev := range u.Events {
		switch ev.Kind {
		case monitor.StateChanged:
			trans = append(trans, Transition{At: ev.At, Process: ev.Process, From: ev.From.String(), To: ev.To.String(), Note: ev.Info.SpawnErr})
			t.trackRunning(ev.Process, ev.To, now)
			if ev.To == supervisor.StateStarting {
				restarted[ev.Process] = true
			}
			opened = append(opened, t.entered(ev, now)...)
		case monitor.Added:
			t.trackRunning(ev.Process, ev.To, now)
			opened = append(opened, t.entered(ev, now)...)
		case monitor.Removed:
			delete(t.runningSince, ev.Process)
			delete(t.restarts, ev.Process)
		case monitor.Restarted:
			restarted[ev.Process] = true
			delete(t.runningSince, ev.Process)
			t.trackRunning(ev.Process, ev.To, now)
			if ev.To != supervisor.StateBackoff && !t.expected(ev.Process, ev.Info.ExitStatus) {
				opened = append(opened, t.openIssue(KindUnexpectedExit, ev.Process, exitMessage(ev.Info.ExitStatus), now)...)
			}
		case monitor.Reconnected:
			closed = append(closed, t.closeWhere(func(i *Issue) bool { return i.Kind == KindUnreachable }, StatusResolvedAuto, now)...)
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
	} else if !u.Snapshot.At.IsZero() {
		t.failedPolls++
		if t.failedPolls >= t.rules.UnreachablePolls {
			msg := "supervisord is not responding"
			if u.Snapshot.Err != nil {
				msg += ": " + u.Snapshot.Err.Error()
			}
			opened = append(opened, t.openIssue(KindUnreachable, "", msg, now)...)
		}
	}

	// Reconcile RUNNING tracking with the snapshot, then close stable fatals.
	for _, p := range u.Snapshot.Processes {
		t.trackRunning(p.FullName(), p.State, now)
	}
	closed = append(closed, t.closeWhere(func(i *Issue) bool {
		since, ok := t.runningSince[i.Process]
		return i.Kind == KindFatal && ok && now.Sub(since) >= t.rules.StableFor
	}, StatusResolvedAuto, now)...)
	return opened, closed
}

// Open returns the open issues, newest first.
func (t *Tracker) Open() []Issue {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]Issue, 0, len(t.open))
	for _, i := range t.open {
		out = append(out, *i)
	}
	slices.SortFunc(out, func(a, b Issue) int { return b.OpenedAt.Compare(a.OpenedAt) })
	return out
}

// Resolve closes an open issue as resolved by the user.
func (t *Tracker) Resolve(id string) error { return t.closeByID(id, StatusResolvedUser) }

// Dismiss closes an open issue without resolving it.
func (t *Tracker) Dismiss(id string) error { return t.closeByID(id, StatusDismissed) }

func (t *Tracker) closeByID(id string, st Status) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.open[id]; !ok {
		return fmt.Errorf("%w: %s", ErrNotOpen, id)
	}
	t.closeWhere(func(i *Issue) bool { return i.ID == id }, st, t.now())
	return nil
}

// entered opens issues for a process that just entered (or appeared in) a state.
func (t *Tracker) entered(ev monitor.Event, now time.Time) []Issue {
	switch ev.To {
	case supervisor.StateFatal:
		msg := ev.Info.SpawnErr
		if msg == "" {
			msg = fatalFallback
		}
		return t.openIssue(KindFatal, ev.Process, msg, now)
	case supervisor.StateExited:
		if !t.expected(ev.Process, ev.Info.ExitStatus) {
			return t.openIssue(KindUnexpectedExit, ev.Process, exitMessage(ev.Info.ExitStatus), now)
		}
	}
	return nil
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
	if t.expectedExit != nil {
		codes = t.expectedExit(proc)
	}
	if len(codes) == 0 {
		codes = []int{0}
	}
	return slices.Contains(codes, status)
}

func exitMessage(status int) string { return fmt.Sprintf("Exited with status %d", status) }

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
	if t.hasOpen(proc, k) {
		return nil
	}
	is := Issue{
		ID:       strconv.FormatInt(now.UnixNano(), 36) + "-" + proc,
		Kind:     k,
		Process:  proc,
		Message:  msg,
		OpenedAt: now,
		Status:   StatusOpen,
	}
	if proc != "" && t.configOf != nil {
		is.Config = t.configOf(proc)
	}
	_ = t.store.putIssue(is)
	t.open[is.ID] = &is
	return []Issue{is}
}

func (t *Tracker) closeWhere(match func(*Issue) bool, st Status, now time.Time) []Issue {
	var out []Issue
	for id, i := range t.open {
		if !match(i) {
			continue
		}
		i.Status, i.ClosedAt = st, now
		_ = t.store.putIssue(*i)
		if i.Kind == KindCrashLoop {
			t.restarts[i.Process] = nil
		}
		out = append(out, *i)
		delete(t.open, id)
	}
	return out
}
