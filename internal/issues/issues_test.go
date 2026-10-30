package issues

import (
	"os"
	"path/filepath"
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
	s, err := OpenStore(t.TempDir(), 90*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	c := &clock{t: time.Now()}
	tr := NewTracker(s, DefaultRules,
		func(p string) string { return "/conf/" + p + ".conf" },
		func(string) []int { return expected }, c.now)
	return tr, s, c
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

func restartEvent(at time.Time, state supervisor.ProcessState, exit int) monitor.Update {
	pi := info(state)
	pi.ExitStatus = exit
	pi.PID = int(at.Unix())
	u := steady(at, pi)
	u.Events = []monitor.Event{{Kind: monitor.Restarted, At: at, Process: "g:w", From: state, To: state, Info: pi}}
	return u
}

func TestUnexpectedExitFromRestarted(t *testing.T) {
	tr, _, c := newTracker(t, nil)
	if opened, _ := tr.Apply(restartEvent(c.now(), supervisor.StateRunning, 0)); len(opened) != 0 {
		t.Fatalf("clean restart opened %+v", opened)
	}
	opened, _ := tr.Apply(restartEvent(c.now(), supervisor.StateRunning, 3))
	if len(opened) != 1 || opened[0].Kind != KindUnexpectedExit || opened[0].Message != "Exited with status 3" {
		t.Fatalf("opened = %+v", opened)
	}
}

func TestBackoffRetriesDoNotOpenUnexpectedExit(t *testing.T) {
	tr, _, c := newTracker(t, nil)
	if opened, _ := tr.Apply(restartEvent(c.now(), supervisor.StateBackoff, 1)); len(opened) != 0 {
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
		tr.Apply(restartEvent(c.now(), supervisor.StateRunning, 0))
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
	tr2 := NewTracker(s, DefaultRules, nil, nil, c.now)
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
