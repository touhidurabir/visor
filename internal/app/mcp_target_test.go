package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/touhidurabir/visor/internal/mcp"
	"github.com/touhidurabir/visor/internal/settings"
	"github.com/touhidurabir/visor/internal/supervisor/supervisortest"
	"github.com/touhidurabir/visor/internal/usage"
)

// logger writes an info line, an error with a trace line, then sleeps; its
// config has an environment with a secret.
var logger = supervisortest.Program{
	Name:    "logger",
	Command: `sh -c 'echo "INFO ok"; echo "ERROR boom"; echo "  at trace line"; exec sleep 3600'`,
	Extra:   map[string]string{"environment": `SECRET="s3cr3t",MODE=dev`},
}

func js(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func decode[T any](t *testing.T, v any) T {
	t.Helper()
	var out T
	if err := json.Unmarshal([]byte(js(t, v)), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func targetFor(t *testing.T, inst *supervisortest.Instance) (*tabTarget, *ServerService) {
	t.Helper()
	rt, _ := startRuntime(t, confOf(inst), t.TempDir(), 100*time.Millisecond)
	tab := newTab(rt, func(string, any) {}, "")
	diag := func() (any, error) { return map[string]any{"doctor": true}, nil }
	return &tabTarget{t: tab, name: "This Mac", diagnose: diag}, tab.Server
}

func waitLog(t *testing.T, tg *tabTarget, process, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		out, err := tg.Logs(mcp.LogsIn{Process: process})
		if err == nil && strings.Contains(js(t, out), want) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("%s's log never had %q", process, want)
}

func TestMCPTargetReads(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Sleeper, logger)
	tg, svc := targetFor(t, inst)
	waitRunning(t, svc, "sleeper:sleeper")
	waitRunning(t, svc, "logger:logger")
	waitLog(t, tg, "logger", "trace line")

	st, err := tg.Status()
	if err != nil {
		t.Fatal(err)
	}
	s := decode[struct {
		Counts      struct{ Processes, Running int } `json:"counts"`
		Supervisord struct {
			Reachable bool
			Version   string
		} `json:"supervisord"`
	}](t, st)
	if s.Counts.Processes != 2 || s.Counts.Running != 2 || !s.Supervisord.Reachable || s.Supervisord.Version == "" {
		t.Fatalf("status: %s", js(t, st))
	}

	ps, _ := tg.Processes(mcp.ProcessesIn{State: "running"})
	if n := len(decode[struct{ Processes []any }](t, ps).Processes); n != 2 {
		t.Fatalf("running processes: %s", js(t, ps))
	}
	ps, _ = tg.Processes(mcp.ProcessesIn{Name: "log"})
	got := decode[struct {
		Processes []struct{ Name, Config, State string }
	}](t, ps).Processes
	if len(got) != 1 || got[0].Name != "logger:logger" || !strings.HasSuffix(got[0].Config, "logger.ini") {
		t.Fatalf("by name: %s", js(t, ps))
	}

	p, err := tg.Process(mcp.ProcessIn{Name: "logger"})
	if err != nil {
		t.Fatal(err)
	}
	if pj := js(t, p); strings.Contains(pj, "s3cr3t") || !strings.Contains(pj, `SECRET=\"«hidden»\"`) {
		t.Fatalf("process settings leak or miss the environment: %s", pj)
	}
	if _, err := tg.Process(mcp.ProcessIn{Name: "nope"}); err == nil {
		t.Fatal("an unknown process gave no error")
	}

	l, _ := tg.Logs(mcp.LogsIn{Process: "logger", ErrorsOnly: true})
	if text := decode[struct{ Text string }](t, l).Text; text != "ERROR boom\n  at trace line\n" {
		t.Fatalf("errors only: %q", text)
	}
	l, _ = tg.Logs(mcp.LogsIn{Process: "logger", Lines: 1})
	one := decode[struct {
		Text   string
		Cursor int
	}](t, l)
	if one.Text != "  at trace line\n" || one.Cursor == 0 {
		t.Fatalf("last line: %+v", one)
	}
	type page struct {
		Text    string
		Cursor  int
		AtStart bool `json:"atStart"`
	}
	l, _ = tg.Logs(mcp.LogsIn{Process: "logger", Before: one.Cursor})
	if back := decode[page](t, l); back.Text != "INFO ok\nERROR boom\n" || !back.AtStart || back.Cursor != 0 {
		t.Fatalf("paging back to the start: %+v", back)
	}
	// Errors only, one line at a time: each error line once, then the start.
	var pages []string
	before := 0
	for range 5 {
		l, _ = tg.Logs(mcp.LogsIn{Process: "logger", ErrorsOnly: true, Lines: 1, Before: before})
		p := decode[page](t, l)
		pages = append(pages, p.Text)
		if p.AtStart {
			break
		}
		before = p.Cursor
	}
	if strings.Join(pages, "|") != "  at trace line\n|ERROR boom\n|" {
		t.Fatalf("errors only, paged: %q", pages)
	}
	if _, err := tg.Logs(mcp.LogsIn{Process: "logger", Stream: "stdlog"}); err == nil {
		t.Fatal("a bad stream gave no error")
	}

	sr, err := tg.SearchLogs(mcp.SearchIn{Process: "logger", Query: "BOOM"})
	if err != nil || !strings.Contains(js(t, sr), "ERROR boom") {
		t.Fatalf("search: %s %v", js(t, sr), err)
	}
	if _, err := tg.SearchLogs(mcp.SearchIn{Process: "logger", Query: "x", WholeFile: true}); err == nil ||
		!strings.Contains(err.Error(), "Search the whole log file") {
		t.Fatalf("whole-file search while Settings don't allow it: %v", err)
	}

	path := confFile(inst, "logger")
	rc, _ := tg.ReadConfig(mcp.PathIn{Path: path}, false)
	f := decode[struct{ Content, Hash string }](t, rc)
	if strings.Contains(f.Content, "s3cr3t") || !strings.Contains(f.Content, "«hidden»") || f.Hash == "" {
		t.Fatalf("read_config hides nothing: %s", js(t, rc))
	}
	rc, _ = tg.ReadConfig(mcp.PathIn{Path: path}, true)
	if !strings.Contains(js(t, rc), "s3cr3t") {
		t.Fatal("read_config with env allowed hides the value")
	}
	ck, err := tg.CheckConfig(mcp.CheckIn{Path: path}, false)
	if err != nil || strings.Contains(js(t, ck), "s3cr3t") || !strings.Contains(js(t, ck), "Summary") {
		t.Fatalf("check_config: %v %s", err, js(t, ck))
	}
	ck, _ = tg.CheckConfig(mcp.CheckIn{Content: "[program:x]\nnumprocs=two\n", FileName: "x.ini"}, false)
	if decode[struct{ Errors int }](t, ck).Errors == 0 {
		t.Fatalf("a bad draft has no errors: %s", js(t, ck))
	}

	dr, err := tg.DraftProgram(mcp.DraftIn{Template: "queue", Values: map[string]string{
		"name": "emails", "command": "sleep 3600", "directory": inst.Dir(),
		"stdout_logfile": filepath.Join(inst.Dir(), "logs", "emails.log"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	d := decode[struct {
		Content, FileName string
		Check             struct{ Errors int }
	}](t, dr)
	if !strings.Contains(d.Content, "[program:emails]") || !strings.Contains(d.Content, "numprocs=3") || d.FileName != "emails.ini" {
		t.Fatalf("draft: %s", js(t, dr))
	}
	if _, err := tg.DraftProgram(mcp.DraftIn{Template: "rails"}); err == nil {
		t.Fatal("an unknown template gave no error")
	}

	for name, call := range map[string]func() (any, error){
		"configs":  tg.Configs,
		"issues":   func() (any, error) { return tg.Issues(mcp.IssuesIn{}) },
		"activity": func() (any, error) { return tg.Activity(mcp.ActivityIn{Since: "1h"}) },
		"pending":  func() (any, error) { return tg.PendingChanges(mcp.OptPathIn{}) },
		"mainlog":  func() (any, error) { return tg.SupervisordLog(mcp.SupervisordLogIn{Lines: 5}) },
		"backups":  func() (any, error) { return tg.Backups(mcp.PathIn{Path: path}) },
		"diagnose": tg.Diagnose,
	} {
		out, err := call()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if b := js(t, out); !strings.HasPrefix(b, "{") {
			t.Fatalf("%s isn't an object: %s", name, b)
		}
	}
	if _, err := tg.Activity(mcp.ActivityIn{Since: "yesterday-ish"}); err == nil {
		t.Fatal("a bad since gave no error")
	}
}

func TestMCPTargetChanges(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Sleeper, logger)
	tg, svc := targetFor(t, inst)
	waitRunning(t, svc, "sleeper:sleeper")
	before := waitRunning(t, svc, "logger:logger").PID

	for _, target := range []string{"sleeper", "sleeper:*", confFile(inst, "sleeper")} {
		if _, err := tg.Restart("Claude Code", mcp.TargetIn{Target: target}); err != nil {
			t.Fatalf("restart %s: %v", target, err)
		}
	}
	if _, err := tg.Stop("Claude Code", mcp.TargetIn{Target: "logger"}); err != nil {
		t.Fatal(err)
	}
	if _, err := tg.Start("Claude Code", mcp.TargetIn{Target: "logger"}); err != nil {
		t.Fatal(err)
	}
	if after := waitRunning(t, svc, "logger:logger").PID; after == before {
		t.Fatal("stop and start left the same PID")
	}
	if _, err := tg.Signal("Claude Code", mcp.SignalIn{Name: "sleeper", Signal: "HUP"}); err != nil {
		t.Fatal(err)
	}

	path := confFile(inst, "sleeper")
	rc, _ := tg.ReadConfig(mcp.PathIn{Path: path}, false)
	f := decode[struct{ Content, Hash string }](t, rc)
	if _, err := tg.SaveConfig("Codex", mcp.SaveIn{Path: path, Content: f.Content + "\n", Hash: "stale"}); err == nil {
		t.Fatal("a stale hash saved")
	}
	lp := confFile(inst, "logger")
	lc, _ := tg.ReadConfig(mcp.PathIn{Path: lp}, false)
	lf := decode[struct{ Content, Hash string }](t, lc)
	// Hidden lines left as read are put back from the file; a changed one is refused.
	if _, err := tg.SaveConfig("Codex", mcp.SaveIn{Path: lp, Content: strings.Replace(lf.Content, "SECRET=", "SECRET2=", 1), Hash: lf.Hash}); err == nil ||
		!strings.Contains(err.Error(), "isn't as Visor showed it") {
		t.Fatalf("a changed hidden line saved: %v", err)
	}
	if _, err := tg.SaveConfig("Codex", mcp.SaveIn{Path: lp, Content: strings.Replace(lf.Content, "exec sleep 3600", "exec sleep 3601", 1), Hash: lf.Hash}); err != nil {
		t.Fatalf("a save around hidden values: %v", err)
	}
	if disk, _ := os.ReadFile(lp); !strings.Contains(string(disk), `SECRET="s3cr3t"`) || !strings.Contains(string(disk), "sleep 3601") {
		t.Fatalf("the file after the save: %s", disk)
	}
	res, err := tg.SaveConfig("Codex", mcp.SaveIn{Path: path, Content: strings.Replace(f.Content, "sleep 3600", "sleep 3601", 1), Hash: f.Hash})
	if err != nil || !strings.Contains(js(t, res), "sleeper") {
		t.Fatalf("save: %v %s", err, js(t, res))
	}

	extra := "[program:extra]\ncommand=sleep 3600\nstdout_logfile=" + filepath.Join(inst.Dir(), "logs", "extra.log") + "\n"
	if _, err := tg.CreateConfig("Codex", mcp.CreateIn{FileName: "extra.ini", Content: extra}); err != nil {
		t.Fatal(err)
	}
	waitRunning(t, svc, "extra:extra")
	ep := filepath.Join(inst.Dir(), "conf.d", "extra.ini")
	if _, err := tg.RemoveConfig("Codex", mcp.PathIn{Path: ep}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ep); !os.IsNotExist(err) {
		t.Fatalf("removed file still there: %v", err)
	}
	if _, err := tg.RestoreConfig("Codex", mcp.RestoreIn{Path: ep}); err != nil {
		t.Fatalf("restore a removed file: %v", err)
	}
	waitRunning(t, svc, "extra:extra")
	bk, _ := tg.Backups(mcp.PathIn{Path: path})
	if !strings.Contains(js(t, bk), `"id"`) {
		t.Fatalf("no backup after save: %s", js(t, bk))
	}

	// Changed on disk outside Visor: pending, then applied.
	cur, _ := os.ReadFile(path)
	if err := os.WriteFile(path, []byte(strings.Replace(string(cur), "sleep 3601", "sleep 3602", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	pc, err := tg.PendingChanges(mcp.OptPathIn{Path: path})
	if err != nil || !strings.Contains(js(t, pc), "sleeper") {
		t.Fatalf("pending: %v %s", err, js(t, pc))
	}
	ap, err := tg.Apply("Codex", mcp.OptPathIn{Path: path})
	if err != nil || !strings.Contains(js(t, ap), "sleeper") {
		t.Fatalf("apply: %v %s", err, js(t, ap))
	}
}

func TestMCPTargetReconnecting(t *testing.T) {
	fastReconnect(t)
	inst := supervisortest.Start(t, supervisortest.Sleeper)
	f := startRemoteRuntime(t, inst, nil)
	waitRunning(t, f.svc, "sleeper:sleeper")
	tg := &tabTarget{t: newTab(f.rt, func(string, any) {}, ""), name: "vm"}
	f.ssh.Freeze()
	waitOverview(t, f.svc, 15*time.Second, "reconnecting", func(ov OverviewView) bool { return ov.Host.Conn == ConnReconnecting })
	start := time.Now()
	_, err := tg.Status()
	if err == nil || err.Error() != "vm is disconnected; Visor is reconnecting" || time.Since(start) > time.Second {
		t.Fatalf("status while reconnecting: %v after %s", err, time.Since(start))
	}
	if _, err := tg.Restart("x", mcp.TargetIn{Target: "sleeper"}); err == nil || !strings.Contains(err.Error(), "reconnecting") {
		t.Fatalf("restart while reconnecting: %v", err)
	}
}

// get_status counts as failing what the dashboard does: a clean, expected
// exit is not failing.
func TestMCPStatusFailingMatchesTheDashboard(t *testing.T) {
	oneshot := supervisortest.Program{Name: "oneshot", Command: "sh -c 'exit 0'", Extra: map[string]string{"startsecs": "0", "startretries": "0", "autorestart": "false"}}
	inst := supervisortest.Start(t, supervisortest.Sleeper, supervisortest.Failer, oneshot)
	tg, svc := targetFor(t, inst)
	waitRunning(t, svc, "sleeper:sleeper")
	waitOverview(t, svc, 15*time.Second, "both exited", func(ov OverviewView) bool {
		a, _, okA := findProc(ov, "failer:failer")
		b, _, okB := findProc(ov, "oneshot:oneshot")
		return okA && okB && a.State == "EXITED" && b.State == "EXITED"
	})
	st, err := tg.Status()
	if err != nil {
		t.Fatal(err)
	}
	got := decode[struct {
		Failing []struct{ Name string } `json:"failing"`
		Counts  struct{ Failing int }   `json:"counts"`
	}](t, st)
	if len(got.Failing) != 1 || got.Failing[0].Name != "failer:failer" || got.Counts.Failing != 1 {
		t.Fatalf("failing: %s", js(t, st))
	}
}

// A Read only server is never asked to re-read its configs: get_pending_changes
// answers from what Visor already knows.
func TestMCPPendingOnAReadOnlyServer(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Sleeper)
	f := startRemoteRuntime(t, inst, func(s *settings.Server) { s.ReadOnly = true })
	waitRunning(t, f.svc, "sleeper:sleeper")
	tg := &tabTarget{t: newTab(f.rt, func(string, any) {}, ""), name: "vm"}
	out, err := tg.PendingChanges(mcp.OptPathIn{})
	if err != nil || !strings.Contains(js(t, out), "Read only") {
		t.Fatalf("pending on a Read only server: %v %s", err, js(t, out))
	}
}

func TestMCPTargetUsage(t *testing.T) {
	if usage.Local() == nil {
		t.Skip("no local sampler on this OS")
	}
	fastUsage(t)
	inst := supervisortest.Start(t, supervisortest.Sleeper)
	tg, svc := targetFor(t, inst)
	waitRunning(t, svc, "sleeper:sleeper")
	waitOverview(t, svc, 5*time.Second, "a sample", func(ov OverviewView) bool {
		p, _, ok := findProc(ov, "sleeper:sleeper")
		return ok && p.MemBytes > 0
	})
	res, err := tg.Usage(mcp.UsageIn{Process: "sleeper"})
	if err != nil {
		t.Fatal(err)
	}
	u := decode[struct {
		Processes []struct {
			Name        string
			MemoryBytes int64 `json:"memory_bytes"`
			Processes   int
			Hour        struct {
				PeakMemoryBytes int64 `json:"peak_memory_bytes"`
			}
		}
		SampleEveryS float64 `json:"sample_every_s"`
	}](t, res)
	if len(u.Processes) != 1 || u.Processes[0].Name != "sleeper:sleeper" || u.Processes[0].MemoryBytes <= 0 || u.Processes[0].Processes < 1 || u.Processes[0].Hour.PeakMemoryBytes <= 0 {
		t.Fatalf("usage %+v", u)
	}
	if _, err := tg.Usage(mcp.UsageIn{Process: "nope"}); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("an unknown process: %v", err)
	}
}
