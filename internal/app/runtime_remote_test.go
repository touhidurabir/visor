package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/touhidurabir/visor/internal/remote"
	"github.com/touhidurabir/visor/internal/remote/remotetest"
	"github.com/touhidurabir/visor/internal/settings"
	"github.com/touhidurabir/visor/internal/supervisor/supervisortest"
)

// noSSHConfig keeps tests away from the real ~/.ssh/config.
func noSSHConfig(t *testing.T) {
	t.Helper()
	old := resolveTarget
	resolveTarget = func(_ context.Context, tg remote.Target) (remote.Target, error) { return tg, nil }
	t.Cleanup(func() { resolveTarget = old })
}

type remoteFixture struct {
	rt     *Runtime
	svc    *ServerService
	server settings.Server
	ssh    *remotetest.Server
	k      settings.Keychain
	hk     remote.HostKeys
}

// startRemoteRuntime runs a test SSH server in front of inst and a Runtime
// managing it as a saved server.
func startRemoteRuntime(t *testing.T, inst *supervisortest.Instance, mod func(*settings.Server)) remoteFixture {
	t.Helper()
	noSSHConfig(t)
	state := t.TempDir()
	ssh := remotetest.Start(t, remotetest.Options{Password: "pw"})
	hk := remote.HostKeys{VisorFile: filepath.Join(state, "known_hosts")}
	if err := hk.Trust(ssh.Host, ssh.Port, ssh.HostKey); err != nil {
		t.Fatal(err)
	}
	k := &settings.MemoryKeychain{}
	srv := settings.Server{
		ID: settings.NewServerID(), Name: "vm", Host: ssh.Host, Port: ssh.Port, User: "visor",
		ConfigPath: inst.ConfigPath(),
	}.Normalize()
	if mod != nil {
		mod(&srv)
	}
	if err := settings.SetServerSecret(k, srv.ID, settings.SecretPassword, "pw"); err != nil {
		t.Fatal(err)
	}
	rt := NewRuntime(RuntimeOptions{
		StateDir: settings.ServerStateDir(state, srv.ID),
		Interval: 100 * time.Millisecond,
		Keychain: k,
		Remote:   &RemoteSource{Server: srv, Connect: ConnectServer(srv, k, hk)},
	})
	if err := rt.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rt.Stop)
	return remoteFixture{rt: rt, svc: NewServerService(rt), server: srv, ssh: ssh, k: k, hk: hk}
}

func TestRemoteRuntimePollsAndActs(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Sleeper, worker)
	f := startRemoteRuntime(t, inst, nil)
	waitRunning(t, f.svc, "sleeper:sleeper")
	ov := f.svc.Overview()
	if !ov.Host.Remote || ov.Host.ID != f.server.ID || ov.Host.Via != remote.ViaForward || ov.Host.Address == "" {
		t.Fatalf("host: %+v", ov.Host)
	}
	if len(ov.Configs) != 2 {
		t.Fatalf("configs: %d", len(ov.Configs))
	}
	if err := f.svc.Stop("sleeper:sleeper"); err != nil {
		t.Fatal(err)
	}
	if p, _ := inst.Client().GetProcessInfo(context.Background(), "sleeper:sleeper"); p.StateName != "STOPPED" {
		t.Fatalf("the stop did not reach the server's supervisord: %s", p.StateName)
	}
	if err := f.svc.Start("sleeper:sleeper"); err != nil {
		t.Fatal(err)
	}

	cs := NewConfigService(f.rt)
	path := filepath.Join(inst.Dir(), "conf.d", "sleeper.ini")
	fv, err := cs.Read(path)
	if err != nil || fv.Writable || fv.Content == "" {
		t.Fatalf("read: %+v %v", fv, err)
	}
	if _, err := cs.Save(path, fv.Content+"\n", fv.Hash); !errors.Is(err, ErrRemoteReadOnly) {
		t.Fatalf("save: %v", err)
	}
	if _, err := cs.ApplyFile(path); !errors.Is(err, ErrRemoteReadOnly) {
		t.Fatalf("apply: %v", err)
	}
	if _, err := cs.LoadFile(path); !errors.Is(err, ErrRemoteReadOnly) {
		t.Fatalf("load: %v", err)
	}
	if _, err := cs.Create("x.ini", "[program:x]\ncommand=sleep 1\n"); !errors.Is(err, ErrRemoteReadOnly) {
		t.Fatalf("create: %v", err)
	}
	if _, err := f.svc.RemoveFromSupervisor(path); !errors.Is(err, ErrRemoteReadOnly) {
		t.Fatalf("remove: %v", err)
	}
	if _, err := f.svc.ConfigDetail(path); err != nil {
		t.Fatalf("detail: %v", err)
	}
}

func TestRemoteRuntimeReadOnlyServer(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Sleeper)
	f := startRemoteRuntime(t, inst, func(s *settings.Server) { s.ReadOnly = true })
	waitRunning(t, f.svc, "sleeper:sleeper")
	ov := f.svc.Overview()
	if !ov.Host.ReadOnly {
		t.Fatal("host should say read only")
	}
	for name, do := range map[string]func() error{
		"stop":    func() error { return f.svc.Stop("sleeper:sleeper") },
		"restart": func() error { return f.svc.Restart("sleeper:sleeper") },
		"all":     f.svc.StopAll,
		"signal":  func() error { return f.svc.Signal("sleeper:sleeper", "HUP") },
		"clear":   func() error { return f.svc.ClearLogs("sleeper:sleeper") },
		"daemon":  f.svc.RestartDaemon,
	} {
		if err := do(); !errors.Is(err, ErrServerReadOnly) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestRemoteRuntimeConnectError(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Sleeper)
	f := startRemoteRuntime(t, inst, func(s *settings.Server) { s.ConfigPath = "/nonexistent/supervisord.conf" })
	ov := f.svc.Overview()
	if ov.Server.Error == "" || !ov.Host.Remote {
		t.Fatalf("want an error in the overview: %+v", ov.Server)
	}
}
