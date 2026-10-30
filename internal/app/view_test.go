package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

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
