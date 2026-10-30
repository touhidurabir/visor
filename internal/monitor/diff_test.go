package monitor

import (
	"errors"
	"testing"
	"time"

	"github.com/touhidurabir/visor/internal/supervisor"
)

var t0 = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func proc(name string, state supervisor.ProcessState) supervisor.ProcessInfo {
	return supervisor.ProcessInfo{Name: name, Group: name, State: state, StateName: state.String()}
}

func up(at time.Duration, procs ...supervisor.ProcessInfo) Snapshot {
	return Snapshot{
		At:        t0.Add(at),
		Reachable: true,
		State:     supervisor.SupervisorState{Code: 1, Name: "RUNNING"},
		Processes: procs,
	}
}

func down(at time.Duration) Snapshot {
	return Snapshot{At: t0.Add(at), Err: supervisor.ErrUnreachable}
}

func kinds(evs []Event) []EventKind {
	out := make([]EventKind, len(evs))
	for i, e := range evs {
		out[i] = e.Kind
	}
	return out
}

func TestDiffStateChange(t *testing.T) {
	prev := up(0, proc("a", supervisor.StateStarting), proc("b", supervisor.StateRunning))
	next := up(time.Second, proc("a", supervisor.StateRunning), proc("b", supervisor.StateRunning))
	evs := Diff(prev, next)
	if len(evs) != 1 {
		t.Fatalf("events = %+v, want one StateChanged", evs)
	}
	e := evs[0]
	if e.Kind != StateChanged || e.Process != "a:a" || e.From != supervisor.StateStarting ||
		e.To != supervisor.StateRunning || e.Info.State != supervisor.StateRunning || !e.At.Equal(next.At) {
		t.Errorf("event = %+v", e)
	}
}

func TestDiffAddedRemoved(t *testing.T) {
	prev := up(0, proc("a", supervisor.StateRunning), proc("gone", supervisor.StateStopped))
	next := up(time.Second, proc("a", supervisor.StateRunning), proc("new", supervisor.StateStarting))
	evs := Diff(prev, next)
	if len(evs) != 2 {
		t.Fatalf("events = %+v, want Added and Removed", evs)
	}
	var added, removed *Event
	for i := range evs {
		switch evs[i].Kind {
		case Added:
			added = &evs[i]
		case Removed:
			removed = &evs[i]
		}
	}
	if added == nil || added.Process != "new:new" || added.To != supervisor.StateStarting || added.Info.Name != "new" {
		t.Errorf("added = %+v", added)
	}
	if removed == nil || removed.Process != "gone:gone" || removed.From != supervisor.StateStopped || removed.Info.Name != "gone" {
		t.Errorf("removed = %+v", removed)
	}
}

// The very first poll is diffed against the zero Snapshot: every process is
// Added, and nothing claims a reconnection.
func TestDiffFirstSnapshot(t *testing.T) {
	evs := Diff(Snapshot{}, up(0, proc("a", supervisor.StateRunning), proc("b", supervisor.StateFatal)))
	if got := kinds(evs); len(got) != 2 || got[0] != Added || got[1] != Added {
		t.Errorf("first reachable poll: kinds = %v, want [Added Added]", got)
	}
	if got := kinds(Diff(Snapshot{}, down(0))); len(got) != 1 || got[0] != Unreachable {
		t.Errorf("first unreachable poll: kinds = %v, want [Unreachable]", got)
	}
}

func TestDiffUnreachableReconnected(t *testing.T) {
	s1 := up(0, proc("a", supervisor.StateRunning))
	s2, s3 := down(time.Second), down(2*time.Second)
	s4 := up(3*time.Second, proc("a", supervisor.StateRunning))

	if got := kinds(Diff(s1, s2)); len(got) != 1 || got[0] != Unreachable {
		t.Errorf("going down: kinds = %v, want [Unreachable]", got)
	}
	if evs := Diff(s2, s3); len(evs) != 0 {
		t.Errorf("staying down: events = %+v, want none", evs)
	}
	evs := Diff(s3, s4)
	if got := kinds(evs); len(got) == 0 || got[0] != Reconnected {
		t.Fatalf("coming back: kinds = %v, want Reconnected first", got)
	}
	if !evs[0].At.Equal(s4.At) {
		t.Errorf("Reconnected at %v, want %v", evs[0].At, s4.At)
	}
}

// Coming back, processes are diffed against the down snapshot's Processes,
// so a caller that carries the last known list over sees what changed.
func TestDiffReconnectedReportsChanges(t *testing.T) {
	last := down(time.Second)
	last.Processes = []supervisor.ProcessInfo{proc("a", supervisor.StateRunning)}
	evs := Diff(last, up(2*time.Second, proc("a", supervisor.StateFatal)))
	if got := kinds(evs); len(got) != 2 || got[0] != Reconnected || got[1] != StateChanged {
		t.Fatalf("kinds = %v, want [Reconnected StateChanged]", got)
	}
	if evs[1].From != supervisor.StateRunning || evs[1].To != supervisor.StateFatal {
		t.Errorf("event = %+v", evs[1])
	}
}

func TestDiffIgnoresUnchanged(t *testing.T) {
	a := proc("a", supervisor.StateRunning)
	a.PID = 10
	b := a
	b.PID = 11 // only details changed, not the state
	b.Description = "pid 11, uptime 0:00:05"
	if evs := Diff(up(0, a), up(time.Second, b)); len(evs) != 0 {
		t.Errorf("events = %+v, want none", evs)
	}
	if evs := Diff(up(0), up(time.Second)); len(evs) != 0 {
		t.Errorf("empty: events = %+v, want none", evs)
	}
	errA, errB := down(0), down(time.Second)
	errB.Err = errors.New("a different error")
	if evs := Diff(errA, errB); len(evs) != 0 {
		t.Errorf("still down with another error: events = %+v, want none", evs)
	}
}
