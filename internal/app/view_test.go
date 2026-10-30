package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/touhidurabir/visor/internal/supervisor"
	"github.com/touhidurabir/visor/internal/supervisor/supervisortest"
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

func TestServerServiceAgainstHarness(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Sleeper)
	inst.WaitFor(t, "sleeper", supervisor.StateRunning)
	svc := NewServerService(filepath.Join(inst.Dir(), "supervisord.conf"))
	views, err := svc.ListProcesses()
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range views {
		if v.Name == "sleeper" {
			if v.State != "RUNNING" {
				t.Errorf("state = %s", v.State)
			}
			return
		}
	}
	t.Fatalf("sleeper not in %+v", views)
}

func writeConf(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "supervisord.conf")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestServerServiceUnreachable(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "vsv")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	conf := writeConf(t, "[unix_http_server]\nfile="+dir+"/dead.sock\n")
	_, err = NewServerService(conf).ListProcesses()
	if !errors.Is(err, supervisor.ErrUnreachable) {
		t.Fatalf("err = %v, want ErrUnreachable", err)
	}
}

func TestServerServiceNoSocket(t *testing.T) {
	conf := writeConf(t, "[supervisord]\nlogfile=/tmp/x.log\n")
	_, err := NewServerService(conf).ListProcesses()
	if err == nil {
		t.Fatal("want error for config without socket")
	}
}

func TestServerServiceNoConfig(t *testing.T) {
	_, err := NewServerService(filepath.Join(t.TempDir(), "missing.conf")).ListProcesses()
	if err == nil {
		t.Fatal("want error")
	}
}
