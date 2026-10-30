package monitor

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/touhidurabir/visor/internal/supervisor"
	"github.com/touhidurabir/visor/internal/supervisor/supervisortest"
)

// fakeSource returns procs(n) on the nth poll (1-based). With gate set,
// each poll first waits for a value on gate, so a test can step the loop.
type fakeSource struct {
	gate  chan struct{}
	procs func(n int) ([]supervisor.ProcessInfo, error)

	mu    sync.Mutex
	calls int
}

func (f *fakeSource) GetState(context.Context) (supervisor.SupervisorState, error) {
	return supervisor.SupervisorState{Code: 1, Name: "RUNNING"}, nil
}

func (f *fakeSource) GetAllProcessInfo(ctx context.Context) ([]supervisor.ProcessInfo, error) {
	if f.gate != nil {
		select {
		case <-f.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	f.mu.Lock()
	f.calls++
	n := f.calls
	f.mu.Unlock()
	return f.procs(n)
}

func (f *fakeSource) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// start runs m until the returned stop is called or the test ends.
func start(t *testing.T, m *Monitor) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		m.Run(ctx)
		close(done)
	}()
	stop = func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("Run did not return after cancel")
		}
	}
	t.Cleanup(stop)
	return stop
}

func recv(t *testing.T, ch <-chan Update, timeout time.Duration) Update {
	t.Helper()
	select {
	case u, ok := <-ch:
		if !ok {
			t.Fatal("subscription closed")
		}
		return u
	case <-time.After(timeout):
		t.Fatalf("no update within %v", timeout)
	}
	return Update{}
}

// step lets one gated poll through.
func step(t *testing.T, gate chan<- struct{}) {
	t.Helper()
	select {
	case gate <- struct{}{}:
	case <-time.After(5 * time.Second):
		t.Fatal("monitor did not poll within 5s")
	}
}

func find(s Snapshot, fullName string) (supervisor.ProcessInfo, bool) {
	for _, p := range s.Processes {
		if p.FullName() == fullName {
			return p, true
		}
	}
	return supervisor.ProcessInfo{}, false
}

func TestNewDefaultsInterval(t *testing.T) {
	for _, d := range []time.Duration{0, -time.Second} {
		if got := New(&fakeSource{}, d).interval; got != 2*time.Second {
			t.Errorf("New(%v).interval = %v, want 2s", d, got)
		}
	}
	if got := New(&fakeSource{}, 0).Latest(); HealthOf(got.Snapshot, nil) != HealthUnknown || got.Events != nil {
		t.Errorf("Latest before Run = %+v, want the zero Update", got)
	}
}

func TestMonitorPollsAndPublishes(t *testing.T) {
	gate := make(chan struct{})
	src := &fakeSource{gate: gate, procs: func(n int) ([]supervisor.ProcessInfo, error) {
		state := supervisor.StateRunning
		if n == 1 {
			state = supervisor.StateStarting
		}
		// Out of order on purpose: snapshots are sorted by FullName.
		return []supervisor.ProcessInfo{proc("web", state), proc("api", supervisor.StateRunning)}, nil
	}}
	m := New(src, 10*time.Millisecond)
	ch, unsub := m.Subscribe()
	defer unsub()
	start(t, m)

	step(t, gate)
	u1 := recv(t, ch, 5*time.Second)
	if !u1.Snapshot.Reachable || u1.Snapshot.At.IsZero() || u1.Snapshot.State.Name != "RUNNING" {
		t.Fatalf("first snapshot = %+v", u1.Snapshot)
	}
	if ps := u1.Snapshot.Processes; len(ps) != 2 || ps[0].Name != "api" || ps[1].Name != "web" {
		t.Errorf("processes not sorted by FullName: %+v", ps)
	}
	if got := kinds(u1.Events); len(got) != 2 || got[0] != Added || got[1] != Added {
		t.Errorf("first update kinds = %v, want [Added Added]", got)
	}

	// A subscriber scribbling on its update must not affect the monitor.
	u1.Snapshot.Processes[1].State = supervisor.StateFatal

	step(t, gate)
	u2 := recv(t, ch, 5*time.Second)
	if len(u2.Events) != 1 {
		t.Fatalf("second update events = %+v, want one StateChanged", u2.Events)
	}
	e := u2.Events[0]
	if e.Kind != StateChanged || e.Process != "web:web" || e.From != supervisor.StateStarting || e.To != supervisor.StateRunning {
		t.Errorf("event = %+v, want web STARTING -> RUNNING", e)
	}
	if l := m.Latest(); !l.Snapshot.At.Equal(u2.Snapshot.At) || len(l.Events) != 1 {
		t.Errorf("Latest = %+v, want the second update", l)
	}
}

func TestMonitorPoke(t *testing.T) {
	src := &fakeSource{procs: func(int) ([]supervisor.ProcessInfo, error) { return nil, nil }}
	m := New(src, time.Hour)
	ch, unsub := m.Subscribe()
	defer unsub()
	start(t, m)
	recv(t, ch, 5*time.Second) // the immediate first poll
	m.Poke()
	recv(t, ch, 5*time.Second)
	if got := src.Calls(); got != 2 {
		t.Errorf("polls = %d, want 2", got)
	}
}

func TestMonitorReportsSourceErrors(t *testing.T) {
	src := &fakeSource{procs: func(n int) ([]supervisor.ProcessInfo, error) {
		if n == 2 {
			return nil, supervisor.ErrUnreachable
		}
		return []supervisor.ProcessInfo{proc("a", supervisor.StateRunning)}, nil
	}}
	m := New(src, time.Hour)
	ch, unsub := m.Subscribe()
	defer unsub()
	start(t, m)
	recv(t, ch, 5*time.Second)

	m.Poke()
	u := recv(t, ch, 5*time.Second)
	if u.Snapshot.Reachable || u.Snapshot.Err == nil || len(u.Snapshot.Processes) != 0 {
		t.Errorf("down snapshot = %+v, want unreachable with an error and no processes", u.Snapshot)
	}
	if got := kinds(u.Events); len(got) != 1 || got[0] != Unreachable {
		t.Errorf("kinds = %v, want [Unreachable]", got)
	}

	// Back up with the same process: Reconnected only, because the monitor
	// diffs against the last processes it saw, not the empty down snapshot.
	m.Poke()
	u = recv(t, ch, 5*time.Second)
	if got := kinds(u.Events); len(got) != 1 || got[0] != Reconnected {
		t.Errorf("kinds = %v, want [Reconnected]", got)
	}
}

// Review Focus 2: a subscriber that never reads must not stall the loop.
// It keeps one update: the latest snapshot, carrying every event it missed.
func TestSlowSubscriberDoesNotBlock(t *testing.T) {
	src := &fakeSource{procs: func(n int) ([]supervisor.ProcessInfo, error) {
		p := proc("flip", supervisor.StateRunning)
		if n%2 == 0 {
			p.State = supervisor.StateStopped
		}
		p.PID = n
		return []supervisor.ProcessInfo{p}, nil
	}}
	m := New(src, time.Millisecond)
	slow, unsubSlow := m.Subscribe()
	defer unsubSlow()
	stop := start(t, m)

	deadline := time.Now().Add(20 * time.Second)
	for src.Calls() <= 50 { // poll 51 has begun, so poll 50 was published
		if time.Now().After(deadline) {
			t.Fatalf("loop stalled after %d polls", src.Calls())
		}
		time.Sleep(time.Millisecond)
	}
	stop()

	u := recv(t, slow, time.Second)
	latest := m.Latest()
	if got, want := u.Snapshot.Processes[0].PID, latest.Snapshot.Processes[0].PID; got != want || want < 50 {
		t.Errorf("slow subscriber holds poll %d, latest is poll %d (want >= 50)", got, want)
	}
	if len(u.Events) < 49 || len(u.Events) > maxPendingEvents {
		t.Fatalf("slow subscriber has %d events, want between 49 and %d", len(u.Events), maxPendingEvents)
	}
	for i := 1; i < len(u.Events); i++ {
		prev, e := u.Events[i-1], u.Events[i]
		if e.Kind != StateChanged || e.From != prev.Info.State || e.Info.PID != prev.Info.PID+1 {
			t.Fatalf("events %d..%d are not consecutive: %+v then %+v", i-1, i, prev, e)
		}
	}
	if last := u.Events[len(u.Events)-1]; last.Info.PID != latest.Snapshot.Processes[0].PID {
		t.Errorf("last event is poll %d, snapshot is poll %d", last.Info.PID, latest.Snapshot.Processes[0].PID)
	}
	if _, ok := <-slow; ok {
		t.Error("subscription still open after Run returned")
	}
}

func TestPendingEventsAreCapped(t *testing.T) {
	m := New(&fakeSource{}, time.Hour)
	ch, unsub := m.Subscribe()
	defer unsub()
	total := maxPendingEvents + 10
	for i := range total {
		m.publish(Update{Snapshot: up(time.Duration(i)), Events: []Event{{Kind: StateChanged, Info: supervisor.ProcessInfo{PID: i}}}})
	}
	u := recv(t, ch, time.Second)
	if len(u.Events) != maxPendingEvents {
		t.Fatalf("events = %d, want %d", len(u.Events), maxPendingEvents)
	}
	if first, last := u.Events[0].Info.PID, u.Events[len(u.Events)-1].Info.PID; first != 10 || last != total-1 {
		t.Errorf("kept events %d..%d, want the newest %d..%d", first, last, 10, total-1)
	}
	if !u.Snapshot.At.Equal(t0.Add(time.Duration(total - 1))) {
		t.Errorf("snapshot at %v, want the latest", u.Snapshot.At)
	}
}

func TestUnsubscribe(t *testing.T) {
	m := New(&fakeSource{}, time.Hour)
	ch, unsub := m.Subscribe()
	unsub()
	unsub() // idempotent
	if _, ok := <-ch; ok {
		t.Error("channel open after unsubscribe")
	}
	m.publish(Update{Snapshot: up(0)}) // must not panic on the closed channel
}

// Not parallel: it counts every goroutine in the process.
func TestMonitorStopsOnCancel(t *testing.T) {
	base := runtime.NumGoroutine()

	src := &fakeSource{procs: func(int) ([]supervisor.ProcessInfo, error) {
		return []supervisor.ProcessInfo{proc("a", supervisor.StateRunning)}, nil
	}}
	m := New(src, 10*time.Millisecond)
	ch, unsub := m.Subscribe()
	defer unsub()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		m.Run(ctx)
		close(done)
	}()
	recv(t, ch, 5*time.Second)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	for range ch { // closed when Run returns; drains the buffered update
	}
	late, _ := m.Subscribe()
	if _, ok := <-late; ok {
		t.Error("Subscribe after Run returned gave an open channel")
	}

	deadline := time.Now().Add(time.Second)
	for runtime.NumGoroutine() > base {
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<16)
			t.Fatalf("goroutines: %d, baseline %d\n%s", runtime.NumGoroutine(), base, buf[:runtime.Stack(buf, true)])
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRunTwicePanics(t *testing.T) {
	m := New(&fakeSource{procs: func(int) ([]supervisor.ProcessInfo, error) { return nil, nil }}, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m.Run(ctx)
	defer func() {
		if recover() == nil {
			t.Error("second Run did not panic")
		}
	}()
	m.Run(ctx)
}

// Starting unreachable and then reaching supervisord is a first contact,
// not a reconnect: Unreachable, then only Added.
func TestMonitorFirstContactIsNotReconnected(t *testing.T) {
	src := &fakeSource{procs: func(n int) ([]supervisor.ProcessInfo, error) {
		if n == 1 {
			return nil, supervisor.ErrUnreachable
		}
		return []supervisor.ProcessInfo{proc("a", supervisor.StateRunning)}, nil
	}}
	m := New(src, time.Hour)
	ch, unsub := m.Subscribe()
	defer unsub()
	start(t, m)
	if got := kinds(recv(t, ch, 5*time.Second).Events); len(got) != 1 || got[0] != Unreachable {
		t.Fatalf("first poll kinds = %v, want [Unreachable]", got)
	}
	m.Poke()
	if got := kinds(recv(t, ch, 5*time.Second).Events); len(got) != 1 || got[0] != Added {
		t.Errorf("first contact kinds = %v, want [Added]", got)
	}
}

// lingerer takes about a second to stop after SIGTERM. supervisor.restart
// runs in-process: while supervisord stops its programs every RPC fails with
// SHUTDOWN_STATE, then it reopens its socket. With only quick programs that
// gap is a few milliseconds and a poll can miss it; lingerer widens it so
// the test observes the outage every time.
var lingerer = supervisortest.Program{
	Name:    "lingerer",
	Command: `sh -c 'trap "sleep 1; exit 0" TERM; while :; do sleep 0.1; done'`,
}

// Review Focus 2: supervisord restarting under the monitor shows up with
// fresh state afterwards, whether or not a poll lands in the outage.
func TestMonitorSurvivesDaemonRestart(t *testing.T) {
	t.Parallel()
	t.Run("outage seen", func(t *testing.T) {
		t.Parallel()
		restartWithOutage(t)
	})
	t.Run("between polls", func(t *testing.T) {
		t.Parallel()
		restartBetweenPolls(t)
	})
}

// A slow-stopping program makes the outage visible: Unreachable, then
// Reconnected, then the sleeper running with a new pid.
func restartWithOutage(t *testing.T) {
	i := supervisortest.Start(t, supervisortest.Sleeper, lingerer)
	before := i.WaitFor(t, "sleeper", supervisor.StateRunning)
	i.WaitFor(t, "lingerer", supervisor.StateRunning)

	m := New(i.Client(), 20*time.Millisecond)
	ch, unsub := m.Subscribe()
	defer unsub()
	start(t, m)

	deadline := time.Now().Add(15 * time.Second)
	for {
		u := recv(t, ch, time.Until(deadline))
		if p, ok := find(u.Snapshot, "sleeper:sleeper"); ok && p.PID == before.PID {
			break
		}
	}

	if err := i.Client().Restart(context.Background()); err != nil {
		t.Fatal(err)
	}
	var (
		sawDown, sawReconnected bool
		downErr                 error
	)
	deadline = time.Now().Add(30 * time.Second)
	for {
		u := recv(t, ch, time.Until(deadline))
		if !u.Snapshot.Reachable {
			sawDown, downErr = true, u.Snapshot.Err
		}
		for _, e := range u.Events {
			switch e.Kind {
			case Unreachable:
				sawDown = true
			case Reconnected:
				if !sawDown {
					t.Fatal("Reconnected without an unreachable update first")
				}
				sawReconnected = true
			}
		}
		p, ok := find(u.Snapshot, "sleeper:sleeper")
		if sawReconnected && u.Snapshot.Reachable && ok && p.State == supervisor.StateRunning && p.PID != before.PID {
			if u.Snapshot.State.Name != "RUNNING" {
				t.Errorf("supervisord state after restart = %+v, want RUNNING", u.Snapshot.State)
			}
			t.Logf("outage error: %v", downErr)
			return
		}
	}
}

// A restart that completes between two polls shows no outage and no state
// change, only a new pid: the monitor reports it as Restarted. Polling is
// on Poke only, so the test controls exactly when the polls happen.
func restartBetweenPolls(t *testing.T) {
	i := supervisortest.Start(t, supervisortest.Sleeper)
	before := i.WaitFor(t, "sleeper", supervisor.StateRunning)

	m := New(i.Client(), time.Hour)
	ch, unsub := m.Subscribe()
	defer unsub()
	start(t, m)
	if p, ok := find(recv(t, ch, 15*time.Second).Snapshot, "sleeper:sleeper"); !ok || p.PID != before.PID {
		t.Fatalf("first poll: sleeper = %+v, want pid %d", p, before.PID)
	}

	ctx := context.Background()
	if err := i.Client().Restart(ctx); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		p, err := i.Client().GetProcessInfo(ctx, "sleeper")
		if err == nil && p.State == supervisor.StateRunning && p.PID != before.PID {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("sleeper not running again with a new pid: %+v, %v", p, err)
		}
		time.Sleep(50 * time.Millisecond)
	}

	m.Poke()
	u := recv(t, ch, 15*time.Second)
	if !u.Snapshot.Reachable || len(u.Events) != 1 {
		t.Fatalf("after restart: reachable=%v events=%+v, want one Restarted", u.Snapshot.Reachable, u.Events)
	}
	e := u.Events[0]
	if e.Kind != Restarted || e.Process != "sleeper:sleeper" || e.To != supervisor.StateRunning || e.Info.PID == before.PID {
		t.Errorf("event = %+v, want sleeper Restarted with a new pid", e)
	}
}
