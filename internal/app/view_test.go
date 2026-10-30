package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

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
