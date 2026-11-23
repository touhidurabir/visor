package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/touhidurabir/visor/internal/supervisor"
	"github.com/touhidurabir/visor/internal/supervisor/supervisortest"
	"github.com/touhidurabir/visor/internal/usage"
)

// fakeUsage gives every pid 1 MB and 2 processes, and one more second of
// CPU per call; fail makes later calls fail.
type fakeUsage struct {
	mu    sync.Mutex
	calls int
	fail  bool
}

func (f *fakeUsage) Sample(_ context.Context, pids []int) (map[int]usage.Totals, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return nil, errors.New("connection lost")
	}
	f.calls++
	out := map[int]usage.Totals{}
	for _, p := range pids {
		out[p] = usage.Totals{CPU: time.Duration(f.calls) * time.Second, RSS: 1 << 20, Procs: 2}
	}
	return out, nil
}

func fastUsage(t *testing.T) {
	old := usageEvery
	usageEvery = 50 * time.Millisecond
	t.Cleanup(func() { usageEvery = old })
}

func startUsageRuntime(t *testing.T, s usage.Sampler) (*ServerService, *supervisortest.Instance) {
	t.Helper()
	fastUsage(t)
	inst := supervisortest.Start(t, supervisortest.Sleeper, supervisortest.Program{Name: "idle", Command: "sleep 3600", Extra: map[string]string{"autostart": "false"}})
	inst.WaitFor(t, "sleeper", supervisor.StateRunning)
	rt := NewRuntime(RuntimeOptions{ConfigPath: confOf(inst), StateDir: t.TempDir(), Interval: 50 * time.Millisecond, Usage: s})
	if err := rt.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rt.Stop)
	return NewServerService(rt), inst
}

func TestUsageInOverview(t *testing.T) {
	svc, _ := startUsageRuntime(t, &fakeUsage{})
	ov := waitOverview(t, svc, 5*time.Second, "sleeper's CPU", func(ov OverviewView) bool {
		p, _, ok := findProc(ov, "sleeper:sleeper")
		return ok && p.CPU > 0
	})
	p, _, _ := findProc(ov, "sleeper:sleeper")
	if p.MemBytes != 1<<20 || p.Procs != 2 || len(p.Spark) == 0 {
		t.Fatalf("sleeper %+v", p)
	}
	idle, _, ok := findProc(ov, "idle:idle")
	if !ok || idle.CPU != -1 || idle.MemBytes != -1 {
		t.Fatalf("a stopped process is unknown: %+v", idle)
	}
}

func TestUsageSamplerErrorKeepsFigures(t *testing.T) {
	f := &fakeUsage{}
	svc, _ := startUsageRuntime(t, f)
	waitOverview(t, svc, 5*time.Second, "a figure", func(ov OverviewView) bool {
		p, _, ok := findProc(ov, "sleeper:sleeper")
		return ok && p.MemBytes > 0
	})
	f.mu.Lock()
	f.fail = true
	f.mu.Unlock()
	time.Sleep(300 * time.Millisecond)
	p, _, _ := findProc(svc.Overview(), "sleeper:sleeper")
	if p.MemBytes != 1<<20 {
		t.Fatalf("a failed sample dropped the figures: %+v", p)
	}
}

func TestUsageService(t *testing.T) {
	svc, _ := startUsageRuntime(t, &fakeUsage{})
	ov := waitOverview(t, svc, 5*time.Second, "two samples", func(ov OverviewView) bool {
		p, _, ok := findProc(ov, "sleeper:sleeper")
		return ok && p.CPU > 0
	})
	_, c, _ := findProc(ov, "sleeper:sleeper")
	views, err := svc.Usage(c.Path)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 || views[0].Process != "sleeper:sleeper" || len(views[0].Points) < 2 || views[0].Summary.PeakRSS != 1<<20 || views[0].EveryMs != 50 {
		t.Fatalf("usage of %s: %+v", c.Path, views)
	}
	if _, err := svc.Usage("/nope.conf"); err == nil {
		t.Fatal("an unknown config has usage")
	}
}

func TestUsageLocalByDefault(t *testing.T) {
	if usage.Local() == nil {
		t.Skip("no local sampler on this OS")
	}
	svc, _ := startUsageRuntime(t, nil)
	waitOverview(t, svc, 5*time.Second, "sleeper's memory, measured", func(ov OverviewView) bool {
		p, _, ok := findProc(ov, "sleeper:sleeper")
		return ok && p.MemBytes > 0 && p.Procs >= 1
	})
}
