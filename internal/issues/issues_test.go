package issues

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/touhidurabir/visor/internal/monitor"
	"github.com/touhidurabir/visor/internal/supervisor"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time          { return c.t }
func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newTracker(t *testing.T, expected []int) (*Tracker, *Store, *clock) {
	t.Helper()
	return newTrackerAuto(t, expected, "")
}

// newTrackerAuto also sets every program's autorestart= ("" is the default).
func newTrackerAuto(t *testing.T, expected []int, autorestart string) (*Tracker, *Store, *clock) {
	t.Helper()
	s, err := OpenStore(t.TempDir(), 90*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	c := &clock{t: time.Now()}
	return NewTracker(s, DefaultRules, programs(expected, autorestart), c.now), s, c
}

func programs(expected []int, autorestart string) Programs {
	return Programs{
		ConfigOf:     func(p string) string { return "/conf/" + p + ".conf" },
		ExpectedExit: func(string) []int { return expected },
		Autorestart:  func(string) string { return autorestart },
	}
}

func info(state supervisor.ProcessState) supervisor.ProcessInfo {
	return supervisor.ProcessInfo{Name: "w", Group: "g", State: state}
}

func change(at time.Time, from, to supervisor.ProcessState, pi supervisor.ProcessInfo) monitor.Update {
	pi.State = to
	return monitor.Update{
		Snapshot: monitor.Snapshot{At: at, Reachable: true, Processes: []supervisor.ProcessInfo{pi}},
		Events:   []monitor.Event{{Kind: monitor.StateChanged, At: at, Process: "g:w", From: from, To: to, Info: pi}},
	}
}

func steady(at time.Time, pi supervisor.ProcessInfo) monitor.Update {
	return monitor.Update{Snapshot: monitor.Snapshot{At: at, Reachable: true, Processes: []supervisor.ProcessInfo{pi}}}
}

func TestFatalOpensAndAutoResolves(t *testing.T) {
	tr, _, c := newTracker(t, nil)
	pi := info(supervisor.StateFatal)
	pi.SpawnErr = "can't find command"
	opened, _ := tr.Apply(change(c.now(), supervisor.StateBackoff, supervisor.StateFatal, pi))
	if len(opened) != 1 || opened[0].Kind != KindFatal || opened[0].Message != "can't find command" || opened[0].Config != "/conf/g:w.conf" {
		t.Fatalf("opened = %+v", opened)
	}
	run := info(supervisor.StateRunning)
	tr.Apply(change(c.now(), supervisor.StateStarting, supervisor.StateRunning, run))
	c.advance(59 * time.Second)
	if _, closed := tr.Apply(steady(c.now(), run)); len(closed) != 0 || len(tr.Open()) != 1 {
		t.Fatalf("closed too early: %+v", closed)
	}
	c.advance(time.Second)
	_, closed := tr.Apply(steady(c.now(), run))
	if len(closed) != 1 || closed[0].Status != StatusResolvedAuto || closed[0].ClosedAt.IsZero() || len(tr.Open()) != 0 {
		t.Fatalf("closed = %+v", closed)
	}
}

func TestFatalEmptySpawnErr(t *testing.T) {
	tr, _, c := newTracker(t, nil)
	opened, _ := tr.Apply(change(c.now(), supervisor.StateBackoff, supervisor.StateFatal, info(0)))
	if len(opened) != 1 || opened[0].Message != "Exited too quickly (process log may have details)" {
		t.Fatalf("opened = %+v", opened)
	}
}

func TestFatalNotResolvedByFlapping(t *testing.T) {
	tr, _, c := newTracker(t, nil)
	tr.Apply(change(c.now(), supervisor.StateBackoff, supervisor.StateFatal, info(0)))
	run := info(supervisor.StateRunning)
	tr.Apply(change(c.now(), supervisor.StateStarting, supervisor.StateRunning, run))
	c.advance(40 * time.Second)
	tr.Apply(change(c.now(), supervisor.StateRunning, supervisor.StateExited, info(0)))
	c.advance(40 * time.Second)
	tr.Apply(change(c.now(), supervisor.StateStarting, supervisor.StateRunning, run))
	c.advance(40 * time.Second)
	if _, closed := tr.Apply(steady(c.now(), run)); len(closed) != 0 {
		t.Fatalf("closed despite flapping: %+v", closed)
	}
}

func TestUnexpectedExit(t *testing.T) {
	tr, _, c := newTracker(t, []int{0, 2})
	pi := info(supervisor.StateExited)
	pi.ExitStatus = 2
	if opened, _ := tr.Apply(change(c.now(), supervisor.StateRunning, supervisor.StateExited, pi)); len(opened) != 0 {
		t.Fatalf("expected exit opened %+v", opened)
	}
	pi.ExitStatus = 1
	opened, _ := tr.Apply(change(c.now(), supervisor.StateRunning, supervisor.StateExited, pi))
	if len(opened) != 1 || opened[0].Kind != KindUnexpectedExit || opened[0].Message != "Exited with status 1" {
		t.Fatalf("opened = %+v", opened)
	}
}

// restartEvent is a process seen again in state with a new pid. Like
// supervisord after a respawn, it reports exit status 0 and the last stop
// one second ago.
func restartEvent(at time.Time, state supervisor.ProcessState) monitor.Update {
	return restartFrom(at, state, state)
}

// restartFrom is a process seen in from, then again in to with a new pid.
func restartFrom(at time.Time, from, to supervisor.ProcessState) monitor.Update {
	pi := info(to)
	pi.PID = int(at.UnixNano() % 1e6)
	pi.Start, pi.Stop = at, at.Add(-time.Second)
	u := steady(at, pi)
	u.Events = []monitor.Event{{Kind: monitor.Restarted, At: at, Process: "g:w", From: from, To: to, Info: pi}}
	return u
}

// crashChange is a poll that finds a RUNNING process in state to: it
// exited and supervisord restarted it in between.
func crashChange(at time.Time, to supervisor.ProcessState) monitor.Update {
	pi := info(to)
	pi.Start, pi.Stop = at, at.Add(-time.Second)
	return change(at, supervisor.StateRunning, to, pi)
}

func TestRestartBySupervisordOpensUnexpectedExit(t *testing.T) {
	cases := []struct {
		name string
		u    func(time.Time) monitor.Update
	}{
		{"RUNNING to STARTING", func(at time.Time) monitor.Update { return crashChange(at, supervisor.StateStarting) }},
		{"RUNNING to BACKOFF", func(at time.Time) monitor.Update { return crashChange(at, supervisor.StateBackoff) }},
		{"restarted RUNNING", func(at time.Time) monitor.Update { return restartEvent(at, supervisor.StateRunning) }},
		{"restarted RUNNING to STARTING", func(at time.Time) monitor.Update {
			return restartFrom(at, supervisor.StateRunning, supervisor.StateStarting)
		}},
	}
	for _, tc := range cases {
		for _, auto := range []string{"", "unexpected", "UNEXPECTED"} {
			t.Run(tc.name+" autorestart="+auto, func(t *testing.T) {
				tr, _, c := newTrackerAuto(t, nil, auto)
				opened, _ := tr.Apply(tc.u(c.now()))
				if len(opened) != 1 || opened[0].Kind != KindUnexpectedExit || opened[0].Message != "Exited and was restarted by supervisord" {
					t.Fatalf("opened = %+v", opened)
				}
				if want := c.now().Add(-time.Second); !opened[0].OpenedAt.Equal(want) {
					t.Errorf("OpenedAt = %v, want the stop time %v", opened[0].OpenedAt, want)
				}
				if again, _ := tr.Apply(tc.u(c.now().Add(5 * time.Second))); len(again) != 0 {
					t.Errorf("second crash opened a duplicate: %+v", again)
				}
			})
		}
		for _, auto := range []string{"true", "yes", "1", "false"} {
			t.Run(tc.name+" autorestart="+auto, func(t *testing.T) {
				tr, _, c := newTrackerAuto(t, nil, auto)
				if opened, _ := tr.Apply(tc.u(c.now())); len(opened) != 0 {
					t.Fatalf("opened = %+v", opened)
				}
			})
		}
	}
}

func TestRestartWithoutStopTimeOpensNow(t *testing.T) {
	tr, _, c := newTracker(t, nil)
	u := restartEvent(c.now(), supervisor.StateRunning)
	u.Events[0].Info.Stop = time.Time{}
	opened, _ := tr.Apply(u)
	if len(opened) != 1 || !opened[0].OpenedAt.Equal(c.now()) {
		t.Fatalf("opened = %+v", opened)
	}
}

func TestStartsThatAreNotCrashesOpenNothing(t *testing.T) {
	tr, _, c := newTracker(t, nil)
	for _, u := range []monitor.Update{
		change(c.now(), supervisor.StateStopped, supervisor.StateStarting, info(0)),
		change(c.now(), supervisor.StateExited, supervisor.StateStarting, info(0)),
		change(c.now(), supervisor.StateStarting, supervisor.StateBackoff, info(0)),
		restartEvent(c.now(), supervisor.StateStopped),
	} {
		if opened, _ := tr.Apply(u); len(opened) != 0 {
			t.Fatalf("%+v opened %+v", u.Events, opened)
		}
	}
}

func TestAutorestartTrueCrashesOnlyCountTowardCrashLoop(t *testing.T) {
	tr, _, c := newTrackerAuto(t, nil, "true")
	var opened []Issue
	for i := 0; i < 5; i++ {
		o, _ := tr.Apply(restartEvent(c.now(), supervisor.StateRunning))
		opened = append(opened, o...)
		c.advance(3 * time.Second)
	}
	if len(opened) != 1 || opened[0].Kind != KindCrashLoop {
		t.Fatalf("opened = %+v", opened)
	}
}

func TestRestartedWrittenToHistory(t *testing.T) {
	tr, s, c := newTracker(t, nil)
	tr.Apply(restartEvent(c.now(), supervisor.StateRunning))
	h, _ := s.History("g:w", 0)
	if len(h) != 1 || h[0].From != "RUNNING" || h[0].To != "RUNNING" || h[0].Note != "Restarted by supervisord" {
		t.Fatalf("history = %+v", h)
	}
}

func TestRestartedIntoFatalOpensFatal(t *testing.T) {
	tr, _, c := newTracker(t, nil)
	u := restartEvent(c.now(), supervisor.StateFatal)
	u.Events[0].Info.SpawnErr = "can't find command"
	opened, _ := tr.Apply(u)
	if len(opened) != 1 || opened[0].Kind != KindFatal || opened[0].Message != "can't find command" {
		t.Fatalf("opened = %+v", opened)
	}
}

func TestRemovedClosesProcessIssues(t *testing.T) {
	tr, _, c := newTracker(t, nil)
	tr.Apply(change(c.now(), supervisor.StateBackoff, supervisor.StateFatal, info(0)))
	other := supervisor.ProcessInfo{Name: "v", Group: "g", State: supervisor.StateFatal}
	tr.Apply(monitor.Update{
		Snapshot: monitor.Snapshot{At: c.now(), Reachable: true},
		Events:   []monitor.Event{{Kind: monitor.StateChanged, At: c.now(), Process: "g:v", From: supervisor.StateBackoff, To: supervisor.StateFatal, Info: other}},
	})
	for i := 0; i < 5; i++ {
		tr.Apply(restartEvent(c.now(), supervisor.StateRunning))
		c.advance(time.Second)
	}
	if n := len(tr.Open()); n != 4 { // g:w fatal, unexpected exit, crash loop; g:v fatal
		t.Fatalf("open = %+v", tr.Open())
	}
	_, closed := tr.Apply(monitor.Update{
		Snapshot: monitor.Snapshot{At: c.now(), Reachable: true},
		Events:   []monitor.Event{{Kind: monitor.Removed, At: c.now(), Process: "g:w", From: supervisor.StateRunning, Info: info(supervisor.StateRunning)}},
	})
	if len(closed) != 3 {
		t.Fatalf("closed = %+v", closed)
	}
	for _, is := range closed {
		if is.Process != "g:w" || is.Status != StatusResolvedAuto || is.Note != "program removed" {
			t.Errorf("closed %+v", is)
		}
	}
	if open := tr.Open(); len(open) != 1 || open[0].Process != "g:v" {
		t.Fatalf("open = %+v", open)
	}
}

// M-12: Visor applying a changed group stops, removes and adds it again.
// Its process going away then is not the program being removed: its issues
// close noting the apply.
func TestRemovedDuringReapplyNotesConfigApplied(t *testing.T) {
	tr, _, c := newTracker(t, nil)
	tr.Apply(change(c.now(), supervisor.StateBackoff, supervisor.StateFatal, info(0)))
	removed := monitor.Update{
		Snapshot: monitor.Snapshot{At: c.now(), Reachable: true},
		Events:   []monitor.Event{{Kind: monitor.Removed, At: c.now(), Process: "g:w", From: supervisor.StateFatal, Info: info(supervisor.StateFatal)}},
	}
	is := func(p string) bool { return p == "g:w" }
	tr.ExpectApply(is, is, c.now().Add(5*time.Second))
	_, closed := tr.Apply(removed)
	if len(closed) != 1 || closed[0].Status != StatusResolvedAuto || closed[0].Note != "config applied" {
		t.Fatalf("closed = %+v, want the fatal issue noted config applied", closed)
	}

	// Removing a program on request (covered, but not re-applied) is a removal.
	c.advance(6 * time.Second) // the apply above has ended
	removed.Snapshot.At, removed.Events[0].At = c.now(), c.now()
	tr.Apply(change(c.now(), supervisor.StateBackoff, supervisor.StateFatal, info(0)))
	tr.ExpectApply(is, nil, c.now().Add(5*time.Second))
	_, closed = tr.Apply(removed)
	if len(closed) != 1 || closed[0].Note != "program removed" {
		t.Fatalf("closed = %+v, want program removed", closed)
	}
}

func TestUserActionsAreNotFailures(t *testing.T) {
	tr, s, c := newTracker(t, nil)
	tr.Expect(func(p string) bool { return p == "g:w" }, c.now().Add(5*time.Second))
	var opened []Issue
	for i := 0; i < 5; i++ {
		o, _ := tr.Apply(restartEvent(c.now(), supervisor.StateRunning))
		opened = append(opened, o...)
		o, _ = tr.Apply(change(c.now(), supervisor.StateStopped, supervisor.StateStarting, info(0)))
		opened = append(opened, o...)
		o, _ = tr.Apply(crashChange(c.now(), supervisor.StateStarting))
		opened = append(opened, o...)
		c.advance(time.Second)
	}
	// Start then Stop inside one poll: same state, new start time, and
	// supervisord reports -1 for "killed by SIGTERM".
	stopped := restartEvent(c.now(), supervisor.StateStopped)
	stopped.Events[0].Info.ExitStatus = -1
	o, _ := tr.Apply(stopped)
	opened = append(opened, o...)
	if len(opened) != 0 {
		t.Fatalf("user actions opened %+v", opened)
	}
	h, _ := s.History("g:w", 2)
	if len(h) != 2 || h[0].Note != "Restarted by you" || h[1].Note != "by you" {
		t.Fatalf("history = %+v", h)
	}

	// Other processes are not covered.
	pi := supervisor.ProcessInfo{Name: "v", Group: "g", State: supervisor.StateRunning, Stop: c.now()}
	o, _ = tr.Apply(monitor.Update{
		Snapshot: monitor.Snapshot{At: c.now(), Reachable: true},
		Events:   []monitor.Event{{Kind: monitor.Restarted, At: c.now(), Process: "g:v", From: supervisor.StateRunning, To: supervisor.StateRunning, Info: pi}},
	})
	if len(o) != 1 || o[0].Process != "g:v" {
		t.Fatalf("other process: opened %+v", o)
	}

	// After the deadline, a crash is a crash again.
	c.advance(5 * time.Second)
	if o, _ := tr.Apply(restartEvent(c.now(), supervisor.StateRunning)); len(o) != 1 || o[0].Kind != KindUnexpectedExit {
		t.Fatalf("after deadline: opened %+v", o)
	}
}

// The user starting a program that exits with a bad code is a failure:
// only restarts and crash-loop counting are the user's doing.
func TestUserActionStillOpensObservedExit(t *testing.T) {
	tr, s, c := newTracker(t, nil)
	tr.Expect(func(string) bool { return true }, c.now().Add(5*time.Second))
	exited := info(supervisor.StateExited)
	exited.ExitStatus = 1
	opened, _ := tr.Apply(change(c.now(), supervisor.StateStarting, supervisor.StateExited, exited))
	if len(opened) != 1 || opened[0].Kind != KindUnexpectedExit || opened[0].Message != "Exited with status 1" {
		t.Fatalf("opened = %+v", opened)
	}
	if h, _ := s.History("g:w", 1); len(h) != 1 || h[0].Note != "by you" {
		t.Fatalf("history = %+v", h)
	}
	exited.ExitStatus = 0 // an expected exit still opens nothing
	tr2, _, c2 := newTracker(t, nil)
	tr2.Expect(func(string) bool { return true }, c2.now().Add(5*time.Second))
	if o, _ := tr2.Apply(change(c2.now(), supervisor.StateRunning, supervisor.StateExited, exited)); len(o) != 0 {
		t.Fatalf("expected exit opened %+v", o)
	}
}

func TestUserActionStillOpensFatal(t *testing.T) {
	tr, _, c := newTracker(t, nil)
	tr.Expect(func(string) bool { return true }, c.now().Add(5*time.Second))
	if opened, _ := tr.Apply(change(c.now(), supervisor.StateBackoff, supervisor.StateFatal, info(0))); len(opened) != 1 {
		t.Fatalf("opened = %+v", opened)
	}
}

func TestExpectDeadlineCanMove(t *testing.T) {
	tr, _, c := newTracker(t, nil)
	settle := tr.Expect(func(string) bool { return true }, c.now().Add(2*time.Minute))
	settle(c.now().Add(5 * time.Second))
	c.advance(6 * time.Second)
	if opened, _ := tr.Apply(restartEvent(c.now(), supervisor.StateRunning)); len(opened) != 1 {
		t.Fatalf("expectation outlived its new deadline: opened %+v", opened)
	}
}

func down(c *clock) monitor.Update {
	return monitor.Update{Snapshot: monitor.Snapshot{At: c.now(), Err: supervisor.ErrUnreachable}}
}

func TestUserShutdownHoldsUntilReachable(t *testing.T) {
	tr, _, c := newTracker(t, nil)
	tr.ExpectDaemon(c.now().Add(5*time.Second), true)
	for i := 0; i < 20; i++ { // 40s down, well past the deadline
		if opened, _ := tr.Apply(down(c)); len(opened) != 0 {
			t.Fatalf("poll %d: opened %+v", i, opened)
		}
		c.advance(2 * time.Second)
	}
	// supervisord comes back (started by the user): its restarted
	// processes are still the user's doing.
	u := restartEvent(c.now(), supervisor.StateRunning)
	u.Events = append([]monitor.Event{{Kind: monitor.Reconnected, At: c.now()}}, u.Events...)
	if opened, _ := tr.Apply(u); len(opened) != 0 {
		t.Fatalf("reconnect opened %+v", opened)
	}
	c.advance(2 * time.Second)
	tr.Apply(down(c))
	c.advance(2 * time.Second)
	if opened, _ := tr.Apply(down(c)); len(opened) != 1 || opened[0].Kind != KindUnreachable {
		t.Fatalf("a later outage: opened %+v", opened)
	}
}

// A restart the user asked for that never comes back is an outage once
// its deadline passes.
func TestUserRestartHoldsOnlyUntilDeadline(t *testing.T) {
	tr, _, c := newTracker(t, nil)
	tr.ExpectDaemon(c.now().Add(30*time.Second), false)
	var opened []Issue
	for i := 0; i < 15; i++ { // 0s..28s: inside the deadline
		o, _ := tr.Apply(down(c))
		opened = append(opened, o...)
		c.advance(2 * time.Second)
	}
	if len(opened) != 0 {
		t.Fatalf("opened before the deadline: %+v", opened)
	}
	c.advance(4 * time.Second)
	o, _ := tr.Apply(down(c))
	if len(o) != 1 || o[0].Kind != KindUnreachable {
		t.Fatalf("after the deadline: opened %+v", o)
	}
}

func TestFailedDaemonActionEndsExpectation(t *testing.T) {
	tr, _, c := newTracker(t, nil)
	settle := tr.ExpectDaemon(c.now().Add(2*time.Minute), true)
	tr.Apply(down(c)) // supervisord was already down when the user asked
	settle(time.Time{})
	c.advance(2 * time.Second)
	if opened, _ := tr.Apply(down(c)); len(opened) != 1 || opened[0].Kind != KindUnreachable {
		t.Fatalf("opened = %+v", opened)
	}
}

func TestDaemonExpectationEndsWhenNeverDown(t *testing.T) {
	tr, _, c := newTracker(t, nil)
	tr.ExpectDaemon(c.now().Add(5*time.Second), true)
	tr.Apply(steady(c.now(), info(supervisor.StateRunning)))
	c.advance(6 * time.Second)
	tr.Apply(steady(c.now(), info(supervisor.StateRunning)))
	tr.Apply(down(c))
	c.advance(2 * time.Second)
	if opened, _ := tr.Apply(down(c)); len(opened) != 1 {
		t.Fatalf("outage after the deadline: opened %+v", opened)
	}
}

func fatalInfo(start, stop time.Time) supervisor.ProcessInfo {
	pi := info(supervisor.StateFatal)
	pi.Start, pi.Stop = start, stop
	return pi
}

func added(at time.Time, pi supervisor.ProcessInfo) monitor.Update {
	u := steady(at, pi)
	u.Events = []monitor.Event{{Kind: monitor.Added, At: at, Process: pi.FullName(), To: pi.State, Info: pi}}
	return u
}

func TestUserClosedIssueStaysClosedAcrossLaunches(t *testing.T) {
	for _, closeIt := range []func(*Tracker, string) error{(*Tracker).Dismiss, (*Tracker).Resolve} {
		tr, s, c := newTracker(t, nil)
		failed := fatalInfo(c.now().Add(-time.Minute), c.now().Add(-50*time.Second))
		opened, _ := tr.Apply(added(c.now(), failed))
		if len(opened) != 1 {
			t.Fatalf("opened = %+v", opened)
		}
		c.advance(time.Minute)
		if err := closeIt(tr, opened[0].ID); err != nil {
			t.Fatal(err)
		}

		// Visor relaunches; supervisord still shows the same failure.
		c.advance(time.Hour)
		tr2 := NewTracker(s, DefaultRules, programs(nil, ""), c.now)
		if o, _ := tr2.Apply(added(c.now(), failed)); len(o) != 0 {
			t.Fatalf("the same failure reopened: %+v", o)
		}
		// A new failure (started and stopped after the close) opens.
		again := fatalInfo(c.now().Add(-10*time.Second), c.now().Add(-5*time.Second))
		o, _ := tr2.Apply(change(c.now(), supervisor.StateBackoff, supervisor.StateFatal, again))
		if len(o) != 1 || o[0].Kind != KindFatal {
			t.Fatalf("a new failure: opened %+v", o)
		}
	}
}

func TestUserClosedUnexpectedExitStaysClosed(t *testing.T) {
	tr, s, c := newTracker(t, nil)
	pi := info(supervisor.StateExited)
	pi.ExitStatus, pi.Start, pi.Stop = 1, c.now().Add(-time.Hour), c.now().Add(-time.Minute)
	opened, _ := tr.Apply(added(c.now(), pi))
	if len(opened) != 1 || opened[0].Kind != KindUnexpectedExit {
		t.Fatalf("opened = %+v", opened)
	}
	c.advance(time.Second)
	_ = tr.Dismiss(opened[0].ID)
	tr2 := NewTracker(s, DefaultRules, programs(nil, ""), c.now)
	if o, _ := tr2.Apply(added(c.now(), pi)); len(o) != 0 {
		t.Fatalf("reopened: %+v", o)
	}
}

func TestAutoResolvedIssueDoesNotBlockReopen(t *testing.T) {
	tr, s, c := newTracker(t, nil)
	failed := fatalInfo(c.now().Add(-time.Minute), c.now().Add(-50*time.Second))
	tr.Apply(added(c.now(), failed))
	tr.Apply(change(c.now(), supervisor.StateStarting, supervisor.StateRunning, info(supervisor.StateRunning)))
	c.advance(time.Minute)
	if _, closed := tr.Apply(steady(c.now(), info(supervisor.StateRunning))); len(closed) != 1 {
		t.Fatalf("closed = %+v", closed)
	}
	tr2 := NewTracker(s, DefaultRules, programs(nil, ""), c.now)
	if o, _ := tr2.Apply(added(c.now(), failed)); len(o) != 1 {
		t.Fatalf("opened = %+v", o)
	}
}

func TestBackoffRetriesDoNotOpenUnexpectedExit(t *testing.T) {
	tr, _, c := newTracker(t, nil)
	if opened, _ := tr.Apply(restartEvent(c.now(), supervisor.StateBackoff)); len(opened) != 0 {
		t.Fatalf("backoff retry opened %+v", opened)
	}
}

func TestCrashLoopOpensOnceAndNeedsUser(t *testing.T) {
	tr, _, c := newTracker(t, nil)
	var loops []Issue
	for i := 0; i < 8; i++ {
		opened, _ := tr.Apply(change(c.now(), supervisor.StateBackoff, supervisor.StateStarting, info(0)))
		loops = append(loops, opened...)
		c.advance(10 * time.Second)
	}
	if len(loops) != 1 || loops[0].Kind != KindCrashLoop || loops[0].Message != "Restarted 5 times in 10 minutes" {
		t.Fatalf("loops = %+v", loops)
	}
	c.advance(time.Hour)
	run := info(supervisor.StateRunning)
	tr.Apply(change(c.now(), supervisor.StateStarting, supervisor.StateRunning, run))
	c.advance(time.Hour)
	if _, closed := tr.Apply(steady(c.now(), run)); len(closed) != 0 || len(tr.Open()) != 1 {
		t.Fatalf("crash loop auto-closed: %+v", closed)
	}
	if err := tr.Resolve(loops[0].ID); err != nil {
		t.Fatal(err)
	}
	if len(tr.Open()) != 0 {
		t.Fatal("still open")
	}
	if err := tr.Resolve(loops[0].ID); err == nil {
		t.Fatal("second resolve should fail")
	}
}

func TestCrashLoopWindowExpires(t *testing.T) {
	tr, _, c := newTracker(t, nil)
	for i := 0; i < 8; i++ {
		tr.Apply(change(c.now(), supervisor.StateBackoff, supervisor.StateStarting, info(0)))
		c.advance(3 * time.Minute)
	}
	if len(tr.Open()) != 0 {
		t.Fatalf("slow restarts opened %+v", tr.Open())
	}
}

func TestCrashLoopCountsRestartedEvents(t *testing.T) {
	tr, _, c := newTracker(t, nil)
	for i := 0; i < 5; i++ {
		tr.Apply(restartEvent(c.now(), supervisor.StateBackoff))
		c.advance(2 * time.Second)
	}
	open := tr.Open()
	if len(open) != 1 || open[0].Kind != KindCrashLoop {
		t.Fatalf("open = %+v", open)
	}
}

func TestOneRestartPerProcessPerUpdate(t *testing.T) {
	tr, _, c := newTracker(t, nil)
	for i := 0; i < 4; i++ {
		u := change(c.now(), supervisor.StateBackoff, supervisor.StateStarting, info(0))
		u.Events = append(u.Events, monitor.Event{Kind: monitor.Restarted, At: c.now(), Process: "g:w", From: 30, To: 10})
		tr.Apply(u)
		c.advance(time.Second)
	}
	if len(tr.Open()) != 0 {
		t.Fatalf("double counted: %+v", tr.Open())
	}
}

func TestUnreachableAfterTwoPolls(t *testing.T) {
	tr, _, c := newTracker(t, nil)
	down := func() monitor.Update {
		return monitor.Update{Snapshot: monitor.Snapshot{At: c.now(), Err: supervisor.ErrUnreachable}}
	}
	if opened, _ := tr.Apply(down()); len(opened) != 0 {
		t.Fatal("opened after one poll")
	}
	c.advance(2 * time.Second)
	opened, _ := tr.Apply(down())
	if len(opened) != 1 || opened[0].Kind != KindUnreachable || opened[0].Process != "" {
		t.Fatalf("opened = %+v", opened)
	}
	c.advance(2 * time.Second)
	tr.Apply(down())
	if len(tr.Open()) != 1 {
		t.Fatal("duplicate unreachable")
	}
	_, closed := tr.Apply(monitor.Update{
		Snapshot: monitor.Snapshot{At: c.now(), Reachable: true},
		Events:   []monitor.Event{{Kind: monitor.Reconnected, At: c.now()}},
	})
	if len(closed) != 1 || closed[0].Status != StatusResolvedAuto {
		t.Fatalf("closed = %+v", closed)
	}
}

// supervisord answers every call with SHUTDOWN_STATE while it stops its
// processes for a restart or shutdown (which can take stopwaitsecs per
// program). It is answering, so those polls are not "not responding"; if it
// then really goes away, connection errors still open the issue.
func TestShutdownStateIsNotUnreachable(t *testing.T) {
	tr, _, c := newTracker(t, nil)
	poll := func(err error) []Issue {
		c.advance(time.Second)
		opened, _ := tr.Apply(monitor.Update{Snapshot: monitor.Snapshot{At: c.now(), Err: err}})
		return opened
	}
	for i := 0; i < 5; i++ {
		if opened := poll(supervisor.ErrShutdownState); len(opened) != 0 {
			t.Fatalf("SHUTDOWN_STATE poll %d opened %+v", i+1, opened)
		}
	}
	if opened := poll(supervisor.ErrUnreachable); len(opened) != 0 {
		t.Fatalf("one failed poll after shutdown opened %+v", opened)
	}
	if opened := poll(supervisor.ErrUnreachable); len(opened) != 1 || opened[0].Kind != KindUnreachable {
		t.Fatalf("supervisord gone after shutdown: opened = %+v", opened)
	}
}

func TestUnreachableNeedsConsecutivePolls(t *testing.T) {
	tr, _, c := newTracker(t, nil)
	down := monitor.Update{Snapshot: monitor.Snapshot{At: c.now()}}
	up := monitor.Update{Snapshot: monitor.Snapshot{At: c.now(), Reachable: true}}
	tr.Apply(down)
	tr.Apply(up)
	tr.Apply(down)
	if len(tr.Open()) != 0 {
		t.Fatal("non-consecutive polls opened an issue")
	}
}

func TestNoDuplicateOpenIssue(t *testing.T) {
	tr, _, c := newTracker(t, nil)
	n := 0
	for i := 0; i < 3; i++ {
		opened, _ := tr.Apply(change(c.now(), supervisor.StateBackoff, supervisor.StateFatal, info(0)))
		n += len(opened)
	}
	if n != 1 || len(tr.Open()) != 1 {
		t.Fatalf("opened %d, open %d", n, len(tr.Open()))
	}
	if err := tr.Dismiss(tr.Open()[0].ID); err != nil {
		t.Fatal(err)
	}
	c.advance(time.Second)
	if opened, _ := tr.Apply(change(c.now(), supervisor.StateBackoff, supervisor.StateFatal, info(0))); len(opened) != 1 {
		t.Fatal("dismissed issue should allow a new one")
	}
}

func TestHistoryAppended(t *testing.T) {
	tr, s, c := newTracker(t, nil)
	tr.Apply(change(c.now(), supervisor.StateStopped, supervisor.StateStarting, info(0)))
	c.advance(time.Second)
	tr.Apply(change(c.now(), supervisor.StateStarting, supervisor.StateRunning, info(0)))
	h, err := s.History("g:w", 10)
	if err != nil || len(h) != 2 {
		t.Fatalf("history = %+v, %v", h, err)
	}
	if h[0].From != "STARTING" || h[0].To != "RUNNING" || h[1].To != "STARTING" {
		t.Fatalf("history order/content = %+v", h)
	}
	if h, _ := s.History("g:w", 1); len(h) != 1 {
		t.Fatal("limit ignored")
	}
	if h, _ := s.History("other", 10); len(h) != 0 {
		t.Fatal("process filter ignored")
	}
}

func TestStoreRoundTripAndLastRecordWins(t *testing.T) {
	dir := t.TempDir()
	s, _ := OpenStore(dir, time.Hour)
	now := time.Now().Truncate(time.Millisecond)
	is := Issue{ID: "a", Kind: KindFatal, Process: "g:w", Message: "m", OpenedAt: now, Status: StatusOpen}
	_ = s.putIssue(is)
	is.Status, is.ClosedAt = StatusResolvedUser, now.Add(time.Minute)
	_ = s.putIssue(is)

	s2, _ := OpenStore(dir, time.Hour)
	got, err := s2.Issues(Filter{})
	if err != nil || len(got) != 1 || got[0].Status != StatusResolvedUser || !got[0].OpenedAt.Equal(now) {
		t.Fatalf("got %+v, %v", got, err)
	}
	for name, mode := range map[string]os.FileMode{"": 0o700, "issues.jsonl": 0o600} {
		fi, err := os.Stat(filepath.Join(dir, name))
		if err != nil || fi.Mode().Perm() != mode {
			t.Fatalf("%q mode = %v, %v", name, fi.Mode(), err)
		}
	}
}

func TestTrackerReloadsOpenIssues(t *testing.T) {
	tr, s, c := newTracker(t, nil)
	tr.Apply(change(c.now(), supervisor.StateBackoff, supervisor.StateFatal, info(0)))
	tr2 := NewTracker(s, DefaultRules, Programs{}, c.now)
	if len(tr2.Open()) != 1 {
		t.Fatalf("open = %+v", tr2.Open())
	}
	if opened, _ := tr2.Apply(change(c.now(), supervisor.StateBackoff, supervisor.StateFatal, info(0))); len(opened) != 0 {
		t.Fatal("duplicate after reload")
	}
}

func TestStoreRetentionCompacts(t *testing.T) {
	dir := t.TempDir()
	s, _ := OpenStore(dir, 90*24*time.Hour)
	old := time.Now().Add(-91 * 24 * time.Hour)
	_ = s.putIssue(Issue{ID: "closed", OpenedAt: old, ClosedAt: old.Add(time.Hour), Status: StatusResolvedAuto})
	_ = s.putIssue(Issue{ID: "open", OpenedAt: old, Status: StatusOpen})
	_ = s.putIssue(Issue{ID: "recent", OpenedAt: time.Now(), ClosedAt: time.Now(), Status: StatusDismissed})
	_ = s.addTransitions([]Transition{{At: old, Process: "p"}, {At: time.Now(), Process: "p"}})

	s2, err := OpenStore(dir, 90*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := s2.Issues(Filter{})
	ids := map[string]bool{}
	for _, i := range got {
		ids[i.ID] = true
	}
	if ids["closed"] || !ids["open"] || !ids["recent"] {
		t.Fatalf("ids = %v", ids)
	}
	if h, _ := s2.History("", 0); len(h) != 1 {
		t.Fatalf("history = %+v", h)
	}
}

func TestStoreFilterAndPaging(t *testing.T) {
	s, _ := OpenStore(t.TempDir(), time.Hour)
	base := time.Now().Add(-time.Hour)
	for i := 0; i < 6; i++ {
		is := Issue{ID: string(rune('a' + i)), Kind: KindFatal, Process: "g:p", Message: "boom",
			OpenedAt: base.Add(time.Duration(i) * time.Minute), Status: StatusOpen}
		if i%2 == 1 {
			is.Status, is.ClosedAt = StatusResolvedUser, is.OpenedAt
			is.Process, is.Message = "g:q", "crashed"
		}
		_ = s.putIssue(is)
	}
	ids := func(f Filter) string {
		l, err := s.Issues(f)
		if err != nil {
			t.Fatal(err)
		}
		out := ""
		for _, i := range l {
			out += i.ID
		}
		return out
	}
	for _, c := range []struct {
		f    Filter
		want string
	}{
		{Filter{}, "fedcba"},
		{Filter{Status: "all"}, "fedcba"},
		{Filter{Status: "open"}, "eca"},
		{Filter{Status: "resolved"}, "fdb"},
		{Filter{Limit: 2}, "fe"},
		{Filter{Limit: 2, Before: base.Add(4 * time.Minute)}, "dc"},
		{Filter{Query: "CRASH"}, "fdb"},
		{Filter{Status: "open", Query: "g:p"}, "eca"},
		{Filter{Query: "nomatch"}, ""},
	} {
		if got := ids(c.f); got != c.want {
			t.Errorf("%+v: got %q, want %q", c.f, got, c.want)
		}
	}
}

func openUnreachable(t *testing.T, tr *Tracker, c *clock) {
	t.Helper()
	down := monitor.Update{Snapshot: monitor.Snapshot{At: c.now(), Err: supervisor.ErrUnreachable}}
	tr.Apply(down)
	c.advance(2 * time.Second)
	if opened, _ := tr.Apply(down); len(opened) != 1 {
		t.Fatalf("unreachable not opened: %+v", opened)
	}
}

func firstContact(c *clock) monitor.Update {
	pi := info(supervisor.StateRunning)
	u := steady(c.now(), pi)
	u.Events = []monitor.Event{{Kind: monitor.Added, At: c.now(), Process: "g:w", To: supervisor.StateRunning, Info: pi}}
	return u
}

func TestUnreachableResolvesWithoutReconnectedEvent(t *testing.T) {
	tr, _, c := newTracker(t, nil)
	openUnreachable(t, tr, c)
	c.advance(2 * time.Second)
	_, closed := tr.Apply(firstContact(c))
	if len(closed) != 1 || closed[0].Kind != KindUnreachable || closed[0].Status != StatusResolvedAuto || len(tr.Open()) != 0 {
		t.Fatalf("closed = %+v, open = %+v", closed, tr.Open())
	}
}

func TestReloadedUnreachableResolvesOnFirstContact(t *testing.T) {
	tr, s, c := newTracker(t, nil)
	openUnreachable(t, tr, c)
	tr2 := NewTracker(s, DefaultRules, Programs{}, c.now)
	if len(tr2.Open()) != 1 {
		t.Fatal("not reloaded")
	}
	c.advance(time.Second)
	if _, closed := tr2.Apply(firstContact(c)); len(closed) != 1 || len(tr2.Open()) != 0 {
		t.Fatalf("closed = %+v", closed)
	}
	if open, _ := s.Issues(Filter{Status: "open"}); len(open) != 0 {
		t.Fatalf("store still open: %+v", open)
	}
}

func TestSimultaneousIssuesHaveDistinctIDs(t *testing.T) {
	tr, s, c := newTracker(t, nil)
	for i := 0; i < 4; i++ {
		tr.Apply(restartEvent(c.now(), supervisor.StateBackoff))
		c.advance(time.Second)
	}
	u := restartEvent(c.now(), supervisor.StateBackoff)
	u.Events = append(u.Events, monitor.Event{Kind: monitor.StateChanged, At: c.now(), Process: "g:w", From: supervisor.StateBackoff, To: supervisor.StateFatal, Info: info(supervisor.StateFatal)})
	opened, _ := tr.Apply(u)
	if len(opened) != 2 || opened[0].ID == opened[1].ID {
		t.Fatalf("opened = %+v", opened)
	}
	stored, _ := s.Issues(Filter{Status: "open"})
	if len(tr.Open()) != 2 || len(stored) != 2 {
		t.Fatalf("open %d, stored %d", len(tr.Open()), len(stored))
	}
}

func TestStorePagesIssuesOpenedTogether(t *testing.T) {
	s, _ := OpenStore(t.TempDir(), time.Hour)
	at := time.Now().Add(-time.Minute)
	for _, id := range []string{"c", "a", "e", "b", "d"} {
		_ = s.putIssue(Issue{ID: id, Kind: KindFatal, Process: "g:" + id, OpenedAt: at, Status: StatusOpen})
	}
	var got []string
	f := Filter{Limit: 2}
	for page := 0; page < 5; page++ {
		l, err := s.Issues(f)
		if err != nil {
			t.Fatal(err)
		}
		if len(l) == 0 {
			break
		}
		for _, is := range l {
			got = append(got, is.ID)
		}
		last := l[len(l)-1]
		// The frontend's cursor has millisecond precision.
		f.Before, f.BeforeID = time.UnixMilli(last.OpenedAt.UnixMilli()), last.ID
	}
	if strings.Join(got, "") != "edcba" {
		t.Fatalf("paged %v, want each of e d c b a once", got)
	}
}

func TestOpenIssuesOrderedLikeTheStore(t *testing.T) {
	tr, s, c := newTracker(t, nil)
	for _, p := range []string{"a", "b", "c"} {
		pi := supervisor.ProcessInfo{Name: p, Group: "g", State: supervisor.StateFatal}
		tr.Apply(monitor.Update{
			Snapshot: monitor.Snapshot{At: c.now(), Reachable: true},
			Events:   []monitor.Event{{Kind: monitor.Added, At: c.now(), Process: "g:" + p, To: supervisor.StateFatal, Info: pi}},
		})
	}
	stored, _ := s.Issues(Filter{Status: "open"})
	open := tr.Open()
	if len(open) != 3 || len(stored) != 3 {
		t.Fatalf("open %d, stored %d", len(open), len(stored))
	}
	for i := range open {
		if open[i].ID != stored[i].ID {
			t.Fatalf("tracker order %v differs from store order %v", open, stored)
		}
	}
}

// newTrackerReason is newTrackerAuto whose exit lookup answers r, or no
// reason when ok is false. calls counts the lookups.
func newTrackerReason(t *testing.T, autorestart string, r ExitReason, ok bool) (tr *Tracker, c *clock, calls *int) {
	t.Helper()
	tr, c, calls, _ = newTrackerReads(t, autorestart, r, ok)
	return tr, c, calls
}

// newTrackerReads is newTrackerReason that also counts reads of the log
// (ExitReasons calls).
func newTrackerReads(t *testing.T, autorestart string, r ExitReason, ok bool) (tr *Tracker, c *clock, calls, reads *int) {
	t.Helper()
	s, err := OpenStore(t.TempDir(), 90*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	c = &clock{t: time.Now()}
	calls, reads = new(int), new(int)
	p := programs(nil, autorestart)
	p.ExitReasons = func() ExitLookup {
		*reads++
		return func(process string, pi supervisor.ProcessInfo) (ExitReason, bool) {
			*calls++
			if !strings.HasPrefix(process, "g:") || "g:"+pi.Name != process {
				t.Errorf("exit lookup(%q, %+v)", process, pi)
			}
			return r, ok
		}
	}
	return NewTracker(s, DefaultRules, p, c.now), c, calls, reads
}

// restartsNobodyAskedFor are the polls that show supervisord restarted a
// RUNNING process.
var restartsNobodyAskedFor = []struct {
	name string
	u    func(time.Time) monitor.Update
}{
	{"RUNNING to STARTING", func(at time.Time) monitor.Update { return crashChange(at, supervisor.StateStarting) }},
	{"RUNNING to BACKOFF", func(at time.Time) monitor.Update { return crashChange(at, supervisor.StateBackoff) }},
	{"restarted RUNNING", func(at time.Time) monitor.Update { return restartEvent(at, supervisor.StateRunning) }},
	{"restarted RUNNING to STARTING", func(at time.Time) monitor.Update {
		return restartFrom(at, supervisor.StateRunning, supervisor.StateStarting)
	}},
}

var autorestarts = []string{"", "unexpected", "true", "false"}

func TestRestartWithExpectedReasonOpensNothing(t *testing.T) {
	planned := ExitReason{Desc: "exit status 0", Status: 0, Expected: true}
	for _, tc := range restartsNobodyAskedFor {
		for _, auto := range autorestarts {
			t.Run(tc.name+" autorestart="+auto, func(t *testing.T) {
				tr, c, calls := newTrackerReason(t, auto, planned, true)
				if opened, _ := tr.Apply(tc.u(c.now())); len(opened) != 0 {
					t.Fatalf("opened = %+v", opened)
				}
				if *calls != 1 {
					t.Errorf("ExitReason called %d times, want 1", *calls)
				}
			})
		}
	}
	// Planned exits still count toward a crash loop.
	tr, c, _ := newTrackerReason(t, "true", planned, true)
	var opened []Issue
	for i := 0; i < 5; i++ {
		o, _ := tr.Apply(restartEvent(c.now(), supervisor.StateRunning))
		opened = append(opened, o...)
		c.advance(3 * time.Second)
	}
	if len(opened) != 1 || opened[0].Kind != KindCrashLoop {
		t.Fatalf("five planned restarts opened %+v", opened)
	}
}

func TestRestartWithUnexpectedReasonOpensUnderAutorestartTrue(t *testing.T) {
	crash := ExitReason{Desc: "exit status 1", Status: 1}
	for _, tc := range restartsNobodyAskedFor {
		for _, auto := range autorestarts {
			t.Run(tc.name+" autorestart="+auto, func(t *testing.T) {
				tr, c, _ := newTrackerReason(t, auto, crash, true)
				opened, _ := tr.Apply(tc.u(c.now()))
				if len(opened) != 1 || opened[0].Kind != KindUnexpectedExit || opened[0].Message != "Exited with status 1 and was restarted by supervisord" {
					t.Fatalf("opened = %+v", opened)
				}
				if want := c.now().Add(-time.Second); !opened[0].OpenedAt.Equal(want) {
					t.Errorf("OpenedAt = %v, want the stop time %v", opened[0].OpenedAt, want)
				}
			})
		}
	}
}

func TestRestartSignalMessage(t *testing.T) {
	for _, desc := range []string{"terminated by SIGSEGV", "terminated by SIGSEGV (core dumped)"} {
		tr, c, _ := newTrackerReason(t, "true", ExitReason{Desc: desc, Status: -1}, true)
		opened, _ := tr.Apply(restartEvent(c.now(), supervisor.StateRunning))
		if len(opened) != 1 || opened[0].Message != "Was terminated by SIGSEGV and restarted by supervisord" {
			t.Fatalf("%s: opened = %+v", desc, opened)
		}
	}
}

func TestRestartWithoutReasonFallsBackToR15(t *testing.T) {
	for _, tc := range restartsNobodyAskedFor {
		t.Run(tc.name, func(t *testing.T) {
			tr, c, calls := newTrackerReason(t, "", ExitReason{}, false)
			opened, _ := tr.Apply(tc.u(c.now()))
			if len(opened) != 1 || opened[0].Message != "Exited and was restarted by supervisord" {
				t.Fatalf("autorestart=unexpected: opened = %+v", opened)
			}
			if *calls != 1 {
				t.Errorf("ExitReason called %d times, want 1", *calls)
			}
			tr, c, _ = newTrackerReason(t, "true", ExitReason{}, false)
			if opened, _ := tr.Apply(tc.u(c.now())); len(opened) != 0 {
				t.Fatalf("autorestart=true: opened = %+v", opened)
			}
		})
	}
}

func TestStartupRetryNeverOpensUnexpectedExit(t *testing.T) {
	crash := ExitReason{Desc: "exit status 1", Status: 1}
	retries := [][2]supervisor.ProcessState{
		{supervisor.StateStarting, supervisor.StateStarting},
		{supervisor.StateStarting, supervisor.StateRunning},
		{supervisor.StateBackoff, supervisor.StateBackoff},
		{supervisor.StateBackoff, supervisor.StateStarting},
		{supervisor.StateBackoff, supervisor.StateRunning},
	}
	for _, auto := range autorestarts {
		for _, r := range retries {
			tr, c, calls := newTrackerReason(t, auto, crash, true)
			if opened, _ := tr.Apply(restartFrom(c.now(), r[0], r[1])); len(opened) != 0 {
				t.Errorf("autorestart=%q %v→%v opened %+v", auto, r[0], r[1], opened)
			}
			if *calls != 0 {
				t.Errorf("autorestart=%q %v→%v: ExitReason called %d times", auto, r[0], r[1], *calls)
			}
		}
	}
	// They still count toward a crash loop.
	tr, c, _ := newTrackerReason(t, "", crash, true)
	for i := 0; i < 5; i++ {
		tr.Apply(restartFrom(c.now(), supervisor.StateStarting, supervisor.StateStarting))
		c.advance(2 * time.Second)
	}
	if open := tr.Open(); len(open) != 1 || open[0].Kind != KindCrashLoop {
		t.Fatalf("open = %+v", open)
	}
}

func TestExitReasonOnlyAskedForUnaskedRestarts(t *testing.T) {
	tr, c, calls := newTrackerReason(t, "true", ExitReason{Desc: "exit status 1", Status: 1}, true)
	run := info(supervisor.StateRunning)
	tr.Apply(change(c.now(), supervisor.StateStarting, supervisor.StateRunning, run))
	for i := 0; i < 3; i++ {
		c.advance(time.Second)
		tr.Apply(steady(c.now(), run))
	}
	tr.Expect(func(p string) bool { return p == "g:w" }, c.now().Add(time.Minute))
	if opened, _ := tr.Apply(restartEvent(c.now(), supervisor.StateRunning)); len(opened) != 0 {
		t.Fatalf("the user's restart opened %+v", opened)
	}
	if *calls != 0 {
		t.Errorf("exit lookup called %d times for polls and a user restart", *calls)
	}
	// With an unexpected_exit already open there is no issue to open, but
	// the reason still decides whether the restart counts toward a crash
	// loop (a stop someone asked for does not), so each restart asks.
	c.advance(2 * time.Minute)
	tr.Apply(restartEvent(c.now(), supervisor.StateRunning))
	c.advance(5 * time.Second)
	tr.Apply(restartEvent(c.now(), supervisor.StateRunning))
	if *calls != 2 {
		t.Errorf("exit lookup called %d times for two crashes with an issue open, want 2", *calls)
	}
}

// R26: supervisord logs "stopped:" for a stop someone asked for, e.g.
// supervisorctl restart, or stop and start from another client. Such a
// restart opens nothing, does not count toward a crash loop, and is noted
// in history.
func TestDeliberateRestartOpensNothingAndCountsNoCrashLoop(t *testing.T) {
	stopped := ExitReason{Desc: "terminated by SIGTERM", Status: -1, Deliberate: true}
	for _, tc := range restartsNobodyAskedFor {
		for _, auto := range autorestarts {
			t.Run(tc.name+" autorestart="+auto, func(t *testing.T) {
				tr, c, calls := newTrackerReason(t, auto, stopped, true)
				var opened []Issue
				for i := 0; i < 6; i++ {
					o, _ := tr.Apply(tc.u(c.now()))
					opened = append(opened, o...)
					c.advance(2 * time.Second)
				}
				if len(opened) != 0 {
					t.Fatalf("deliberate restarts opened %+v", opened)
				}
				if *calls != 6 {
					t.Errorf("exit lookup called %d times, want 6", *calls)
				}
				h, _ := tr.store.History("g:w", 0)
				if len(h) == 0 || h[0].Note != "Restarted (stopped on request)" {
					t.Errorf("history = %+v, want the restart noted as stopped on request", h)
				}
			})
		}
	}
}

// A start from STOPPED is always someone's doing (supervisord never starts
// a stopped process by itself), so it does not count toward a crash loop.
func TestStartFromStoppedCountsNoCrashLoop(t *testing.T) {
	tr, _, c := newTracker(t, nil)
	for i := 0; i < 6; i++ {
		tr.Apply(change(c.now(), supervisor.StateStopped, supervisor.StateStarting, info(0)))
		c.advance(time.Second)
		tr.Apply(restartEvent(c.now(), supervisor.StateStopped)) // started and stopped between polls
		c.advance(time.Second)
	}
	if open := tr.Open(); len(open) != 0 {
		t.Fatalf("starts from STOPPED opened %+v", open)
	}
}

// After a daemon restart every process restarts in one update; the log is
// read once for all of them.
func TestOneLogReadPerApply(t *testing.T) {
	for _, reason := range []ExitReason{
		{Desc: "terminated by SIGTERM", Status: -1, Deliberate: true},
		{Desc: "exit status 1", Status: 1},
	} {
		tr, c, calls, reads := newTrackerReads(t, "", reason, true)
		at := c.now()
		u := monitor.Update{Snapshot: monitor.Snapshot{At: at, Reachable: true}}
		for _, name := range []string{"a", "b", "c", "d"} {
			pi := supervisor.ProcessInfo{Name: name, Group: "g", State: supervisor.StateRunning, PID: len(name) + 100, Start: at}
			u.Snapshot.Processes = append(u.Snapshot.Processes, pi)
			u.Events = append(u.Events, monitor.Event{Kind: monitor.Restarted, At: at, Process: "g:" + name,
				From: supervisor.StateRunning, To: supervisor.StateRunning, Info: pi})
		}
		tr.Apply(u)
		if *reads != 1 || *calls != 4 {
			t.Errorf("%+v: %d log reads and %d lookups for 4 restarts, want 1 and 4", reason, *reads, *calls)
		}
		// An update with no restart nobody asked for reads nothing.
		tr.Apply(steady(c.now(), info(supervisor.StateRunning)))
		if *reads != 1 {
			t.Errorf("%d log reads after a quiet poll, want 1", *reads)
		}
	}
}

func TestClosedByUserSecondGranularity(t *testing.T) {
	tr, s, c := newTracker(t, nil)
	sec := c.now().Truncate(time.Second).Add(time.Hour) // T, a whole second
	c.t = sec.Add(-time.Minute)
	opened, _ := tr.Apply(added(c.now(), fatalInfo(c.now().Add(-10*time.Second), c.now().Add(-5*time.Second))))
	if len(opened) != 1 {
		t.Fatalf("opened = %+v", opened)
	}
	c.t = sec.Add(400 * time.Millisecond)
	_ = tr.Dismiss(opened[0].ID)

	// It fails again at T+0.9s; supervisord reports the stop as T, before
	// the dismissal at T+0.4s. It is a new failure all the same.
	c.t = sec.Add(900 * time.Millisecond)
	again := fatalInfo(sec.Add(-2*time.Second), sec)
	opened, _ = tr.Apply(change(c.now(), supervisor.StateBackoff, supervisor.StateFatal, again))
	if len(opened) != 1 {
		t.Fatalf("a failure in the second of the dismissal: opened %+v", opened)
	}
	// Dismissed a second later, the same failure stays closed on relaunch.
	c.t = sec.Add(1200 * time.Millisecond)
	_ = tr.Dismiss(opened[0].ID)
	tr2 := NewTracker(s, DefaultRules, programs(nil, ""), c.now)
	if o, _ := tr2.Apply(added(c.now(), again)); len(o) != 0 {
		t.Fatalf("the dismissed failure reopened: %+v", o)
	}
}

func TestStoreTornLineKeepsNextRecord(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	_ = s.putIssue(Issue{ID: "a", OpenedAt: now, Status: StatusOpen})
	_ = s.addTransitions([]Transition{{At: now, Process: "p", To: "RUNNING"}})
	for _, name := range []string{"issues.jsonl", "history.jsonl"} {
		f, err := os.OpenFile(filepath.Join(dir, name), os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.WriteString(`{"id":"torn","kin`) // a crash mid-write
		_ = f.Close()
	}
	_ = s.putIssue(Issue{ID: "b", OpenedAt: now, Status: StatusOpen})
	_ = s.addTransitions([]Transition{{At: now, Process: "p", To: "STOPPED"}})

	got, err := s.Issues(Filter{})
	if err != nil || len(got) != 2 {
		t.Fatalf("issues = %+v, %v; want a and b", got, err)
	}
	if h, err := s.History("p", 0); err != nil || len(h) != 2 || h[0].To != "STOPPED" {
		t.Fatalf("history = %+v, %v", h, err)
	}
}

func TestStoreOversizeRecordSkipped(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	huge, _ := json.Marshal(Issue{ID: "huge", Message: strings.Repeat("x", 5<<20), OpenedAt: now, Status: StatusOpen})
	small, _ := json.Marshal(Issue{ID: "small", OpenedAt: now, Status: StatusOpen})
	body := append(append(append(huge, '\n'), small...), '\n')
	if err := os.WriteFile(filepath.Join(dir, "issues.jsonl"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := OpenStore(dir, time.Hour)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	got, err := s.Issues(Filter{})
	if err != nil || len(got) != 1 || got[0].ID != "small" {
		t.Fatalf("issues = %d records (%v); want only small", len(got), err)
	}
}

func TestStoreCounts(t *testing.T) {
	s, err := OpenStore(t.TempDir(), 90*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	put := func(id string, st Status) {
		t.Helper()
		if err := s.putIssue(Issue{ID: id, Kind: KindFatal, Process: id, OpenedAt: now, Status: st}); err != nil {
			t.Fatal(err)
		}
	}
	if o, c, err := s.Counts(); err != nil || o != 0 || c != 0 {
		t.Fatalf("empty store: %d, %d, %v", o, c, err)
	}
	put("a", StatusOpen)
	put("b", StatusOpen)
	put("c", StatusResolvedAuto)
	put("d", StatusDismissed)
	put("b", StatusResolvedUser) // a later record replaces b
	o, c, err := s.Counts()
	if err != nil || o != 1 || c != 3 {
		t.Errorf("Counts = open %d, closed %d, %v; want 1, 3", o, c, err)
	}
}

func TestSetRulesAppliesToLaterRestarts(t *testing.T) {
	tr, _, c := newTracker(t, nil)
	r := DefaultRules
	r.CrashLoopRestarts, r.CrashLoopWindow = 3, 2*time.Minute
	tr.SetRules(r)
	if got := tr.Rules(); got != r {
		t.Errorf("Rules = %+v, want %+v", got, r)
	}
	var loops []Issue
	for range 3 {
		opened, _ := tr.Apply(change(c.now(), supervisor.StateBackoff, supervisor.StateStarting, info(0)))
		loops = append(loops, opened...)
		c.advance(10 * time.Second)
	}
	if len(loops) != 1 || loops[0].Message != "Restarted 3 times in 2 minutes" {
		t.Fatalf("loops = %+v", loops)
	}
}

func TestStoreCompactWithNewRetention(t *testing.T) {
	s, err := OpenStore(t.TempDir(), 90*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-10 * 24 * time.Hour)
	_ = s.putIssue(Issue{ID: "closed", OpenedAt: old, ClosedAt: old, Status: StatusResolvedAuto})
	_ = s.putIssue(Issue{ID: "open", OpenedAt: old, Status: StatusOpen})
	_ = s.addTransitions([]Transition{{At: old, Process: "p"}, {At: time.Now(), Process: "p"}})
	if err := s.Compact(7 * 24 * time.Hour); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Issues(Filter{})
	if len(got) != 1 || got[0].ID != "open" {
		t.Errorf("issues after Compact = %+v, want only the open one", got)
	}
	if h, _ := s.History("", 0); len(h) != 1 {
		t.Errorf("history after Compact = %+v, want 1", h)
	}
}

// M4: after switching to another supervisord, issues of processes it does
// not run can never close themselves, so they close as soon as its first
// reachable snapshot shows they are absent.
func TestCloseAbsent(t *testing.T) {
	tr, s, c := newTracker(t, nil)
	other := supervisor.ProcessInfo{Name: "x", Group: "old", State: supervisor.StateFatal}
	tr.Apply(added(c.now(), other))
	tr.Apply(added(c.now(), fatalInfo(c.now(), c.now())))
	for i := 0; i < 2; i++ {
		tr.Apply(down(c))
		c.advance(time.Second)
	}
	if n := len(tr.Open()); n != 3 {
		t.Fatalf("open = %+v, want old:x fatal, g:w fatal and unreachable", tr.Open())
	}
	c.advance(time.Minute)
	closed := tr.CloseAbsent([]supervisor.ProcessInfo{info(supervisor.StateRunning)}, "not in this supervisord")
	if len(closed) != 1 || closed[0].Process != "old:x" || closed[0].Status != StatusResolvedAuto || closed[0].Note != "not in this supervisord" {
		t.Fatalf("closed = %+v", closed)
	}
	open := tr.Open()
	if len(open) != 2 {
		t.Errorf("open = %+v, want g:w and unreachable left", open)
	}
	stored, _ := s.Issues(Filter{})
	for _, is := range stored {
		if is.Process == "old:x" && (is.Status != StatusResolvedAuto || is.ClosedAt.IsZero()) {
			t.Errorf("stored %+v", is)
		}
	}
}

func TestStoreFilterKindAndDates(t *testing.T) {
	s, _ := OpenStore(t.TempDir(), time.Hour)
	base := time.Now().Add(-time.Hour).Truncate(time.Millisecond)
	kinds := []Kind{KindFatal, KindUnexpectedExit, KindFatal, KindCrashLoop}
	for i, k := range kinds {
		_ = s.putIssue(Issue{ID: string(rune('a' + i)), Kind: k, OpenedAt: base.Add(time.Duration(i) * time.Minute), Status: StatusOpen})
	}
	ids := func(f Filter) string {
		l, err := s.Issues(f)
		if err != nil {
			t.Fatal(err)
		}
		out := ""
		for _, i := range l {
			out += i.ID
		}
		return out
	}
	for _, c := range []struct {
		f    Filter
		want string
	}{
		{Filter{Kind: KindFatal}, "ca"},
		{Filter{Kind: KindCrashLoop}, "d"},
		{Filter{Kind: KindUnreachable}, ""},
		{Filter{From: base.Add(time.Minute)}, "dcb"},
		{Filter{To: base.Add(2 * time.Minute)}, "ba"},                              // To is exclusive
		{Filter{From: base.Add(time.Minute), To: base.Add(3 * time.Minute)}, "cb"}, // From inclusive
		{Filter{Kind: KindFatal, From: base.Add(time.Minute)}, "c"},
	} {
		if got := ids(c.f); got != c.want {
			t.Errorf("%+v: got %q, want %q", c.f, got, c.want)
		}
	}
}

func TestStoreStatsWindows(t *testing.T) {
	s, _ := OpenStore(t.TempDir(), 365*24*time.Hour)
	now := time.Now().Truncate(time.Millisecond)
	put := func(id string, age time.Duration, st Status) {
		is := Issue{ID: id, OpenedAt: now.Add(-age), Status: st}
		if st != StatusOpen {
			is.ClosedAt = now
		}
		_ = s.putIssue(is)
	}
	put("a", time.Hour, StatusOpen)
	put("b", 24*time.Hour, StatusResolvedUser)               // exactly 24h: inside
	put("c", 24*time.Hour+time.Millisecond, StatusDismissed) // just outside 24h
	put("d", 7*24*time.Hour, StatusResolvedAuto)             // exactly 7d: inside
	put("e", 30*24*time.Hour+time.Second, StatusOpen)        // outside 30d
	got, err := s.Stats(now)
	if err != nil {
		t.Fatal(err)
	}
	want := Stats{Total: 5, Open: 2, Resolved: 3, Last24h: 2, Last7d: 4, Last30d: 4}
	if got != want {
		t.Errorf("Stats = %+v, want %+v", got, want)
	}
	if z, _ := (&Store{dir: t.TempDir()}).Stats(now); z != (Stats{}) {
		t.Errorf("empty store: %+v", z)
	}
}

func TestStoreDeleteSkipsOpenAndSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	s, _ := OpenStore(dir, time.Hour)
	now := time.Now()
	_ = s.putIssue(Issue{ID: "open", OpenedAt: now, Status: StatusOpen})
	_ = s.putIssue(Issue{ID: "r1", OpenedAt: now, ClosedAt: now, Status: StatusResolvedUser})
	_ = s.putIssue(Issue{ID: "r2", OpenedAt: now, ClosedAt: now, Status: StatusDismissed})
	_ = s.putIssue(Issue{ID: "r3", OpenedAt: now, ClosedAt: now, Status: StatusResolvedAuto})
	_ = s.addTransitions([]Transition{{At: now, Process: "p"}})

	n, err := s.Delete("open", "r1", "r2", "nope")
	if err != nil || n != 2 {
		t.Fatalf("Delete = %d, %v", n, err)
	}
	if n, err := s.Delete("nope", "r1"); err != nil || n != 0 {
		t.Fatalf("second Delete = %d, %v", n, err)
	}
	if n, err := s.Delete(); err != nil || n != 0 {
		t.Fatalf("empty Delete = %d, %v", n, err)
	}
	s2, _ := OpenStore(dir, time.Hour)
	got, _ := s2.Issues(Filter{})
	ids := map[string]bool{}
	for _, i := range got {
		ids[i.ID] = true
	}
	if len(got) != 2 || !ids["open"] || !ids["r3"] {
		t.Fatalf("after reopen: %+v", got)
	}
	if h, _ := s2.History("", 0); len(h) != 1 {
		t.Fatalf("history touched: %+v", h)
	}
}

func TestResolveManyMixed(t *testing.T) {
	tr, s, c := newTracker(t, nil)
	a := fatalInfo(c.now().Add(-time.Minute), c.now().Add(-50*time.Second))
	opened, _ := tr.Apply(added(c.now(), a))
	if len(opened) != 1 {
		t.Fatalf("opened = %+v", opened)
	}
	c.advance(time.Minute)
	n, err := tr.ResolveMany([]string{opened[0].ID, "unknown"})
	if err != nil || n != 1 {
		t.Fatalf("ResolveMany = %d, %v", n, err)
	}
	if n, _ := tr.ResolveMany([]string{opened[0].ID}); n != 0 {
		t.Fatalf("already closed resolved again: %d", n)
	}
	got, _ := s.Issues(Filter{})
	if len(got) != 1 || got[0].Status != StatusResolvedUser || len(tr.Open()) != 0 {
		t.Fatalf("store = %+v", got)
	}
	// Suppression applies: the same failure does not reopen.
	if o, _ := tr.Apply(added(c.now(), a)); len(o) != 0 {
		t.Fatalf("reopened after ResolveMany: %+v", o)
	}
}

// A deleted resolved issue must not come back as a new one while the same
// failure is still on show.
func TestDeletedIssueDoesNotReopenSameFailure(t *testing.T) {
	tr, s, c := newTracker(t, nil)
	failed := fatalInfo(c.now().Add(-time.Minute), c.now().Add(-50*time.Second))
	opened, _ := tr.Apply(added(c.now(), failed))
	if len(opened) != 1 {
		t.Fatalf("opened = %+v", opened)
	}
	c.advance(time.Minute)
	if n, _ := tr.ResolveMany([]string{opened[0].ID}); n != 1 {
		t.Fatal("not resolved")
	}
	if n, err := s.Delete(opened[0].ID); err != nil || n != 1 {
		t.Fatalf("Delete = %d, %v", n, err)
	}
	c.advance(time.Minute)
	if o, _ := tr.Apply(added(c.now(), failed)); len(o) != 0 {
		t.Fatalf("the deleted failure reopened: %+v", o)
	}
	// A genuinely new failure still opens.
	again := fatalInfo(c.now().Add(-10*time.Second), c.now().Add(-5*time.Second))
	if o, _ := tr.Apply(change(c.now(), supervisor.StateBackoff, supervisor.StateFatal, again)); len(o) != 1 {
		t.Fatalf("a new failure: opened %+v", o)
	}
}

func TestMarkNotified(t *testing.T) {
	tr, s, c := newTracker(t, nil)
	opened, _ := tr.Apply(change(c.now(), supervisor.StateBackoff, supervisor.StateFatal, info(0)))
	if len(opened) != 1 || opened[0].Notified {
		t.Fatalf("opened = %+v", opened)
	}
	id := opened[0].ID
	if err := tr.MarkNotified(id); err != nil {
		t.Fatal(err)
	}
	if open := tr.Open(); len(open) != 1 || !open[0].Notified {
		t.Fatalf("open = %+v", open)
	}
	got, _ := s.Issues(Filter{})
	if len(got) != 1 || !got[0].Notified || got[0].Status != StatusOpen {
		t.Fatalf("store = %+v", got)
	}
	// A reopened store keeps it.
	s2, err := OpenStore(s.dir, 90*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := s2.Issues(Filter{}); len(got) != 1 || !got[0].Notified {
		t.Fatalf("reopened store = %+v", got)
	}
	// Closing keeps it, and a closed or unknown issue is not open.
	if err := tr.Resolve(id); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Issues(Filter{}); len(got) != 1 || !got[0].Notified || got[0].Status != StatusResolvedUser {
		t.Fatalf("after resolve = %+v", got)
	}
	if err := tr.MarkNotified(id); !errors.Is(err, ErrNotOpen) {
		t.Errorf("MarkNotified(closed) = %v, want ErrNotOpen", err)
	}
	if err := tr.MarkNotified("unknown"); !errors.Is(err, ErrNotOpen) {
		t.Errorf("MarkNotified(unknown) = %v, want ErrNotOpen", err)
	}
}

// A deleted user-closed issue keeps suppressing the same failure after
// Visor relaunches, so it neither reopens nor notifies again.
func TestDeletedIssueStaysSuppressedAcrossLaunches(t *testing.T) {
	for _, closeIt := range []func(*Tracker, string) error{(*Tracker).Dismiss, (*Tracker).Resolve} {
		tr, s, c := newTracker(t, nil)
		failed := fatalInfo(c.now().Add(-time.Minute), c.now().Add(-50*time.Second))
		opened, _ := tr.Apply(added(c.now(), failed))
		if len(opened) != 1 {
			t.Fatalf("opened = %+v", opened)
		}
		c.advance(time.Minute)
		if err := closeIt(tr, opened[0].ID); err != nil {
			t.Fatal(err)
		}
		if n, err := s.Delete(opened[0].ID); err != nil || n != 1 {
			t.Fatalf("Delete = %d, %v", n, err)
		}
		if got, _ := s.Issues(Filter{}); len(got) != 0 {
			t.Fatalf("deleted issue still listed: %+v", got)
		}
		if st, _ := s.Stats(c.now()); st.Total != 0 {
			t.Fatalf("deleted issue still counted: %+v", st)
		}

		// Visor relaunches; supervisord still shows the same failure.
		c.advance(time.Hour)
		s2, err := OpenStore(s.dir, 90*24*time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		tr2 := NewTracker(s2, DefaultRules, programs(nil, ""), c.now)
		if o, _ := tr2.Apply(added(c.now(), failed)); len(o) != 0 {
			t.Fatalf("the deleted failure reopened after a relaunch: %+v", o)
		}
		// A genuinely new failure still opens.
		again := fatalInfo(c.now().Add(-10*time.Second), c.now().Add(-5*time.Second))
		if o, _ := tr2.Apply(change(c.now(), supervisor.StateBackoff, supervisor.StateFatal, again)); len(o) != 1 {
			t.Fatalf("a new failure: opened %+v", o)
		}
	}
}

// Deleting an issue that closed by itself keeps nothing: it never
// suppressed anything.
func TestDeletedAutoResolvedIssueKeepsNoSuppression(t *testing.T) {
	dir := t.TempDir()
	s, _ := OpenStore(dir, time.Hour)
	now := time.Now()
	_ = s.putIssue(Issue{ID: "a", Kind: KindFatal, Process: "g:w", OpenedAt: now, ClosedAt: now, Status: StatusResolvedAuto})
	if n, err := s.Delete("a"); err != nil || n != 1 {
		t.Fatalf("Delete = %d, %v", n, err)
	}
	if sup, err := s.suppressions(); err != nil || len(sup) != 0 {
		t.Fatalf("suppressions = %+v, %v", sup, err)
	}
}

// Suppressions of deleted issues go once the issue itself would have:
// after the retention window.
func TestSuppressionsCompactWithRetention(t *testing.T) {
	dir := t.TempDir()
	s, _ := OpenStore(dir, 90*24*time.Hour)
	old := time.Now().Add(-91 * 24 * time.Hour)
	recent := time.Now().Add(-time.Hour)
	_ = s.putIssue(Issue{ID: "old", Kind: KindFatal, Process: "g:old", OpenedAt: old, ClosedAt: old, Status: StatusResolvedUser})
	_ = s.putIssue(Issue{ID: "new", Kind: KindFatal, Process: "g:new", OpenedAt: recent, ClosedAt: recent, Status: StatusDismissed})
	_ = s.putIssue(Issue{ID: "newer", Kind: KindFatal, Process: "g:new", OpenedAt: recent, ClosedAt: recent.Add(time.Minute), Status: StatusResolvedUser})
	if n, err := s.Delete("old", "new", "newer"); err != nil || n != 3 {
		t.Fatalf("Delete = %d, %v", n, err)
	}
	s2, err := OpenStore(dir, 90*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	sup, err := s2.suppressions()
	if err != nil {
		t.Fatal(err)
	}
	// The old one is past retention; the two for g:new fold into the latest.
	if len(sup) != 1 || sup[0].Process != "g:new" || !sup[0].ClosedAt.Equal(recent.Add(time.Minute)) {
		t.Fatalf("suppressions after compaction = %+v", sup)
	}
}

func TestStoreFilterNotified(t *testing.T) {
	s, _ := OpenStore(t.TempDir(), time.Hour)
	now := time.Now()
	_ = s.putIssue(Issue{ID: "a", OpenedAt: now, Status: StatusOpen, Notified: true})
	_ = s.putIssue(Issue{ID: "b", OpenedAt: now, Status: StatusOpen})
	_ = s.putIssue(Issue{ID: "c", OpenedAt: now, ClosedAt: now, Status: StatusResolvedUser, Notified: true})
	got, err := s.Issues(Filter{Notified: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "c" || got[1].ID != "a" {
		t.Fatalf("notified only = %+v", got)
	}
	if got, _ := s.Issues(Filter{}); len(got) != 3 {
		t.Fatalf("no filter = %+v", got)
	}
}
