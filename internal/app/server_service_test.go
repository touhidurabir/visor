package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/touhidurabir/visor/internal/supervisor"
	"github.com/touhidurabir/visor/internal/supervisor/supervisortest"
)

// recorder captures emitted events in place of Wails.
type recorder struct {
	mu     sync.Mutex
	events []recorded
}

type recorded struct {
	name string
	data any
}

func (r *recorder) emit(name string, data any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, recorded{name, data})
}

func (r *recorder) overviews() []OverviewView {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []OverviewView
	for _, e := range r.events {
		if e.name == EventOverview {
			out = append(out, e.data.(OverviewView))
		}
	}
	return out
}

func (r *recorder) issuesOpened() []IssueView {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []IssueView
	for _, e := range r.events {
		if e.name == EventIssueOpened {
			out = append(out, e.data.(IssueView))
		}
	}
	return out
}

func confOf(inst *supervisortest.Instance) string {
	return filepath.Join(inst.Dir(), "supervisord.conf")
}

// startRuntime starts a runtime on conf with its own state dir and stops it
// when the test ends.
func startRuntime(t *testing.T, conf, stateDir string, interval time.Duration) (*Runtime, *recorder) {
	t.Helper()
	rec := &recorder{}
	rt := NewRuntime(RuntimeOptions{ConfigPath: conf, StateDir: stateDir, Interval: interval, Emit: rec.emit})
	if err := rt.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rt.Stop)
	return rt, rec
}

func waitOverview(t *testing.T, svc *ServerService, timeout time.Duration, what string, ok func(OverviewView) bool) OverviewView {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		ov := svc.Overview()
		if ok(ov) {
			return ov
		}
		if time.Now().After(deadline) {
			b, _ := json.MarshalIndent(ov, "", "  ")
			t.Fatalf("timed out after %v waiting for %s; last overview:\n%s", timeout, what, b)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func findProc(ov OverviewView, fullName string) (ProcessView, ConfigView, bool) {
	for _, c := range ov.Configs {
		for _, p := range c.Processes {
			if p.FullName == fullName {
				return p, c, true
			}
		}
	}
	return ProcessView{}, ConfigView{}, false
}

func procState(fullName, state string) func(OverviewView) bool {
	return func(ov OverviewView) bool {
		p, _, ok := findProc(ov, fullName)
		return ok && p.State == state
	}
}

func TestOverviewGroupsByConfigFile(t *testing.T) {
	multi := supervisortest.Program{
		Name: "multi", Command: "sleep 3600",
		Extra: map[string]string{"numprocs": "2", "process_name": "%(program_name)s_%(process_num)d"},
	}
	orphan := supervisortest.Program{Name: "orphan", Command: "sleep 3600"}
	inst := supervisortest.Start(t, supervisortest.Sleeper, multi, orphan)
	// supervisord still runs orphan, but its file is gone.
	if err := os.Remove(filepath.Join(inst.Dir(), "conf.d", "orphan.ini")); err != nil {
		t.Fatal(err)
	}
	rt, _ := startRuntime(t, confOf(inst), t.TempDir(), 100*time.Millisecond)
	svc := NewServerService(rt)

	ov := waitOverview(t, svc, 15*time.Second, "all running", func(ov OverviewView) bool {
		return ov.Stats.Running == 4
	})
	if ov.Server.Name != "Local" || !ov.Server.Reachable || ov.Server.Version == "" || ov.Server.Health != "ok" {
		t.Errorf("server = %+v", ov.Server)
	}
	if ov.Server.ConfigPath != confOf(inst) {
		t.Errorf("config path = %q", ov.Server.ConfigPath)
	}
	if ov.Server.UptimeMs <= 0 {
		t.Errorf("uptime = %d, want > 0 from the pidfile", ov.Server.UptimeMs)
	}
	var names []string
	for _, c := range ov.Configs {
		names = append(names, c.FileName)
	}
	if want := []string{"multi.ini", "sleeper.ini", NoConfigFile}; !slices.Equal(names, want) {
		t.Fatalf("configs = %v, want %v", names, want)
	}
	m := ov.Configs[0]
	if m.Path != filepath.Join(inst.Dir(), "conf.d", "multi.ini") || !m.Watched || len(m.Processes) != 2 {
		t.Fatalf("multi config = %+v", m)
	}
	for _, p := range m.Processes {
		if p.Program != "multi" || p.Group != "multi" || !strings.HasPrefix(p.Since, "up ") {
			t.Errorf("multi process = %+v", p)
		}
	}
	if m.Summary != "1 program · 2 of 2 running" {
		t.Errorf("summary = %q", m.Summary)
	}
	none := ov.Configs[2]
	if none.Path != "" || len(none.Processes) != 1 || none.Processes[0].FullName != "orphan:orphan" {
		t.Fatalf("no-config group = %+v", none)
	}
	if ov.Stats.Configs != 2 || ov.Stats.Processes != 4 {
		t.Errorf("stats = %+v", ov.Stats)
	}
}

func TestOverviewStats(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Sleeper, supervisortest.Failer)
	rt, rec := startRuntime(t, confOf(inst), t.TempDir(), 100*time.Millisecond)
	svc := NewServerService(rt)

	ov := waitOverview(t, svc, 15*time.Second, "failer failing", func(ov OverviewView) bool {
		return ov.Stats.Failing == 1 && ov.Stats.Running == 1 && ov.Stats.OpenIssues == 1
	})
	want := StatsView{Configs: 2, Processes: 2, Running: 1, Failing: 1, Stopped: 0, OpenIssues: 1}
	if ov.Stats != want {
		t.Errorf("stats = %+v, want %+v", ov.Stats, want)
	}
	if ov.Server.Health != "error" {
		t.Errorf("health = %q, want error for an unexpected exit", ov.Server.Health)
	}
	p, _, _ := findProc(ov, "failer:failer")
	if !strings.HasPrefix(p.Since, "failing ") || !strings.Contains(p.Detail, "exited 3") {
		t.Errorf("failer = since %q detail %q", p.Since, p.Detail)
	}
	if len(ov.Issues) != 1 || ov.Issues[0].Process != "failer:failer" || ov.Issues[0].Kind != "unexpected_exit" {
		t.Fatalf("issues = %+v", ov.Issues)
	}
	if got := rec.issuesOpened(); len(got) != 1 || got[0].ID != ov.Issues[0].ID {
		t.Errorf("issue-opened events = %+v", got)
	}
	if len(rec.overviews()) == 0 {
		t.Error("no overview events emitted")
	}
}

func TestSinceLabels(t *testing.T) {
	cases := []struct {
		prefix string
		d      time.Duration
		want   string
	}{
		{"up", 0, "up 0s"},
		{"up", 59 * time.Second, "up 59s"},
		{"up", 41*time.Minute + 30*time.Second, "up 41m"},
		{"failing", 3*time.Hour + 2*time.Minute + 9*time.Second, "failing 3h 2m"},
		{"up", 6*24*time.Hour + 3*time.Hour + 5*time.Minute, "up 6d 3h"},
		{"stopped", 5 * 24 * time.Hour, "stopped 5d"},
		{"up", 2 * time.Hour, "up 2h"},
		{"up", -time.Second, "up 0s"}, // clock skew
	}
	for _, c := range cases {
		if got := sinceLabel(c.prefix, c.d); got != c.want {
			t.Errorf("sinceLabel(%q, %v) = %q, want %q", c.prefix, c.d, got, c.want)
		}
	}
}

func TestActionsPokeMonitor(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Sleeper)
	inst.WaitFor(t, "sleeper", supervisor.StateRunning)
	// A long interval: only a poke can refresh the overview in time.
	rt, rec := startRuntime(t, confOf(inst), t.TempDir(), time.Minute)
	svc := NewServerService(rt)
	waitOverview(t, svc, 5*time.Second, "sleeper running", procState("sleeper:sleeper", "RUNNING"))

	if err := svc.Stop("sleeper:sleeper"); err != nil {
		t.Fatal(err)
	}
	waitOverview(t, svc, 3*time.Second, "sleeper stopped", procState("sleeper:sleeper", "STOPPED"))
	ovs := rec.overviews()
	if !procState("sleeper:sleeper", "STOPPED")(ovs[len(ovs)-1]) {
		t.Error("the last visor:overview event does not show the stop")
	}

	if err := svc.Start("sleeper:sleeper"); err != nil {
		t.Fatal(err)
	}
	waitOverview(t, svc, 3*time.Second, "sleeper running again", procState("sleeper:sleeper", "RUNNING"))
}

func TestStartFileStartsEveryGroup(t *testing.T) {
	inst := supervisortest.Start(t)
	path := filepath.Join(inst.Dir(), "conf.d", "pair.ini")
	body := "[program:one]\ncommand=sleep 3600\nautostart=false\nstartsecs=0\n\n" +
		"[program:two]\ncommand=sleep 3600\nautostart=false\nstartsecs=0\nnumprocs=2\nprocess_name=%(program_name)s_%(process_num)d\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c := inst.Client()
	ctx := context.Background()
	if _, err := c.ReloadConfig(ctx); err != nil {
		t.Fatal(err)
	}
	for _, g := range []string{"one", "two"} {
		if err := c.AddProcessGroup(ctx, g); err != nil {
			t.Fatal(err)
		}
	}
	rt, _ := startRuntime(t, confOf(inst), t.TempDir(), time.Minute)
	svc := NewServerService(rt)
	waitOverview(t, svc, 5*time.Second, "pair loaded", func(ov OverviewView) bool { return ov.Stats.Stopped == 3 })

	procs := []string{"one:one", "two:two_0", "two:two_1"}
	if err := svc.StartFile(path); err != nil {
		t.Fatal(err)
	}
	for _, p := range procs {
		inst.WaitFor(t, p, supervisor.StateRunning)
	}
	waitOverview(t, svc, 3*time.Second, "pair running", func(ov OverviewView) bool { return ov.Stats.Running == 3 })

	pids := map[string]int{}
	for _, p := range procs {
		info, _ := c.GetProcessInfo(ctx, p)
		pids[p] = info.PID
	}
	if err := svc.RestartFile(path); err != nil {
		t.Fatal(err)
	}
	for _, p := range procs {
		info := inst.WaitFor(t, p, supervisor.StateRunning)
		if info.PID == pids[p] {
			t.Errorf("%s not restarted (pid %d)", p, info.PID)
		}
	}

	if err := svc.StopFile(path); err != nil {
		t.Fatal(err)
	}
	for _, p := range procs {
		inst.WaitFor(t, p, supervisor.StateStopped)
	}
	if err := svc.StartFile(filepath.Join(inst.Dir(), "conf.d", "missing.ini")); err == nil {
		t.Error("StartFile on an unknown file: want error")
	}
}

func TestSetWatchPersists(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Sleeper, supervisortest.Program{Name: "other", Command: "sleep 3600"})
	state := t.TempDir()
	sleeperIni := filepath.Join(inst.Dir(), "conf.d", "sleeper.ini")
	watched := func(ov OverviewView, path string) (bool, bool) {
		for _, c := range ov.Configs {
			if c.Path == path {
				return c.Watched, true
			}
		}
		return false, false
	}

	rt, rec := startRuntime(t, confOf(inst), state, 100*time.Millisecond)
	svc := NewServerService(rt)
	waitOverview(t, svc, 5*time.Second, "configs listed", func(ov OverviewView) bool { return len(ov.Configs) == 2 })
	if err := svc.SetWatch(sleeperIni, false); err != nil {
		t.Fatal(err)
	}
	if w, ok := watched(svc.Overview(), sleeperIni); !ok || w {
		t.Fatalf("after SetWatch(false): watched=%v found=%v", w, ok)
	}
	ovs := rec.overviews()
	if w, _ := watched(ovs[len(ovs)-1], sleeperIni); w {
		t.Error("SetWatch did not emit an overview showing the change")
	}
	if err := svc.SetWatch("", true); err == nil {
		t.Error("SetWatch with an empty path: want error")
	}
	if _, err := os.Stat(filepath.Join(state, "watch.json")); err != nil {
		t.Fatalf("watch.json not written: %v", err)
	}
	rt.Stop()

	rt2, _ := startRuntime(t, confOf(inst), state, 100*time.Millisecond)
	svc2 := NewServerService(rt2)
	ov := waitOverview(t, svc2, 5*time.Second, "configs listed again", func(ov OverviewView) bool { return len(ov.Configs) == 2 })
	if w, _ := watched(ov, sleeperIni); w {
		t.Error("watch off did not survive a restart")
	}
	if w, _ := watched(ov, filepath.Join(inst.Dir(), "conf.d", "other.ini")); !w {
		t.Error("a config never toggled should be watched by default")
	}
}

func TestGroupAndAllActions(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Sleeper, supervisortest.Program{Name: "other", Command: "sleep 3600"})
	inst.WaitFor(t, "sleeper", supervisor.StateRunning)
	inst.WaitFor(t, "other", supervisor.StateRunning)
	rt, _ := startRuntime(t, confOf(inst), t.TempDir(), time.Minute)
	svc := NewServerService(rt)

	pid := func(name string) int {
		info, err := inst.Client().GetProcessInfo(context.Background(), name)
		if err != nil {
			t.Fatal(err)
		}
		return info.PID
	}
	before := pid("sleeper")
	if err := svc.RestartGroup("sleeper"); err != nil {
		t.Fatal(err)
	}
	if after := inst.WaitFor(t, "sleeper", supervisor.StateRunning).PID; after == before {
		t.Error("RestartGroup kept the same pid")
	}
	if err := svc.StopGroup("sleeper"); err != nil {
		t.Fatal(err)
	}
	inst.WaitFor(t, "sleeper", supervisor.StateStopped)
	if err := svc.StartGroup("sleeper"); err != nil {
		t.Fatal(err)
	}
	inst.WaitFor(t, "sleeper", supervisor.StateRunning)

	if err := svc.StopAll(); err != nil {
		t.Fatal(err)
	}
	waitOverview(t, svc, 3*time.Second, "all stopped", func(ov OverviewView) bool { return ov.Stats.Stopped == 2 })
	if err := svc.StartAll(); err != nil {
		t.Fatal(err)
	}
	waitOverview(t, svc, 3*time.Second, "all running", func(ov OverviewView) bool { return ov.Stats.Running == 2 })
	before = pid("other")
	if err := svc.RestartAll(); err != nil {
		t.Fatal(err)
	}
	if after := inst.WaitFor(t, "other", supervisor.StateRunning).PID; after == before {
		t.Error("RestartAll kept the same pid")
	}
	if err := svc.Restart("other:other"); err != nil {
		t.Fatal(err)
	}

	if err := svc.Signal("sleeper:sleeper", "CONT"); err != nil {
		t.Errorf("Signal: %v", err)
	}
	if err := svc.ClearLogs("sleeper:sleeper"); err != nil {
		t.Errorf("ClearLogs: %v", err)
	}
	if err := svc.Stop("nope:nope"); !errors.Is(err, supervisor.ErrBadName) {
		t.Errorf("Stop(unknown) = %v, want ErrBadName", err)
	}
	if err := svc.StartGroup("nope"); !errors.Is(err, supervisor.ErrBadName) {
		t.Errorf("StartGroup(unknown) = %v, want ErrBadName", err)
	}
}

func TestCheckForChangesAndMainLog(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Sleeper)
	rt, _ := startRuntime(t, confOf(inst), t.TempDir(), 100*time.Millisecond)
	svc := NewServerService(rt)
	waitOverview(t, svc, 5*time.Second, "reachable", func(ov OverviewView) bool { return ov.Server.Reachable })

	inst.WriteProgram(t, supervisortest.Program{Name: "fresh", Command: "sleep 3600"})
	diff, err := svc.CheckForChanges()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(diff.Added, []string{"fresh"}) {
		t.Errorf("diff = %+v, want fresh added", diff)
	}
	ov := waitOverview(t, svc, 5*time.Second, "fresh listed", func(ov OverviewView) bool { return len(ov.Configs) == 2 })
	if ov.Configs[0].FileName != "fresh.ini" || ov.Configs[0].Summary != "1 program · not loaded" {
		t.Errorf("fresh config = %+v", ov.Configs[0])
	}

	res, err := svc.MainLogLines(MainLogQuery{Query: "supervisord started"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Lines) == 0 || res.Lines[0].Level != "INFO" || res.Truncated || res.Matched != len(res.Lines) {
		t.Errorf("main log = %+v", res)
	}
}

func TestRestartDaemonMarksConfigsLoaded(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Sleeper)
	rt, _ := startRuntime(t, confOf(inst), t.TempDir(), 100*time.Millisecond)
	svc := NewServerService(rt)
	// Wait for a polled overview: the watcher must have seen the file as
	// loaded before it is edited, or the edit is merely "uncertain".
	waitOverview(t, svc, 5*time.Second, "polled", func(ov OverviewView) bool {
		return ov.Server.Reachable && len(ov.Configs) == 1 && len(ov.Configs[0].Processes) == 1
	})

	ini := filepath.Join(inst.Dir(), "conf.d", "sleeper.ini")
	f, err := os.OpenFile(ini, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("; edited\n"); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	waitOverview(t, svc, 5*time.Second, "changed badge", func(ov OverviewView) bool {
		return len(ov.Configs) == 1 && ov.Configs[0].Changed
	})

	if err := svc.RestartDaemon(); err != nil {
		t.Fatal(err)
	}
	if got, ok := rt.loadedSnapshot(ini); !ok || !strings.Contains(string(got), "; edited") {
		t.Fatalf("snapshot after RestartDaemon = %q, %v", got, ok)
	}
	waitOverview(t, svc, 15*time.Second, "back after restart", func(ov OverviewView) bool {
		return ov.Server.Reachable && len(ov.Configs) == 1 && !ov.Configs[0].Changed && ov.Stats.Running == 1
	})
}

func TestIssueService(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Failer)
	rt, rec := startRuntime(t, confOf(inst), t.TempDir(), 100*time.Millisecond)
	svc := NewServerService(rt)
	is := NewIssueService(rt)
	waitOverview(t, svc, 15*time.Second, "issue opened", func(ov OverviewView) bool { return ov.Stats.OpenIssues == 1 })

	open, err := is.List(IssueQuery{Status: "open"})
	if err != nil || len(open) != 1 {
		t.Fatalf("List(open) = %+v, %v", open, err)
	}
	if open[0].Config != filepath.Join(inst.Dir(), "conf.d", "failer.ini") || open[0].OpenedMs == 0 || open[0].Status != "open" {
		t.Errorf("issue = %+v", open[0])
	}
	if got, _ := is.List(IssueQuery{Status: "all", Query: "no-such-text"}); len(got) != 0 {
		t.Errorf("query filtered nothing: %+v", got)
	}
	if got, _ := is.List(IssueQuery{Status: "all", BeforeMs: open[0].OpenedMs}); len(got) != 0 {
		t.Errorf("before cursor kept the issue: %+v", got)
	}
	if got, _ := is.List(IssueQuery{Status: "all", BeforeMs: open[0].OpenedMs, BeforeID: open[0].ID}); len(got) != 0 {
		t.Errorf("a cursor at the issue itself kept it: %+v", got)
	}
	if got, _ := is.List(IssueQuery{Status: "all", BeforeMs: open[0].OpenedMs, BeforeID: open[0].ID + "~"}); len(got) != 1 {
		t.Errorf("a cursor after the issue in the same millisecond dropped it: %+v", got)
	}

	if c, err := is.Stats(); err != nil || c.Open != 1 || c.Resolved != 0 {
		t.Errorf("Stats before Resolve = %+v, %v", c, err)
	}
	n := len(rec.overviews())
	if err := is.Resolve(open[0].ID); err != nil {
		t.Fatal(err)
	}
	if ovs := rec.overviews(); len(ovs) == n {
		t.Error("Resolve emitted no overview")
	}
	if ov := svc.Overview(); ov.Stats.OpenIssues != 0 || len(ov.Issues) != 0 {
		t.Errorf("after Resolve: %+v / %+v", ov.Stats, ov.Issues)
	}
	if got, _ := is.List(IssueQuery{Status: "resolved"}); len(got) != 1 || got[0].Status != "resolved_user" || got[0].ClosedMs == 0 {
		t.Errorf("List(resolved) = %+v", got)
	}
	if c, err := is.Stats(); err != nil || c.Open != 0 || c.Resolved != 1 {
		t.Errorf("Stats after Resolve = %+v, %v", c, err)
	}
	if err := is.Dismiss(open[0].ID); err == nil {
		t.Error("Dismiss of a closed issue: want error")
	}

	if got, _ := is.List(IssueQuery{Kind: open[0].Kind}); len(got) != 1 {
		t.Errorf("kind=%s: %+v", open[0].Kind, got)
	}
	if got, _ := is.List(IssueQuery{Kind: "unreachable"}); len(got) != 0 {
		t.Errorf("kind=unreachable: %+v", got)
	}
	if got, _ := is.List(IssueQuery{FromMs: open[0].OpenedMs + 1}); len(got) != 0 {
		t.Errorf("From after the issue kept it: %+v", got)
	}
	if got, _ := is.List(IssueQuery{FromMs: open[0].OpenedMs, ToMs: open[0].OpenedMs + 1}); len(got) != 1 {
		t.Errorf("range around the issue dropped it: %+v", got)
	}
	if st, err := is.Stats(); err != nil || st.Total != 1 || st.Resolved != 1 || st.Open != 0 || st.Last24h != 1 {
		t.Errorf("Stats = %+v, %v", st, err)
	}
	// ResolveMany of a closed id resolves nothing; Delete removes it and emits.
	if n, err := is.ResolveMany([]string{open[0].ID}); err != nil || n != 0 {
		t.Errorf("ResolveMany(closed) = %d, %v", n, err)
	}
	hist, err := is.History("failer:failer", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) == 0 || hist[0].Process != "failer:failer" || hist[0].AtMs == 0 {
		t.Errorf("history = %+v", hist)
	}
}

func TestIssueServiceBulk(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Failer)
	rt, rec := startRuntime(t, confOf(inst), t.TempDir(), 100*time.Millisecond)
	svc := NewServerService(rt)
	is := NewIssueService(rt)
	waitOverview(t, svc, 15*time.Second, "issue opened", func(ov OverviewView) bool { return ov.Stats.OpenIssues == 1 })
	open, _ := is.List(IssueQuery{Status: "open"})
	if len(open) != 1 {
		t.Fatalf("open = %+v", open)
	}

	// An open issue is never deleted.
	if n, err := is.Delete([]string{open[0].ID}); err != nil || n != 0 {
		t.Fatalf("Delete(open) = %d, %v", n, err)
	}
	n := len(rec.overviews())
	if got, err := is.ResolveMany([]string{open[0].ID, "unknown"}); err != nil || got != 1 {
		t.Fatalf("ResolveMany = %d, %v", got, err)
	}
	if len(rec.overviews()) == n {
		t.Error("ResolveMany emitted no overview")
	}
	if st, _ := is.Stats(); st.Open != 0 || st.Resolved != 1 {
		t.Errorf("Stats = %+v", st)
	}
	n = len(rec.overviews())
	if got, err := is.Delete([]string{open[0].ID, "unknown"}); err != nil || got != 1 {
		t.Fatalf("Delete = %d, %v", got, err)
	}
	if len(rec.overviews()) == n {
		t.Error("Delete emitted no overview")
	}
	if all, _ := is.List(IssueQuery{Status: "all"}); len(all) != 0 {
		t.Errorf("after Delete: %+v", all)
	}
	if st, _ := is.Stats(); st != (IssueStats{}) {
		t.Errorf("Stats after Delete = %+v", st)
	}
}

func TestOverviewUnreachable(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "vsv")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	conf := writeConf(t, "[unix_http_server]\nfile="+dir+"/dead.sock\n")
	rt, _ := startRuntime(t, conf, t.TempDir(), 100*time.Millisecond)
	svc := NewServerService(rt)
	ov := waitOverview(t, svc, 5*time.Second, "unreachable", func(ov OverviewView) bool { return ov.Server.Health == "error" })
	if ov.Server.Reachable || ov.Server.Error == "" {
		t.Errorf("server = %+v", ov.Server)
	}
	if err := svc.Stop("x"); !errors.Is(err, supervisor.ErrUnreachable) {
		t.Errorf("Stop while unreachable = %v, want ErrUnreachable", err)
	}
}

func TestOverviewWithoutUsableConfig(t *testing.T) {
	for name, conf := range map[string]string{
		"missing":   filepath.Join(t.TempDir(), "missing.conf"),
		"no socket": writeConf(t, "[supervisord]\nlogfile=/tmp/x.log\n"),
	} {
		t.Run(name, func(t *testing.T) {
			rt, _ := startRuntime(t, conf, t.TempDir(), 100*time.Millisecond)
			svc := NewServerService(rt)
			ov := svc.Overview()
			if ov.Server.Reachable || ov.Server.Error == "" || ov.Server.Health != "error" {
				t.Errorf("server = %+v", ov.Server)
			}
			if err := svc.StartAll(); err == nil {
				t.Error("StartAll without a connection: want error")
			}
			if _, err := NewIssueService(rt).List(IssueQuery{Status: "all"}); err != nil {
				t.Errorf("issue list should still work: %v", err)
			}
		})
	}
}

func TestServiceLifecycle(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Sleeper)
	rec := &recorder{}
	rt := NewRuntime(RuntimeOptions{ConfigPath: confOf(inst), StateDir: t.TempDir(), Interval: 50 * time.Millisecond, Emit: rec.emit})
	t.Cleanup(rt.Stop)
	svc := NewServerService(rt)
	if err := svc.ServiceStartup(context.Background(), application.ServiceOptions{}); err != nil {
		t.Fatal(err)
	}
	waitOverview(t, svc, 5*time.Second, "reachable", func(ov OverviewView) bool { return ov.Server.Reachable })
	if err := svc.ServiceShutdown(); err != nil {
		t.Fatal(err)
	}
	n := len(rec.overviews())
	time.Sleep(200 * time.Millisecond)
	if m := len(rec.overviews()); m != n {
		t.Errorf("%d overviews emitted after shutdown", m-n)
	}
	if err := svc.ServiceShutdown(); err != nil { // idempotent
		t.Fatal(err)
	}
}

// faultServer is a minimal supervisord on a unix socket: getState says
// RUNNING, getAllProcessInfo lists nothing, and method answers with the
// fault code:name. It returns a config pointing at it.
func faultServer(t *testing.T, method string, code int, name string) string {
	t.Helper()
	dir := shortDir(t)
	sock := filepath.Join(dir, "s.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/xml")
		if strings.Contains(string(body), "<methodName>"+method+"</methodName>") {
			_, _ = fmt.Fprintf(w, `<?xml version='1.0'?><methodResponse><fault><value><struct>`+
				`<member><name>faultCode</name><value><int>%d</int></value></member>`+
				`<member><name>faultString</name><value><string>%s</string></value></member>`+
				`</struct></value></fault></methodResponse>`, code, name)
			return
		}
		value := `<array><data></data></array>`
		if strings.Contains(string(body), "supervisor.getState") {
			value = `<struct><member><name>statecode</name><value><int>1</int></value></member>` +
				`<member><name>statename</name><value><string>RUNNING</string></value></member></struct>`
		}
		_, _ = fmt.Fprintf(w, `<?xml version='1.0'?><methodResponse><params><param><value>%s</value></param></params></methodResponse>`, value)
	})}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	conf := filepath.Join(dir, "supervisord.conf")
	if err := os.WriteFile(conf, []byte("[unix_http_server]\nfile="+sock+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return conf
}

// M1: with logfile=syslog, or none, readLog faults NO_FILE. The message
// says so, not that a command is missing.
func TestMainLogWithoutLogFile(t *testing.T) {
	conf := faultServer(t, "supervisor.readLog", 20, "NO_FILE: syslog")
	rt, _ := startRuntime(t, conf, t.TempDir(), 50*time.Millisecond)
	svc := NewServerService(rt)
	waitOverview(t, svc, 5*time.Second, "reachable", func(ov OverviewView) bool { return ov.Server.Reachable })
	_, err := svc.MainLogLines(MainLogQuery{})
	want := "supervisord writes no log file (logfile= is unset or syslog). Set [supervisord] logfile= to see it here."
	if err == nil || err.Error() != want {
		t.Errorf("MainLogLines = %v, want %q", err, want)
	}
	if !errors.Is(err, supervisor.ErrNoFile) {
		t.Errorf("MainLogLines error %v does not wrap ErrNoFile", err)
	}
}

func TestFilterMainLog(t *testing.T) {
	base := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	mk := func(min int, lvl, text string) supervisor.MainLogLine {
		return supervisor.MainLogLine{At: base.Add(time.Duration(min) * time.Minute), Level: lvl, Text: text}
	}
	lines := []supervisor.MainLogLine{
		mk(0, "INFO", "spawned: 'a'"), mk(1, "WARN", "exited: a"), mk(2, "CRIT", "Boom\nTraceback"),
		mk(3, "DEBG", "noise"), mk(4, "INFO", "spawned: 'b'"),
	}
	texts := func(r MainLogResult) []string {
		out := []string{}
		for _, l := range r.Lines {
			out = append(out, l.Text)
		}
		return out
	}
	tests := []struct {
		name string
		q    MainLogQuery
		want []string
		n    int
	}{
		{"all", MainLogQuery{}, []string{"spawned: 'a'", "exited: a", "Boom\nTraceback", "noise", "spawned: 'b'"}, 5},
		{"levels", MainLogQuery{Levels: []string{"WARN", "CRIT"}}, []string{"exited: a", "Boom\nTraceback"}, 2},
		{"search is case-insensitive and sees tracebacks", MainLogQuery{Query: "TRACEBACK"}, []string{"Boom\nTraceback"}, 1},
		{"from is inclusive, to exclusive", MainLogQuery{FromMs: base.Add(time.Minute).UnixMilli(), ToMs: base.Add(3 * time.Minute).UnixMilli()}, []string{"exited: a", "Boom\nTraceback"}, 2},
		{"limit keeps the newest", MainLogQuery{Limit: 2}, []string{"noise", "spawned: 'b'"}, 5},
		{"no match", MainLogQuery{Query: "zzz"}, []string{}, 0},
	}
	for _, tc := range tests {
		got := filterMainLog(lines, tc.q)
		if !slices.Equal(texts(got), tc.want) || got.Matched != tc.n {
			t.Errorf("%s: lines %q matched %d, want %q matched %d", tc.name, texts(got), got.Matched, tc.want, tc.n)
		}
	}
}
