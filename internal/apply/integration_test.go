package apply

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/touhidurabir/visor/internal/supervisor"
	"github.com/touhidurabir/visor/internal/supervisor/supervisortest"
)

var worker = supervisortest.Program{Name: "worker", Command: "sleep 3600"}

// harness starts supervisord with progs and returns it, a state folder's
// Backups, and a context for the test.
func harness(t *testing.T, progs ...supervisortest.Program) (*supervisortest.Instance, Backups, context.Context) {
	t.Helper()
	inst := supervisortest.Start(t, progs...)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	return inst, Backups{Dir: filepath.Join(t.TempDir(), "backups")}, ctx
}

func confPath(inst *supervisortest.Instance, name string) string {
	return filepath.Join(inst.Dir(), "conf.d", name+".ini")
}

// programIni is a program config with its logs in the instance's folder.
func programIni(inst *supervisortest.Instance, name, command string) []byte {
	return fmt.Appendf(nil, "[program:%[1]s]\ncommand=%[2]s\nautostart=true\nstdout_logfile=%[3]s/logs/%[1]s.out.log\nstderr_logfile=%[3]s/logs/%[1]s.err.log\n",
		name, command, inst.Dir())
}

func pidOf(t *testing.T, ctx context.Context, c *supervisor.Client, name string) int {
	t.Helper()
	p, err := c.GetProcessInfo(ctx, name)
	if err != nil {
		t.Fatalf("GetProcessInfo(%s): %v", name, err)
	}
	return p.PID
}

func assertGone(t *testing.T, ctx context.Context, c *supervisor.Client, name string) {
	t.Helper()
	if _, err := c.GetProcessInfo(ctx, name); !errors.Is(err, supervisor.ErrBadName) {
		t.Errorf("%s: err = %v, want BAD_NAME (not in supervisord)", name, err)
	}
}

func assertResult(t *testing.T, got Result, added, changed, removed []string) {
	t.Helper()
	if !slices.Equal(got.Added, added) || !slices.Equal(got.Changed, changed) || !slices.Equal(got.Removed, removed) || len(got.Errors) != 0 {
		t.Errorf("Result = %+v, want added %v, changed %v, removed %v, no errors", got, added, changed, removed)
	}
}

func TestReloadAddsNewProgram(t *testing.T) {
	inst, b, ctx := harness(t, supervisortest.Sleeper)
	c := inst.Client()
	if _, err := Write(confPath(inst, "fresh"), programIni(inst, "fresh", "sleep 3600"), "", b); err != nil {
		t.Fatal(err)
	}
	res, err := Reload(ctx, c, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertResult(t, res, []string{"fresh"}, nil, nil)
	inst.WaitFor(t, "fresh", supervisor.StateRunning)
}

func TestReloadChangedRestartsOnlyThatGroup(t *testing.T) {
	inst, b, ctx := harness(t, supervisortest.Sleeper, worker)
	c := inst.Client()
	inst.WaitFor(t, "sleeper", supervisor.StateRunning)
	inst.WaitFor(t, "worker", supervisor.StateRunning)
	sleeperPID, workerPID := pidOf(t, ctx, c, "sleeper"), pidOf(t, ctx, c, "worker")

	p := confPath(inst, "worker")
	old, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Write(p, programIni(inst, "worker", "sleep 3601"), Hash(old), b); err != nil {
		t.Fatal(err)
	}
	res, err := Reload(ctx, c, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertResult(t, res, nil, []string{"worker"}, nil)
	w := inst.WaitFor(t, "worker", supervisor.StateRunning)
	if w.PID == workerPID {
		t.Errorf("worker PID unchanged (%d): the changed group was not restarted", w.PID)
	}
	if got := pidOf(t, ctx, c, "sleeper"); got != sleeperPID {
		t.Errorf("sleeper PID %d -> %d: an unrelated program was restarted", sleeperPID, got)
	}
}

// A changed group is removed only once its processes have stopped, even
// when they take a while to exit.
func TestReloadWaitsForSlowStop(t *testing.T) {
	slow := supervisortest.Program{
		Name:    "slow",
		Command: `sh -c 'trap "sleep 1; exit 0" TERM; while :; do sleep 0.1; done'`,
		Extra:   map[string]string{"stopwaitsecs": "10", "startsecs": "0"},
	}
	inst, b, ctx := harness(t, slow)
	c := inst.Client()
	inst.WaitFor(t, "slow", supervisor.StateRunning)
	before := pidOf(t, ctx, c, "slow")

	p := confPath(inst, "slow")
	old, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Write(p, append(old, "priority=500\n"...), Hash(old), b); err != nil {
		t.Fatal(err)
	}
	res, err := Reload(ctx, c, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertResult(t, res, nil, []string{"slow"}, nil)
	if after := inst.WaitFor(t, "slow", supervisor.StateRunning).PID; after == before {
		t.Errorf("slow PID unchanged (%d)", after)
	}
}

func TestReloadRemoved(t *testing.T) {
	inst, b, ctx := harness(t, supervisortest.Sleeper, worker)
	c := inst.Client()
	inst.WaitFor(t, "worker", supervisor.StateRunning)
	sleeperPID := pidOf(t, ctx, c, "sleeper")
	if _, err := Remove(confPath(inst, "worker"), b); err != nil {
		t.Fatal(err)
	}
	res, err := Reload(ctx, c, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertResult(t, res, nil, nil, []string{"worker"})
	assertGone(t, ctx, c, "worker")
	if got := pidOf(t, ctx, c, "sleeper"); got != sleeperPID {
		t.Errorf("sleeper PID %d -> %d", sleeperPID, got)
	}
}

func TestReloadOnlyLimits(t *testing.T) {
	inst, b, ctx := harness(t, supervisortest.Sleeper)
	c := inst.Client()
	for _, n := range []string{"alpha", "beta"} {
		if _, err := Write(confPath(inst, n), programIni(inst, n, "sleep 3600"), "", b); err != nil {
			t.Fatal(err)
		}
	}
	res, err := Reload(ctx, c, []string{"alpha"})
	if err != nil {
		t.Fatal(err)
	}
	assertResult(t, res, []string{"alpha"}, nil, nil)
	inst.WaitFor(t, "alpha", supervisor.StateRunning)
	assertGone(t, ctx, c, "beta")

	// An empty (non-nil) list applies nothing.
	res, err = Reload(ctx, c, []string{})
	if err != nil {
		t.Fatal(err)
	}
	assertResult(t, res, nil, nil, nil)
	assertGone(t, ctx, c, "beta")

	res, err = Reload(ctx, c, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertResult(t, res, []string{"beta"}, nil, nil)
	inst.WaitFor(t, "beta", supervisor.StateRunning)
}

func TestRemoveFromSupervisorStaysRemoved(t *testing.T) {
	inst, b, ctx := harness(t, supervisortest.Sleeper, worker)
	c := inst.Client()
	inst.WaitFor(t, "worker", supervisor.StateRunning)
	p := confPath(inst, "worker")
	content, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	bk, err := Remove(p, b)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Reload(ctx, c, []string{"worker"})
	if err != nil {
		t.Fatal(err)
	}
	assertResult(t, res, nil, nil, []string{"worker"})

	// A later update (reread + apply everything) does not bring it back:
	// the file is in Visor's backups, not the include folder.
	res, err = Reload(ctx, c, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertResult(t, res, nil, nil, nil)
	assertGone(t, ctx, c, "worker")
	if got, err := b.Read(bk); err != nil || string(got) != string(content) {
		t.Errorf("backup = %q, %v", got, err)
	}
	pidOf(t, ctx, c, "sleeper")
}
