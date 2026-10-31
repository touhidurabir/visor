package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/touhidurabir/visor/internal/issues"
	"github.com/touhidurabir/visor/internal/monitor"
	"github.com/touhidurabir/visor/internal/supervisor"
)

func TestToProcessView(t *testing.T) {
	p := supervisor.ProcessInfo{
		Name: "q_00", Group: "queue", Description: "pid 7", State: supervisor.StateRunning,
		StateName: "RUNNING", PID: 7, ExitStatus: 2, SpawnErr: "",
		Start: time.UnixMilli(1500), Now: time.UnixMilli(4000),
	}
	v := toProcessView(p)
	if v.FullName != "queue:q_00" || v.State != "RUNNING" {
		t.Errorf("got %+v", v)
	}
	if v.StartMs != 1500 || v.NowMs != 4000 || v.StopMs != 0 {
		t.Errorf("times: start=%d stop=%d now=%d", v.StartMs, v.StopMs, v.NowMs)
	}
	if v.PID != 7 || v.ExitStatus != 2 {
		t.Errorf("got %+v", v)
	}
}

func writeConf(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "supervisord.conf")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// Status lets the frontend colour a process without re-deriving
// supervisord's exitcodes= rules.
func TestBuildOverviewProcessStatus(t *testing.T) {
	now := time.UnixMilli(10_000_000)
	proc := func(name string, st supervisor.ProcessState, exit int) supervisor.ProcessInfo {
		return supervisor.ProcessInfo{Name: name, Group: name, State: st, ExitStatus: exit, Now: now}
	}
	ov := buildOverview(overviewInput{
		now: now,
		snap: monitor.Snapshot{At: now, Reachable: true, Processes: []supervisor.ProcessInfo{
			proc("run", supervisor.StateRunning, 0),
			proc("fatal", supervisor.StateFatal, 0),
			proc("backoff", supervisor.StateBackoff, 0),
			proc("clean", supervisor.StateExited, 0),
			proc("crash", supervisor.StateExited, 3),
			proc("off", supervisor.StateStopped, 0),
			proc("boot", supervisor.StateStarting, 0),
		}},
		watched: func(string) bool { return true },
	})
	want := map[string]string{
		"run": "running", "fatal": "failing", "backoff": "failing", "clean": "stopped",
		"crash": "failing", "off": "stopped", "boot": "other",
	}
	got := map[string]string{}
	for _, c := range ov.Configs {
		for _, p := range c.Processes {
			got[p.Name] = p.Status
		}
	}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s: Status = %q, want %q", name, got[name], w)
		}
	}
}

// One failed poll is a blip; the tray turns red from the second in a row,
// when the unreachable issue opens.
func TestHealthErrorAfterSecondUnreachablePoll(t *testing.T) {
	now := time.UnixMilli(10_000_000)
	snap := monitor.Snapshot{At: now, Err: supervisor.ErrUnreachable}
	for polls, want := range map[int]string{1: "warning", 2: "error", 5: "error"} {
		ov := buildOverview(overviewInput{now: now, snap: snap, failedPolls: polls})
		if ov.Server.Health != want {
			t.Errorf("%d failed polls: health %q, want %q", polls, ov.Server.Health, want)
		}
	}
}

func TestFailingSinceCountsFromLatestFailureIssue(t *testing.T) {
	now := time.UnixMilli(1_000_000_000)
	p := supervisor.ProcessInfo{Name: "w", Group: "g", State: supervisor.StateFatal, Stop: now.Add(-10 * time.Hour), Now: now}
	is := func(k issues.Kind, ago time.Duration) issues.Issue {
		return issues.Issue{Kind: k, Process: "g:w", OpenedAt: now.Add(-ago), Message: "Restarted 5 times in 10 minutes"}
	}
	open := []issues.Issue{
		is(issues.KindCrashLoop, 5*time.Minute),
		is(issues.KindFatal, 2*time.Hour),
		is(issues.KindUnexpectedExit, 72*time.Hour),
	}
	if since, _ := sinceAndDetail(p, false, open, now); since != "failing 2h" {
		t.Errorf("since = %q, want failing 2h (the latest fatal or unexpected exit)", since)
	}
	// A crash loop alone does not say when it started failing: use the stop.
	if since, _ := sinceAndDetail(p, false, open[:1], now); since != "failing 10h" {
		t.Errorf("crash loop only: since = %q, want failing 10h", since)
	}
}

// A process that crashed and was restarted is running again: an open
// unexpected exit warns; a crash loop is an error.
func TestOpenIssuesRaiseHealth(t *testing.T) {
	now := time.UnixMilli(10_000_000)
	snap := monitor.Snapshot{At: now, Reachable: true, Processes: []supervisor.ProcessInfo{
		{Name: "w", Group: "g", State: supervisor.StateRunning, Now: now},
	}}
	for kinds, want := range map[string]string{
		"":                           "ok",
		"unexpected_exit":            "warning",
		"crash_loop":                 "error",
		"unexpected_exit crash_loop": "error",
	} {
		var open []issues.Issue
		for _, k := range strings.Fields(kinds) {
			open = append(open, issues.Issue{Kind: issues.Kind(k), Process: "g:w", OpenedAt: now})
		}
		if got := buildOverview(overviewInput{now: now, snap: snap, open: open}).Server.Health; got != want {
			t.Errorf("open %q: health %q, want %q", kinds, got, want)
		}
	}
	// It never lowers an error.
	fatal := snap
	fatal.Processes = []supervisor.ProcessInfo{{Name: "w", Group: "g", State: supervisor.StateFatal, Now: now}}
	open := []issues.Issue{{Kind: issues.KindUnexpectedExit, Process: "g:w", OpenedAt: now}}
	if got := buildOverview(overviewInput{now: now, snap: fatal, open: open}).Server.Health; got != "error" {
		t.Errorf("fatal with an unexpected exit: health %q, want error", got)
	}
}
