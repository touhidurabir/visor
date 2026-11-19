package activity

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/touhidurabir/visor/internal/config"
	"github.com/touhidurabir/visor/internal/issues"
	"github.com/touhidurabir/visor/internal/monitor"
	"github.com/touhidurabir/visor/internal/supervisor"
)

var t0 = time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)

func upd(at time.Duration, evs ...monitor.Event) Input {
	for i := range evs {
		evs[i].At = t0.Add(at)
	}
	return Input{Update: monitor.Update{Snapshot: monitor.Snapshot{At: t0.Add(at), Reachable: true}, Events: evs}}
}

func change(p string, from, to supervisor.ProcessState) monitor.Event {
	return monitor.Event{Kind: monitor.StateChanged, Process: p, From: from, To: to}
}

func kinds(evs []Event) []Kind {
	var k []Kind
	for _, e := range evs {
		k = append(k, e.Kind)
	}
	return k
}

// started returns a Deriver that has seen its first (baseline) update.
func started() *Deriver {
	d := NewDeriver()
	d.Next(upd(-time.Second))
	return d
}

func TestFirstUpdateListsNotChanges(t *testing.T) {
	d := NewDeriver()
	if out := d.Next(upd(0, monitor.Event{Kind: monitor.Added, Process: "a:a", To: supervisor.StateRunning})); len(out) != 0 {
		t.Fatalf("%v", kinds(out))
	}
}

func TestRunningToStartingIsARestartUnlessACrash(t *testing.T) {
	d := started()
	in := upd(0, change("a:a", supervisor.StateRunning, supervisor.StateStarting), change("b:b", supervisor.StateRunning, supervisor.StateStarting))
	in.Crashed = func(p string) bool { return p == "b:b" }
	out := d.Next(in)
	if len(out) != 1 || out[0].Process != "a:a" || out[0].Kind != ProcessRestarted {
		t.Fatalf("%+v", out)
	}
}

// A slow stop (Laravel Horizon waits for its jobs) is seen as STOPPING; the
// next poll already finds it starting again: that is a restart too.
func TestStoppingToStartingIsARestart(t *testing.T) {
	d := started()
	d.Next(upd(0, change("h:h", supervisor.StateRunning, supervisor.StateStopping)))
	out := d.Next(upd(6*time.Second, change("h:h", supervisor.StateStopping, supervisor.StateStarting)))
	if len(out) != 1 || out[0].Kind != ProcessRestarted {
		t.Fatalf("%v", kinds(out))
	}
}

func TestStopThenStartIsARestart(t *testing.T) {
	d := started()
	if out := d.Next(upd(0, change("w:w", supervisor.StateRunning, supervisor.StateStopped))); len(out) != 0 {
		t.Fatalf("a stop is held: %v", kinds(out))
	}
	out := d.Next(upd(2*time.Second, change("w:w", supervisor.StateStopped, supervisor.StateStarting)))
	if len(out) != 1 || out[0].Kind != ProcessRestarted || out[0].Process != "w:w" {
		t.Fatalf("%v", out)
	}
}

func TestAStopAloneComesOutAfterTheWindow(t *testing.T) {
	d := started()
	d.Next(upd(0, change("w:w", supervisor.StateStopping, supervisor.StateStopped)))
	if out := d.Next(upd(5 * time.Second)); len(out) != 0 {
		t.Fatal("too early")
	}
	out := d.Next(upd(11 * time.Second))
	if len(out) != 1 || out[0].Kind != ProcessStopped || !out[0].At.Equal(t0) {
		t.Fatalf("%v", out)
	}
}

func TestStartedAddedRemovedAndRestartedBetweenPolls(t *testing.T) {
	d := started()
	out := d.Next(upd(0,
		change("a:a", supervisor.StateStopped, supervisor.StateRunning),
		monitor.Event{Kind: monitor.Added, Process: "b:b", To: supervisor.StateRunning},
		monitor.Event{Kind: monitor.Removed, Process: "c:c", From: supervisor.StateRunning},
		monitor.Event{Kind: monitor.Restarted, Process: "e:e", From: supervisor.StateRunning, To: supervisor.StateRunning},
		// A crash is an issue, not activity.
		change("f:f", supervisor.StateRunning, supervisor.StateExited),
		change("f:f", supervisor.StateExited, supervisor.StateStarting),
	))
	want := []Kind{ProcessStarted, ProgramAdded, ProgramRemoved, ProcessRestarted}
	if got := kinds(out); len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if out[i].Kind != want[i] {
			t.Fatalf("got %v want %v", kinds(out), want)
		}
	}
}

func TestByVisor(t *testing.T) {
	d := started()
	in := upd(0, monitor.Event{Kind: monitor.Restarted, Process: "a:a", From: supervisor.StateRunning, To: supervisor.StateRunning},
		monitor.Event{Kind: monitor.Restarted, Process: "b:b", From: supervisor.StateRunning, To: supervisor.StateRunning})
	in.ByVisor = func(p string, _ time.Time) bool { return p == "a:a" }
	out := d.Next(in)
	if !out[0].ByVisor || out[1].ByVisor {
		t.Fatalf("%+v", out)
	}
}

func TestDaemonRestartDownAndBack(t *testing.T) {
	d := NewDeriver()
	in := upd(0)
	in.PidMtime = t0
	d.Next(in)
	// Restarted between polls: one daemon event, not one per process.
	in = upd(2*time.Second, monitor.Event{Kind: monitor.Restarted, Process: "a:a", From: supervisor.StateRunning, To: supervisor.StateRunning})
	in.PidMtime = t0.Add(time.Second)
	if got := kinds(d.Next(in)); len(got) != 1 || got[0] != DaemonRestarted {
		t.Fatalf("%v", got)
	}
	failing := func(at time.Duration, err error) Input {
		in := upd(at)
		in.Update.Snapshot.Reachable, in.Update.Snapshot.Err = false, err
		return in
	}
	// One failed poll is not "down"; two are.
	if got := d.Next(failing(4*time.Second, errors.New("timeout"))); len(got) != 0 {
		t.Fatalf("one poll: %v", kinds(got))
	}
	if got := kinds(d.Next(failing(6*time.Second, errors.New("timeout")))); len(got) != 1 || got[0] != DaemonDown {
		t.Fatalf("%v", got)
	}
	back := upd(8*time.Second, monitor.Event{Kind: monitor.Reconnected})
	back.PidMtime = t0.Add(time.Second)
	if got := kinds(d.Next(back)); len(got) != 1 || got[0] != DaemonBack {
		t.Fatalf("%v", got)
	}
	// A lost SSH connection and supervisord restarting are not "down".
	for i, err := range []error{fmt.Errorf("poll: %w", issues.ErrHostDown), supervisor.ErrShutdownState} {
		for k := range 3 {
			if got := d.Next(failing(time.Duration(10+i*10+k)*time.Second, err)); len(got) != 0 {
				t.Fatalf("%v: %v", err, kinds(got))
			}
		}
		if got := d.Next(upd(time.Duration(20+i*10) * time.Second)); len(got) != 0 {
			t.Fatalf("back after %v: %v", err, kinds(got))
		}
	}
}

func TestFileEventsAskAboutTheFile(t *testing.T) {
	d := started()
	in := upd(0)
	in.Checked = true
	d.Next(in)
	in = upd(time.Second)
	in.Checked, in.Changes = true, config.ChangeSet{Changed: []string{"/c/web.conf"}}
	var asked string
	in.ByVisor = func(p string, _ time.Time) bool { asked = p; return true }
	out := d.Next(in)
	if asked != "file:/c/web.conf" || len(out) != 1 || !out[0].ByVisor {
		t.Fatalf("%q %+v", asked, out)
	}
}

func TestFilesChangedOnDisk(t *testing.T) {
	d := NewDeriver()
	in := upd(0)
	in.Checked, in.Changes = true, config.ChangeSet{Changed: []string{"/c/old.conf"}}
	if out := d.Next(in); len(out) != 0 {
		t.Fatal("what was already changed at the start is not news")
	}
	in = upd(10 * time.Second)
	in.Checked, in.Changes = true, config.ChangeSet{Changed: []string{"/c/old.conf", "/c/web.conf"}, Added: []string{"/c/new.conf"}}
	out := d.Next(in)
	if len(out) != 2 {
		t.Fatalf("%v", out)
	}
	// Not checked this poll: no file events.
	if out := d.Next(upd(12 * time.Second)); len(out) != 0 {
		t.Fatal("unchecked")
	}
}

func TestStore(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, 0, t0)
	if err != nil {
		t.Fatal(err)
	}
	old := Event{ID: "1", At: t0.Add(-100 * 24 * time.Hour), Kind: ProcessStarted, Process: "a:a"}
	newer := Event{ID: "2", At: t0, Kind: ProcessRestarted, Process: "web:web", ByVisor: true}
	newest := Event{ID: "3", At: t0.Add(time.Minute), Kind: FileChanged, Config: "/c/web.conf"}
	if err := s.Add(old, newer, newest); err != nil {
		t.Fatal(err)
	}
	if got := s.List(Query{}); len(got) != 3 || got[0].ID != "3" {
		t.Fatalf("%+v", got)
	}
	if got := s.List(Query{Kind: "processes"}); len(got) != 2 {
		t.Fatalf("group: %+v", got)
	}
	if got := s.List(Query{Outside: true, Process: "web"}); len(got) != 1 || got[0].ID != "3" {
		t.Fatalf("outside web: %+v", got)
	}
	if e, ok := s.Latest(); !ok || e.ID != "3" {
		t.Fatalf("latest %+v", e)
	}
	// Reopened with a 90-day retention: the old one goes.
	s2, err := Open(dir, 90*24*time.Hour, t0)
	if err != nil {
		t.Fatal(err)
	}
	if got := s2.List(Query{}); len(got) != 2 {
		t.Fatalf("after retention: %+v", got)
	}
}
