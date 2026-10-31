package supervisor_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/touhidurabir/visor/internal/supervisor"
	"github.com/touhidurabir/visor/internal/supervisor/supervisortest"
)

// supervisord's SUCCESS code in group/all results.
const statusSuccess = 80

func TestIntegrationListsPrograms(t *testing.T) {
	t.Parallel()
	i := supervisortest.Start(t, supervisortest.Sleeper, supervisortest.Failer, supervisortest.Spammer)
	ctx := context.Background()
	c := i.Client()

	v, err := c.GetSupervisorVersion(ctx)
	if err != nil || !strings.HasPrefix(v, "4.") {
		t.Errorf("version = %q, %v; want 4.x", v, err)
	}
	procs, err := c.GetAllProcessInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range procs {
		names = append(names, p.FullName())
	}
	slices.Sort(names)
	if want := []string{"failer:failer", "sleeper:sleeper", "spammer:spammer"}; !slices.Equal(names, want) {
		t.Errorf("processes = %v, want %v", names, want)
	}
}

func TestIntegrationProcessLifecycle(t *testing.T) {
	t.Parallel()
	i := supervisortest.Start(t, supervisortest.Sleeper)
	ctx := context.Background()
	c := i.Client()

	first := i.WaitFor(t, "sleeper", supervisor.StateRunning)
	if first.PID <= 0 || first.Start.IsZero() {
		t.Errorf("running process should have a pid and start time: %+v", first)
	}
	if err := c.StartProcess(ctx, "sleeper", false); !errors.Is(err, supervisor.ErrAlreadyStarted) {
		t.Errorf("start while running: want ErrAlreadyStarted, got %v", err)
	}
	if err := c.StopProcess(ctx, "sleeper", true); err != nil {
		t.Fatal(err)
	}
	if p, _ := c.GetProcessInfo(ctx, "sleeper"); p.State != supervisor.StateStopped || p.PID != 0 {
		t.Errorf("after stop: %v pid %d", p.State, p.PID)
	}
	if err := c.StopProcess(ctx, "sleeper", true); !errors.Is(err, supervisor.ErrNotRunning) {
		t.Errorf("stop while stopped: want ErrNotRunning, got %v", err)
	}
	if err := c.StartProcess(ctx, "sleeper", true); err != nil {
		t.Fatal(err)
	}
	before := i.WaitFor(t, "sleeper", supervisor.StateRunning)
	if err := c.RestartProcess(ctx, "sleeper"); err != nil {
		t.Fatal(err)
	}
	after := i.WaitFor(t, "sleeper", supervisor.StateRunning)
	if after.PID == before.PID {
		t.Errorf("restart kept pid %d", after.PID)
	}

	// Restart of a stopped process starts it, like supervisorctl.
	if err := c.StopProcess(ctx, "sleeper", true); err != nil {
		t.Fatal(err)
	}
	if err := c.RestartProcess(ctx, "sleeper"); err != nil {
		t.Fatal(err)
	}
	i.WaitFor(t, "sleeper", supervisor.StateRunning)
}

func TestIntegrationFailerExits(t *testing.T) {
	t.Parallel()
	i := supervisortest.Start(t, supervisortest.Failer)
	p := i.WaitFor(t, "failer", supervisor.StateExited, supervisor.StateFatal)
	if p.ExitStatus != 3 {
		t.Errorf("exit status = %d, want 3", p.ExitStatus)
	}
	if p.Stop.IsZero() {
		t.Error("exited process should have a stop time")
	}
}

func TestIntegrationBadName(t *testing.T) {
	t.Parallel()
	i := supervisortest.Start(t, supervisortest.Sleeper)
	ctx := context.Background()
	c := i.Client()
	if _, err := c.GetProcessInfo(ctx, "nope"); !errors.Is(err, supervisor.ErrBadName) {
		t.Errorf("GetProcessInfo: want ErrBadName, got %v", err)
	}
	if err := c.StartProcess(ctx, "nope", false); !errors.Is(err, supervisor.ErrBadName) {
		t.Errorf("StartProcess: want ErrBadName, got %v", err)
	}
	if _, err := c.StartProcessGroup(ctx, "nope", false); !errors.Is(err, supervisor.ErrBadName) {
		t.Errorf("StartProcessGroup: want ErrBadName, got %v", err)
	}
}

func TestIntegrationGroupsAndSignals(t *testing.T) {
	t.Parallel()
	i := supervisortest.Start(t, supervisortest.Sleeper)
	ctx := context.Background()
	c := i.Client()
	i.WaitFor(t, "sleeper", supervisor.StateRunning)

	if err := c.SignalProcess(ctx, "sleeper", "CONT"); err != nil {
		t.Errorf("SignalProcess CONT: %v", err)
	}
	if err := c.SignalProcess(ctx, "sleeper", "NOPE"); !errors.Is(err, supervisor.ErrBadSignal) {
		t.Errorf("bad signal: want ErrBadSignal, got %v", err)
	}

	checkOne := func(op string, rs []supervisor.ProcessResult, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", op, err)
		}
		if len(rs) != 1 || rs[0].Name != "sleeper" || rs[0].Group != "sleeper" || rs[0].Status != statusSuccess {
			t.Errorf("%s results = %+v", op, rs)
		}
	}
	rs, err := c.StopProcessGroup(ctx, "sleeper", true)
	checkOne("StopProcessGroup", rs, err)
	rs, err = c.StartProcessGroup(ctx, "sleeper", true)
	checkOne("StartProcessGroup", rs, err)
	rs, err = c.StopAllProcesses(ctx, true)
	checkOne("StopAllProcesses", rs, err)
	rs, err = c.StartAllProcesses(ctx, true)
	checkOne("StartAllProcesses", rs, err)
	i.WaitFor(t, "sleeper", supervisor.StateRunning)
}

func TestIntegrationLogs(t *testing.T) {
	t.Parallel()
	i := supervisortest.Start(t, supervisortest.Spammer)
	ctx := context.Background()
	c := i.Client()
	i.WaitFor(t, "spammer", supervisor.StateRunning)

	// Wait for a few hundred bytes of output.
	var first supervisor.TailResult
	for deadline := time.Now().Add(10 * time.Second); first.Offset < 300; time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("spammer log never grew: %+v", first)
		}
		var err error
		if first, err = c.TailProcessStdoutLog(ctx, "spammer", 0, 100); err != nil {
			t.Fatal(err)
		}
	}
	if len(first.Bytes) != 100 || !first.Overflow || !strings.Contains(first.Bytes, "line ") {
		t.Errorf("tail of last 100 bytes = %q overflow %v", first.Bytes, first.Overflow)
	}

	time.Sleep(200 * time.Millisecond)
	// supervisord does not read forward from offset: it returns the last
	// `length` bytes ending at the log's size, which here is the whole log,
	// overlapping what the first tail already returned.
	next, err := c.TailProcessStdoutLog(ctx, "spammer", first.Offset, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	start := next.Offset - len(next.Bytes)
	if next.Offset <= first.Offset || next.Overflow || start != 0 {
		t.Fatalf("tail from %d = bytes [%d, %d), overflow %v", first.Offset, start, next.Offset, next.Overflow)
	}
	if overlap := next.Bytes[first.Offset-len(first.Bytes) : first.Offset]; overlap != first.Bytes {
		t.Errorf("chunks disagree on bytes before %d: %q vs %q", first.Offset, overlap, first.Bytes)
	}
	// An offset past the end (as after truncation) returns nothing and the
	// real, smaller size, which is how a reader detects truncation.
	if past, err := c.TailProcessStdoutLog(ctx, "spammer", 1<<30, 100); err != nil || past.Bytes != "" || past.Offset >= 1<<30 {
		t.Errorf("tail past end = %+v, %v", past, err)
	}

	if errTail, err := c.TailProcessStderrLog(ctx, "spammer", 0, 100); err != nil || errTail.Bytes != "" {
		t.Errorf("stderr tail = %+v, %v; want empty", errTail, err)
	}
	if main, err := c.ReadLog(ctx, 0, 0); err != nil || !strings.Contains(main, "supervisord started") {
		t.Errorf("ReadLog = %q, %v", main, err)
	}

	if err := c.ClearProcessLogs(ctx, "spammer"); err != nil {
		t.Fatal(err)
	}
	cleared, err := c.TailProcessStdoutLog(ctx, "spammer", 0, 100)
	if err != nil || cleared.Offset >= next.Offset {
		t.Errorf("after clear: offset %d (was %d), %v", cleared.Offset, next.Offset, err)
	}
}

// The `supervisorctl update` flow: write an .ini, reload, add/remove groups.
func TestIntegrationReloadConfig(t *testing.T) {
	t.Parallel()
	i := supervisortest.Start(t, supervisortest.Sleeper)
	ctx := context.Background()
	c := i.Client()
	i.WaitFor(t, "sleeper", supervisor.StateRunning)

	i.WriteProgram(t, supervisortest.Program{Name: "late", Command: "sleep 3600"})
	diff, err := c.ReloadConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(diff.Added, []string{"late"}) || len(diff.Changed) != 0 || len(diff.Removed) != 0 {
		t.Errorf("diff = %+v, want late added", diff)
	}
	if _, err := c.GetProcessInfo(ctx, "late"); !errors.Is(err, supervisor.ErrBadName) {
		t.Errorf("reload alone must not activate the group: %v", err)
	}
	if err := c.AddProcessGroup(ctx, "late"); err != nil {
		t.Fatal(err)
	}
	i.WaitFor(t, "late", supervisor.StateRunning)
	if err := c.AddProcessGroup(ctx, "late"); !errors.Is(err, supervisor.ErrAlreadyAdded) {
		t.Errorf("add twice: want ErrAlreadyAdded, got %v", err)
	}
	if err := c.RemoveProcessGroup(ctx, "late"); !errors.Is(err, supervisor.ErrStillRunning) {
		t.Errorf("remove running: want ErrStillRunning, got %v", err)
	}
	if err := c.StopProcess(ctx, "late", true); err != nil {
		t.Fatal(err)
	}
	if err := c.RemoveProcessGroup(ctx, "late"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetProcessInfo(ctx, "late"); !errors.Is(err, supervisor.ErrBadName) {
		t.Errorf("removed group still present: %v", err)
	}

	i.WriteProgram(t, supervisortest.Program{Name: "sleeper", Command: "sleep 3600", Extra: map[string]string{"priority": "5"}})
	if diff, err = c.ReloadConfig(ctx); err != nil || !slices.Equal(diff.Changed, []string{"sleeper"}) {
		t.Errorf("after editing sleeper: diff = %+v, %v", diff, err)
	}
	if err := os.Remove(filepath.Join(i.Dir(), "conf.d", "sleeper.ini")); err != nil {
		t.Fatal(err)
	}
	if diff, err = c.ReloadConfig(ctx); err != nil || !slices.Equal(diff.Removed, []string{"sleeper"}) {
		t.Errorf("after deleting sleeper: diff = %+v, %v", diff, err)
	}
}

// supervisord drops its socket while restarting; calls fail in the gap and
// recover once it is back.
func TestIntegrationDaemonRestart(t *testing.T) {
	t.Parallel()
	i := supervisortest.Start(t, supervisortest.Sleeper)
	ctx := context.Background()
	c := i.Client()
	before := i.WaitFor(t, "sleeper", supervisor.StateRunning)

	if err := c.Restart(ctx); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		st, err := c.GetState(ctx)
		if err == nil && st.Name == "RUNNING" {
			if p, err := c.GetProcessInfo(ctx, "sleeper"); err == nil && p.State == supervisor.StateRunning && p.PID != before.PID {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("supervisord did not come back: %+v, %v", st, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestIntegrationListMethods(t *testing.T) {
	t.Parallel()
	i := supervisortest.Start(t)
	methods, err := i.Client().ListMethods(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Every RPC the client calls.
	for _, m := range []string{
		"supervisor.getState", "supervisor.getSupervisorVersion",
		"supervisor.getAllProcessInfo", "supervisor.getProcessInfo", "system.listMethods",
		"supervisor.startProcess", "supervisor.stopProcess",
		"supervisor.startProcessGroup", "supervisor.stopProcessGroup",
		"supervisor.startAllProcesses", "supervisor.stopAllProcesses", "supervisor.signalProcess",
		"supervisor.tailProcessStdoutLog", "supervisor.tailProcessStderrLog",
		"supervisor.readLog", "supervisor.clearProcessLogs",
		"supervisor.reloadConfig", "supervisor.addProcessGroup", "supervisor.removeProcessGroup",
		"supervisor.restart", "supervisor.shutdown",
	} {
		if !slices.Contains(methods, m) {
			t.Errorf("supervisord does not offer %s", m)
		}
	}
}

func TestRecentExitsHarness(t *testing.T) {
	t.Parallel()
	// autorestart=true restarts both whatever the code. crash_er's exit is
	// "not expected" whether or not it outlives startsecs=1; finisher's exit
	// 0 is "expected" only once it is RUNNING, so it runs past startsecs.
	i := supervisortest.Start(t,
		supervisortest.Program{Name: "crash_er", Command: "sh -c 'sleep 1; exit 3'", Extra: map[string]string{"autorestart": "true"}},
		supervisortest.Program{Name: "finisher", Command: "sh -c 'sleep 2; exit 0'", Extra: map[string]string{"autorestart": "true"}},
	)
	ctx := context.Background()
	var crash, finish *supervisor.ExitLine
	deadline := time.Now().Add(8 * time.Second)
	for crash == nil || finish == nil {
		lines, err := i.Client().RecentExits(ctx, 64<<10)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range lines {
			switch l.Name {
			case "crash_er":
				crash = &l
			case "finisher":
				finish = &l
			}
		}
		if time.Now().After(deadline) {
			log, _ := i.Client().ReadLog(ctx, 0, 0)
			t.Fatalf("no exit lines for both programs after 8s; main log:\n%s", log)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if crash.Expected || crash.Status != 3 || crash.Desc != "exit status 3" {
		t.Errorf("crash_er exit = %+v, want status 3, not expected", *crash)
	}
	if !finish.Expected || finish.Status != 0 {
		t.Errorf("finisher exit = %+v, want status 0, expected", *finish)
	}
	for _, l := range []*supervisor.ExitLine{crash, finish} {
		if d := time.Since(l.At); d < 0 || d > 10*time.Second {
			t.Errorf("%s exited at %v, %v ago: wrong zone or format?", l.Name, l.At, d)
		}
	}
}
