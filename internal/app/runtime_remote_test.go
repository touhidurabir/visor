package app

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/touhidurabir/visor/internal/notify"
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
	return startRemoteRuntimeWith(t, inst, mod, nil)
}

func startRemoteRuntimeWith(t *testing.T, inst *supervisortest.Instance, mod func(*settings.Server), sender notify.Sender) remoteFixture {
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
		Notifier: sender,
		Remote:   &RemoteSource{Server: srv, Connect: ConnectServer(k, hk)},
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

func TestRemoteReadOnlyStillReadsTheMainLog(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Sleeper)
	f := startRemoteRuntime(t, inst, func(s *settings.Server) { s.ReadOnly = true })
	waitRunning(t, f.svc, "sleeper:sleeper")
	if _, err := f.svc.MainLogLines(MainLogQuery{Limit: 10}); err != nil {
		t.Fatalf("main log on a Read only server: %v", err)
	}
	// A reread changes what supervisord has loaded: Read only refuses it.
	if _, err := f.svc.CheckForChanges(); !errors.Is(err, ErrServerReadOnly) {
		t.Fatalf("reread on a Read only server: %v", err)
	}
}

func TestSetServerAppliesReadOnlyAtOnce(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Sleeper)
	f := startRemoteRuntime(t, inst, nil)
	waitRunning(t, f.svc, "sleeper:sleeper")
	s := f.server
	s.ReadOnly, s.Tag = true, settings.TagProduction
	f.rt.SetServer(s)
	if err := f.svc.Stop("sleeper:sleeper"); !errors.Is(err, ErrServerReadOnly) {
		t.Fatalf("stop after Read only was saved: %v", err)
	}
	if h := f.svc.Overview().Host; !h.ReadOnly || h.Tag != settings.TagProduction {
		t.Fatalf("host: %+v", h)
	}
}

func TestIssuesRoute(t *testing.T) {
	if IssuesRoute("") != RouteIssues || IssuesRoute(LocalID) != RouteIssues || IssuesRoute("../x") != RouteIssues {
		t.Fatal("this computer")
	}
	if got := IssuesRoute("s0123456789"); got != "#/s/s0123456789/issues" {
		t.Fatal(got)
	}
}

func TestRemoteNotificationNamesTheServer(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Sleeper)
	sender := &fakeSender{}
	f := startRemoteRuntimeWith(t, inst, nil, sender)
	waitRunning(t, f.svc, "sleeper:sleeper")
	if err := f.svc.SetWatch(filepath.Join(inst.Dir(), "conf.d", "sleeper.ini"), true); err != nil {
		t.Fatal(err)
	}
	// Killed behind supervisord's back: an unexpected exit.
	p, _ := inst.Client().GetProcessInfo(context.Background(), "sleeper:sleeper")
	if err := syscall.Kill(p.PID, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		for _, m := range sender.messages() {
			if !strings.HasSuffix(m.Title, " on vm") || m.Server != f.server.ID || m.Route != IssuesRoute(f.server.ID) {
				t.Fatalf("notification %+v", m)
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("no notification")
}

func fastReconnect(t *testing.T) {
	t.Helper()
	f, m := reconnectFirst, reconnectMax
	reconnectFirst, reconnectMax = 100*time.Millisecond, 400*time.Millisecond
	ki, km := remote.KeepaliveInterval, remote.KeepaliveMax
	remote.KeepaliveInterval, remote.KeepaliveMax = 100*time.Millisecond, 2
	t.Cleanup(func() {
		reconnectFirst, reconnectMax = f, m
		remote.KeepaliveInterval, remote.KeepaliveMax = ki, km
	})
}

func TestRemoteReconnectsAfterADrop(t *testing.T) {
	fastReconnect(t)
	inst := supervisortest.Start(t, supervisortest.Sleeper)
	f := startRemoteRuntime(t, inst, nil)
	waitRunning(t, f.svc, "sleeper:sleeper")
	before := len(f.ssh.UnixDials())
	f.ssh.DropAll()
	// It reconnects on its own: an action soon works over a new connection.
	deadline := time.Now().Add(10 * time.Second)
	for {
		err := f.svc.Restart("sleeper:sleeper")
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no recovery: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if ov := f.svc.Overview(); ov.Host.Conn != ConnConnected {
		t.Fatalf("conn %q", ov.Host.Conn)
	}
	if len(f.ssh.UnixDials()) <= before {
		t.Fatal("no new forwarded connection")
	}
}

func TestRemoteHostDownOpensItsOwnIssue(t *testing.T) {
	fastReconnect(t)
	inst := supervisortest.Start(t, supervisortest.Sleeper)
	f := startRemoteRuntime(t, inst, nil)
	waitRunning(t, f.svc, "sleeper:sleeper")
	f.ssh.Freeze()
	waitOverview(t, f.svc, 15*time.Second, "reconnecting", func(ov OverviewView) bool { return ov.Host.Conn == ConnReconnecting })
	is := NewIssueService(f.rt)
	deadline := time.Now().Add(15 * time.Second)
	for {
		list, _ := is.List(IssueQuery{Status: "open"})
		if len(list) > 0 {
			if list[0].Kind != "host_down" {
				t.Fatalf("issue %+v", list[0])
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no host_down issue")
		}
		time.Sleep(100 * time.Millisecond)
	}
	f.ssh.Thaw()
	waitOverview(t, f.svc, 15*time.Second, "back", func(ov OverviewView) bool { return ov.Host.Conn == ConnConnected && ov.Server.Reachable })
	deadline = time.Now().Add(10 * time.Second)
	for {
		list, _ := is.List(IssueQuery{Status: "open"})
		if len(list) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("still open: %+v", list)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestActivityOutsideAndByVisor(t *testing.T) {
	old := activityGroupWait
	activityGroupWait = 200 * time.Millisecond
	t.Cleanup(func() { activityGroupWait = old })
	inst := supervisortest.Start(t, supervisortest.Sleeper, worker)
	sender := &fakeSender{}
	f := startRemoteRuntimeWith(t, inst, nil, sender)
	waitRunning(t, f.svc, "sleeper:sleeper")
	waitRunning(t, f.svc, "worker:worker")
	is := NewIssueService(f.rt)

	// Outside Visor: a restart straight over XML-RPC.
	ctx := context.Background()
	if err := inst.Client().StopProcess(ctx, "sleeper:sleeper", true); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if err := inst.Client().StartProcess(ctx, "sleeper:sleeper", true); err != nil {
		t.Fatal(err)
	}
	// By Visor.
	if err := f.svc.Restart("worker:worker"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(20 * time.Second)
	var outside, mine bool
	for time.Now().Before(deadline) && (!outside || !mine) {
		for _, e := range is.Activity(ActivityQuery{Kind: "processes"}).Events {
			switch {
			case e.Process == "sleeper:sleeper" && e.Kind == "process_restarted" && !e.ByVisor:
				outside = true
			case e.Process == "worker:worker" && e.ByVisor:
				mine = true
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !outside || !mine {
		t.Fatalf("outside %v, by Visor %v: %+v", outside, mine, is.Activity(ActivityQuery{}).Events)
	}
	// One notification, for the outside restart only.
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var acts []string
		for _, m := range sender.messages() {
			if strings.HasPrefix(m.ID, "activity-") {
				acts = append(acts, m.Title)
			}
		}
		if len(acts) > 0 {
			if len(acts) != 1 || !strings.Contains(acts[0], "sleeper:sleeper restarted outside Visor on vm") {
				t.Fatalf("activity notifications: %q", acts)
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("no activity notification")
}

func TestActivityMutedPerServer(t *testing.T) {
	old := activityGroupWait
	activityGroupWait = 100 * time.Millisecond
	t.Cleanup(func() { activityGroupWait = old })
	inst := supervisortest.Start(t, supervisortest.Sleeper)
	sender := &fakeSender{}
	f := startRemoteRuntimeWith(t, inst, func(s *settings.Server) { s.Mutes.Processes = true }, sender)
	waitRunning(t, f.svc, "sleeper:sleeper")
	if err := inst.Client().StopProcess(context.Background(), "sleeper:sleeper", true); err != nil {
		t.Fatal(err)
	}
	time.Sleep(12 * time.Second) // past the restart window, so the stop is recorded
	if got := NewIssueService(f.rt).Activity(ActivityQuery{}).Events; len(got) == 0 {
		t.Fatal("a muted change is still recorded")
	}
	for _, m := range sender.messages() {
		if strings.HasPrefix(m.ID, "activity-") {
			t.Fatalf("muted, but notified: %q", m.Title)
		}
	}
}

func TestMainLogFromFileWhenSupervisordIsDown(t *testing.T) {
	for _, remoteTab := range []bool{false, true} {
		t.Run(map[bool]string{false: "local", true: "server"}[remoteTab], func(t *testing.T) {
			inst := supervisortest.Start(t, supervisortest.Sleeper)
			var svc *ServerService
			if remoteTab {
				svc = startRemoteRuntime(t, inst, nil).svc
			} else {
				rt, _ := startRuntime(t, confOf(inst), t.TempDir(), 100*time.Millisecond)
				svc = NewServerService(rt)
			}
			waitRunning(t, svc, "sleeper:sleeper")
			if err := inst.Client().Shutdown(context.Background()); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(15 * time.Second)
			for {
				res, err := svc.MainLogLines(MainLogQuery{Limit: 50})
				if err == nil && res.FromFile != "" {
					if len(res.Lines) == 0 {
						t.Fatal("no lines from the file")
					}
					return
				}
				if time.Now().After(deadline) {
					t.Fatalf("still %+v %v", res.FromFile, err)
				}
				time.Sleep(200 * time.Millisecond)
			}
		})
	}
}
