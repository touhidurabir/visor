package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/touhidurabir/visor/internal/settings"
	"github.com/touhidurabir/visor/internal/supervisor"
	"github.com/touhidurabir/visor/internal/supervisor/supervisortest"
)

// crasher runs for 2s (past startsecs=1), then exits 1. supervisord shows
// it EXITED for only a few milliseconds before restarting it, so polls see
// a restart, not the exit.
func crasher(autorestart string) supervisortest.Program {
	p := supervisortest.Program{Name: "crasher", Command: "sh -c 'sleep 2; exit 1'"}
	if autorestart != "" {
		p.Extra = map[string]string{"autorestart": autorestart}
	}
	return p
}

// openIssues lists the open issues (the overview carries only their count).
func openIssues(svc *ServerService) []IssueView {
	out, err := NewIssueService(svc.rt).List(IssueQuery{Status: "open"})
	if err != nil {
		return nil
	}
	return out
}

func openOfKind(svc *ServerService, kind string) []IssueView {
	var out []IssueView
	for _, is := range openIssues(svc) {
		if is.Kind == kind {
			out = append(out, is)
		}
	}
	return out
}

// crashMessages are the two correct issues for crasher's exit 1: read
// from supervisord's log after the restart, or (when a poll lands in the
// few milliseconds it sits in EXITED) the observed exit.
var crashMessages = []string{"Exited with status 1 and was restarted by supervisord", "Exited with status 1"}

func TestAutoRestartedCrashOpensUnexpectedExit(t *testing.T) {
	inst := supervisortest.Start(t, crasher(""))
	rt, rec := startRuntime(t, confOf(inst), t.TempDir(), 500*time.Millisecond)
	svc := NewServerService(rt)
	assertOneCrashIssue(t, svc, rec)
}

func TestAutorestartTrueCrashOpensIssueHarness(t *testing.T) {
	inst := supervisortest.Start(t, crasher("true"))
	rt, rec := startRuntime(t, confOf(inst), t.TempDir(), 500*time.Millisecond)
	svc := NewServerService(rt)
	assertOneCrashIssue(t, svc, rec)
}

// assertOneCrashIssue waits for crasher's unexpected_exit, then checks a
// second crash cycle opens no other.
func assertOneCrashIssue(t *testing.T, svc *ServerService, rec *recorder) {
	t.Helper()
	waitOverview(t, svc, 10*time.Second, "an unexpected_exit issue", func(OverviewView) bool {
		return len(openOfKind(svc, "unexpected_exit")) > 0
	})
	got := openOfKind(svc, "unexpected_exit")
	if len(got) != 1 || got[0].Process != "crasher:crasher" || !slices.Contains(crashMessages, got[0].Message) {
		t.Fatalf("unexpected_exit issues = %+v, want one with a message in %q", got, crashMessages)
	}
	time.Sleep(3 * time.Second) // another crash cycle must not open a second one
	n := 0
	for _, is := range rec.issuesOpened() {
		if is.Kind == "unexpected_exit" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d unexpected_exit issues opened, want 1", n)
	}
}

// A planned exit 0 under autorestart=true (e.g. queue:work --max-time) is
// in supervisord's log as expected, so its restarts open nothing.
func TestAutorestartTruePlannedExitOpensNothingHarness(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Program{
		Name: "finisher", Command: "sh -c 'sleep 2; exit 0'", Extra: map[string]string{"autorestart": "true"},
	})
	rt, rec := startRuntime(t, confOf(inst), t.TempDir(), 500*time.Millisecond)
	svc := NewServerService(rt)
	waitOverview(t, svc, 5*time.Second, "finisher running", procState("finisher:finisher", "RUNNING"))
	is := NewIssueService(rt)
	deadline := time.Now().Add(12 * time.Second)
	for restarts := 0; restarts < 2; {
		if time.Now().After(deadline) {
			t.Fatalf("saw %d restarts in 12s, want 2", restarts)
		}
		time.Sleep(200 * time.Millisecond)
		hist, err := is.History("finisher:finisher", 0)
		if err != nil {
			t.Fatal(err)
		}
		restarts = 0
		for _, h := range hist {
			if h.Note == "Restarted by supervisord" || (h.From == "RUNNING" && h.To == "STARTING") {
				restarts++
			}
		}
	}
	for _, is := range rec.issuesOpened() {
		if is.Kind == "unexpected_exit" {
			t.Errorf("a planned exit 0 opened %+v", is)
		}
	}
}

func TestUserRestartsOpenNoIssues(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Sleeper)
	rt, rec := startRuntime(t, confOf(inst), t.TempDir(), 100*time.Millisecond)
	svc := NewServerService(rt)
	waitOverview(t, svc, 5*time.Second, "sleeper running", procState("sleeper:sleeper", "RUNNING"))
	for i := 0; i < 5; i++ {
		if err := svc.Restart("sleeper:sleeper"); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(500 * time.Millisecond) // a few more polls
	ov := svc.Overview()
	if got := rec.issuesOpened(); len(got) != 0 {
		t.Errorf("user restarts opened %+v", got)
	}
	if ov.Server.Health == "error" {
		t.Errorf("health = error after user restarts; issues %+v", openIssues(svc))
	}
	hist, _ := NewIssueService(rt).History("sleeper:sleeper", 0)
	byYou := false
	for _, h := range hist {
		if h.Note == "by you" || h.Note == "Restarted by you" {
			byYou = true
		}
	}
	if !byYou {
		t.Errorf("history does not note the user's restarts: %+v", hist)
	}
}

func TestUserStartThenStopInOnePollOpensNothing(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Program{
		// startsecs stays 1: a SIGTERM sent before the child has exec'd is
		// lost, and the stop then waits stopwaitsecs for SIGKILL.
		Name: "sleeper", Command: "sleep 3600", Extra: map[string]string{"autostart": "false"},
	})
	// A long interval: only the action's poke polls, after both calls.
	rt, rec := startRuntime(t, confOf(inst), t.TempDir(), time.Minute)
	svc := NewServerService(rt)
	waitOverview(t, svc, 5*time.Second, "sleeper stopped", procState("sleeper:sleeper", "STOPPED"))
	before := len(rec.overviews())
	err := rt.act(expectProcess("sleeper:sleeper"), func(ctx context.Context, c *supervisor.Client) error {
		if err := c.StartProcess(ctx, "sleeper:sleeper", true); err != nil {
			return err
		}
		return c.StopProcess(ctx, "sleeper:sleeper", true)
	})
	if err != nil {
		t.Fatal(err)
	}
	waitOverview(t, svc, 3*time.Second, "the poke's poll", func(OverviewView) bool { return len(rec.overviews()) > before })
	if got := rec.issuesOpened(); len(got) != 0 {
		t.Errorf("start then stop opened %+v", got)
	}
}

func TestUserShutdownOpensNoUnreachable(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Sleeper)
	rt, rec := startRuntime(t, confOf(inst), t.TempDir(), 100*time.Millisecond)
	svc := NewServerService(rt)
	waitOverview(t, svc, 5*time.Second, "reachable", func(ov OverviewView) bool { return ov.Server.Reachable })
	if err := svc.ShutdownDaemon(); err != nil {
		t.Fatal(err)
	}
	waitOverview(t, svc, 5*time.Second, "unreachable", func(ov OverviewView) bool { return !ov.Server.Reachable })
	time.Sleep(6 * time.Second) // past the action deadline
	if got := rec.issuesOpened(); len(got) != 0 {
		t.Errorf("user shutdown opened %+v", got)
	}
}

// Starting a program that exits 1 is a failure even though the user asked
// for the start: only restarts and crash loops are the user's doing.
func TestUserStartOfFailingProgramOpensUnexpectedExit(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Program{
		Name: "oneshot", Command: "sh -c 'exit 1'",
		Extra: map[string]string{"startsecs": "0", "autorestart": "false", "autostart": "false"},
	})
	rt, rec := startRuntime(t, confOf(inst), t.TempDir(), 100*time.Millisecond)
	svc := NewServerService(rt)
	waitOverview(t, svc, 5*time.Second, "oneshot stopped", procState("oneshot:oneshot", "STOPPED"))
	_ = svc.Start("oneshot:oneshot") // may report an abnormal termination; the state is what counts
	waitOverview(t, svc, 5*time.Second, "oneshot exited", procState("oneshot:oneshot", "EXITED"))
	time.Sleep(300 * time.Millisecond) // a few more polls
	var got []IssueView
	for _, is := range rec.issuesOpened() {
		if is.Kind == "unexpected_exit" {
			got = append(got, is)
		}
	}
	if len(got) != 1 || got[0].Process != "oneshot:oneshot" || got[0].Message != "Exited with status 1" {
		t.Fatalf("unexpected_exit issues = %+v (all: %+v)", got, rec.issuesOpened())
	}
}

func TestDaemonActionGrace(t *testing.T) {
	rt := NewRuntime(RuntimeOptions{Interval: 100 * time.Millisecond})
	for _, c := range []struct {
		name string
		e    expectation
		want time.Duration
	}{
		{"process", expectProcess("g:w"), 5 * time.Second},
		{"restart daemon", expectDaemonRestart(), 30 * time.Second},
		{"shutdown daemon", expectDaemonShutdown(), 5 * time.Second},
	} {
		if got := rt.actionGrace(c.e); got != c.want {
			t.Errorf("%s: grace %v, want %v", c.name, got, c.want)
		}
	}
	slow := NewRuntime(RuntimeOptions{Interval: 20 * time.Second})
	if got := slow.actionGrace(expectDaemonRestart()); got != 40*time.Second {
		t.Errorf("slow poll restart grace %v, want 40s", got)
	}
}

// R26: restarts made outside Visor (supervisorctl restart, reload, a deploy
// script). The crash-loop threshold is the lowest allowed, 2, so a single
// counted restart pair would open a crash loop.

// outsideRuntime starts a Runtime on inst whose crash loop opens after two
// restarts.
func outsideRuntime(t *testing.T, inst *supervisortest.Instance, interval time.Duration) (*Runtime, *recorder) {
	t.Helper()
	s := settings.Defaults()
	s.CrashLoop = settings.CrashLoopRules{Restarts: 2, Window: 10 * time.Minute}
	return newRuntimeWith(t, RuntimeOptions{ConfigPath: confOf(inst), Interval: interval, Settings: s})
}

// pidsOf returns the PID Visor shows for each process.
func pidsOf(ov OverviewView) map[string]int {
	out := map[string]int{}
	for _, c := range ov.Configs {
		for _, p := range c.Processes {
			out[p.FullName] = p.PID
		}
	}
	return out
}

// allRestarted waits until Visor shows every process in before RUNNING
// with a new PID.
func allRestarted(before map[string]int) func(OverviewView) bool {
	return func(ov OverviewView) bool {
		if !ov.Server.Reachable {
			return false
		}
		for name, pid := range before {
			p, _, ok := findProc(ov, name)
			if !ok || p.State != "RUNNING" || p.PID == pid || p.PID == 0 {
				return false
			}
		}
		return true
	}
}

// settle waits for n more polls to be handled.
func settle(t *testing.T, rec *recorder, n int) {
	t.Helper()
	want := len(rec.overviews()) + n
	deadline := time.Now().Add(10 * time.Second)
	for len(rec.overviews()) < want {
		if time.Now().After(deadline) {
			t.Fatalf("only %d of %d overviews after 10s", len(rec.overviews()), want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// assertNoIssues fails if any issue opened, or one is still open.
func assertNoIssues(t *testing.T, svc *ServerService, rec *recorder) {
	t.Helper()
	if got := rec.issuesOpened(); len(got) != 0 {
		t.Errorf("restarts made outside Visor opened %+v", got)
	}
	if ov := svc.Overview(); ov.Stats.OpenIssues != 0 || ov.Server.Health == "error" {
		t.Errorf("open issues %+v, health %s", openIssues(svc), ov.Server.Health)
	}
}

// assertRestartsNoted checks that every restart of a RUNNING process in
// proc's history is noted as a stop someone asked for.
func assertRestartsNoted(t *testing.T, rt *Runtime, proc string) {
	t.Helper()
	hist, err := NewIssueService(rt).History(proc, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hist {
		if h.From == "RUNNING" && (h.To == "RUNNING" || h.To == "STARTING" || h.To == "BACKOFF") && h.Note != "Restarted (stopped on request)" {
			t.Errorf("%s history row %+v: want the note \"Restarted (stopped on request)\"", proc, h)
		}
	}
}

// (a) supervisor.restart from another client: what supervisorctl reload does.
func TestOutsideDaemonRestartOpensNoIssues(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Sleeper, supervisortest.Program{Name: "web", Command: "sleep 3600"})
	rt, rec := outsideRuntime(t, inst, 300*time.Millisecond)
	svc := NewServerService(rt)
	for i := 0; i < 2; i++ {
		ov := waitOverview(t, svc, 10*time.Second, "both running", func(ov OverviewView) bool {
			return procState("sleeper:sleeper", "RUNNING")(ov) && procState("web:web", "RUNNING")(ov)
		})
		before := pidsOf(ov)
		if err := inst.Client().Restart(context.Background()); err != nil {
			t.Fatal(err)
		}
		waitOverview(t, svc, 15*time.Second, "both restarted", allRestarted(before))
	}
	settle(t, rec, 4)
	assertNoIssues(t, svc, rec)
	assertRestartsNoted(t, rt, "web:web")
}

// (b) stop, then start, from another client between two polls: what
// supervisorctl restart does.
func TestOutsideStopStartOpensNoIssues(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Sleeper)
	rt, rec := outsideRuntime(t, inst, 300*time.Millisecond)
	svc := NewServerService(rt)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		ov := waitOverview(t, svc, 10*time.Second, "sleeper running", procState("sleeper:sleeper", "RUNNING"))
		before := pidsOf(ov)
		if err := inst.Client().StopProcess(ctx, "sleeper", true); err != nil {
			t.Fatal(err)
		}
		if err := inst.Client().StartProcess(ctx, "sleeper", false); err != nil {
			t.Fatal(err)
		}
		waitOverview(t, svc, 10*time.Second, "sleeper restarted", allRestarted(before))
	}
	settle(t, rec, 4)
	assertNoIssues(t, svc, rec)
	assertRestartsNoted(t, rt, "sleeper:sleeper")
}

// Review Focus 2 at the issue level: supervisord reloaded from outside
// Visor (SIGHUP, as a deploy script or launchd might send) with a mix of
// programs, numprocs, autorestart=true and one that exits 0 on SIGTERM.
// Once things settle, nothing is open.
func TestOutsideReloadSettlesWithNoOpenIssues(t *testing.T) {
	inst := supervisortest.Start(t,
		supervisortest.Sleeper,
		supervisortest.Program{Name: "web", Command: "sleep 3600", Extra: map[string]string{
			"numprocs": "2", "process_name": "%(program_name)s_%(process_num)02d",
		}},
		supervisortest.Program{Name: "graceful", Command: `sh -c 'trap "exit 0" TERM; while :; do sleep 0.2; done'`},
		supervisortest.Program{Name: "always", Command: "sleep 3600", Extra: map[string]string{"autorestart": "true"}},
	)
	rt, rec := outsideRuntime(t, inst, 300*time.Millisecond)
	svc := NewServerService(rt)
	names := []string{"sleeper:sleeper", "web:web_00", "web:web_01", "graceful:graceful", "always:always"}
	ov := waitOverview(t, svc, 10*time.Second, "all running", func(ov OverviewView) bool {
		for _, n := range names {
			if !procState(n, "RUNNING")(ov) {
				return false
			}
		}
		return true
	})
	before := pidsOf(ov)
	raw, err := os.ReadFile(filepath.Join(inst.Dir(), "supervisord.pid"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(pid, syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	waitOverview(t, svc, 20*time.Second, "all restarted", allRestarted(before))
	settle(t, rec, 5)
	assertNoIssues(t, svc, rec)
	for _, n := range names {
		assertRestartsNoted(t, rt, n)
	}
}
