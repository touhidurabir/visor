package app

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/touhidurabir/visor/internal/remote"
	"github.com/touhidurabir/visor/internal/remote/remotetest"
	"github.com/touhidurabir/visor/internal/settings"
	"github.com/touhidurabir/visor/internal/supervisor/supervisortest"
)

func TestHubTabsReachTheirOwnSupervisord(t *testing.T) {
	noSSHConfig(t)
	a := supervisortest.Start(t, supervisortest.Sleeper)
	b := supervisortest.Start(t, supervisortest.Sleeper)
	state := t.TempDir()
	ssh := remotetest.Start(t, remotetest.Options{Password: "pw"})
	hk := remote.HostKeys{VisorFile: filepath.Join(state, "known_hosts")}
	if err := hk.Trust(ssh.Host, ssh.Port, ssh.HostKey); err != nil {
		t.Fatal(err)
	}
	k := &settings.MemoryKeychain{}
	server := func(name string, inst *supervisortest.Instance) settings.Server {
		s := settings.Server{ID: settings.NewServerID(), Name: name, Host: ssh.Host, Port: ssh.Port, User: "visor", ConfigPath: inst.ConfigPath()}.Normalize()
		if err := settings.SetServerSecret(k, s.ID, settings.SecretPassword, "pw"); err != nil {
			t.Fatal(err)
		}
		return s
	}
	sa, sb := server("a", a), server("b", b)

	prefs := settings.Defaults()
	prefs.MaxServers = 2
	prefs.PollInterval = time.Second
	local := NewRuntime(RuntimeOptions{ConfigPath: "/nonexistent/supervisord.conf", StateDir: state})
	var mu sync.Mutex
	var events []string
	hub := NewHub(local, HubOptions{
		StateDir: state, Keychain: k, HostKeys: hk,
		Settings: func() settings.Settings { return prefs },
		Emit: func(name string, data any) {
			if ov, ok := data.(OverviewView); ok && name == EventOverview {
				mu.Lock()
				events = append(events, ov.Host.ID)
				mu.Unlock()
			}
		},
	})
	if err := hub.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = hub.ServiceShutdown() })

	api := NewServerAPI(hub)
	for _, s := range []settings.Server{sa, sb} {
		if _, err := hub.OpenServer(s); err != nil {
			t.Fatal(err)
		}
	}
	if got := hub.Open(); len(got) != 2 || got[0] != sa.ID || got[1] != sb.ID {
		t.Fatalf("open: %v", got)
	}
	// A third is over the limit of 2.
	sc := sa
	sc.ID = settings.NewServerID()
	if _, err := hub.OpenServer(sc); !errors.Is(err, ErrTooManyServers) {
		t.Fatalf("limit: %v", err)
	}
	// Opening an open server again returns its tab.
	if _, err := hub.OpenServer(sa); err != nil {
		t.Fatal(err)
	}

	ta, _ := hub.Tab(sa.ID)
	waitRunning(t, ta.Server, "sleeper:sleeper")
	tb, _ := hub.Tab(sb.ID)
	waitRunning(t, tb.Server, "sleeper:sleeper")
	if err := api.Stop(sa.ID, "sleeper:sleeper"); err != nil {
		t.Fatal(err)
	}
	pa, _ := a.Client().GetProcessInfo(context.Background(), "sleeper:sleeper")
	pb, _ := b.Client().GetProcessInfo(context.Background(), "sleeper:sleeper")
	if pa.StateName != "STOPPED" || pb.StateName != "RUNNING" {
		t.Fatalf("the stop went to the wrong supervisord: a=%s b=%s", pa.StateName, pb.StateName)
	}
	ov, err := api.Overview(sb.ID)
	if err != nil || ov.Host.ID != sb.ID || ov.Host.Name != "b" {
		t.Fatalf("overview b: %+v %v", ov.Host, err)
	}
	if lov, err := api.Overview(LocalID); err != nil || lov.Host.Remote {
		t.Fatalf("local overview: %+v %v", lov.Host, err)
	}

	if err := hub.CloseServer(sa.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := api.Overview(sa.ID); !errors.Is(err, ErrNoTab) {
		t.Fatalf("closed tab: %v", err)
	}
	if err := hub.CloseServer(LocalID); err == nil {
		t.Fatal("the local tab closed")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(events) == 0 {
		t.Fatal("no overview events")
	}
}
