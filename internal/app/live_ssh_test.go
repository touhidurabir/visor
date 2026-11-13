package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/touhidurabir/visor/internal/remote"
	"github.com/touhidurabir/visor/internal/settings"
)

// TestLiveServerTab opens a real server (VISOR_SSH_TEST, an alias from
// ~/.ssh/config) in a tab as Visor does, with Use sudo when VISOR_SSH_SUDO=1
// (NOPASSWD, or VISOR_SSH_SUDO_PASSWORD). With VISOR_SSH_RESTART=<process>
// it also restarts that process there: use it only on a test machine.
func TestLiveServerTab(t *testing.T) {
	alias := os.Getenv("VISOR_SSH_TEST")
	if alias == "" {
		t.Skip("set VISOR_SSH_TEST=<ssh host alias> to run")
	}
	state := t.TempDir()
	k := &settings.MemoryKeychain{}
	s := settings.Server{ID: settings.NewServerID(), Name: alias, Host: alias, UseSudo: os.Getenv("VISOR_SSH_SUDO") == "1", SudoPassword: settings.SudoOwn}.Normalize()
	if pw := os.Getenv("VISOR_SSH_SUDO_PASSWORD"); pw != "" {
		_ = settings.SetServerSecret(k, s.ID, settings.SecretSudo, pw)
	}
	hk := remote.DefaultHostKeys(state)
	local := NewRuntime(RuntimeOptions{ConfigPath: "/nonexistent/supervisord.conf", StateDir: filepath.Join(state, "local")})
	hub := NewHub(local, HubOptions{StateDir: state, Keychain: k, HostKeys: hk})
	if err := hub.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = hub.Shutdown() }()

	rep, _ := probeServer(context.Background(), s, remote.Secrets{}, os.Getenv("VISOR_SSH_SUDO_PASSWORD"), hk)
	for _, r := range rep.Rows {
		t.Logf("report %-8s %-7s %s", r.ID, r.Status, r.Detail)
	}
	t0 := time.Now()
	if _, err := hub.OpenServer(s); err != nil {
		t.Fatal(err)
	}
	tab, _ := hub.Tab(s.ID)
	var ov OverviewView
	for time.Since(t0) < 20*time.Second {
		ov = tab.RT.Overview()
		if ov.Server.Reachable && ov.Stats.Processes > 0 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !ov.Server.Reachable {
		t.Fatalf("not reachable: %s", ov.Server.Error)
	}
	t.Logf("open in %v via %s as %s: supervisord %s, %d processes in %d configs", time.Since(t0).Round(time.Millisecond), ov.Host.Via, ov.Host.Address, ov.Server.Version, ov.Stats.Processes, len(ov.Configs))
	for _, c := range ov.Configs {
		for _, p := range c.Processes {
			t.Logf("  %s %s pid %d", p.FullName, p.State, p.PID)
		}
	}
	name := os.Getenv("VISOR_SSH_RESTART")
	if name == "" {
		return
	}
	before, _, _ := findProc(ov, name)
	if err := tab.Server.Restart(name); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		after, _, _ := findProc(tab.RT.Overview(), name)
		if after.State == "RUNNING" && after.PID != before.PID && after.PID > 0 {
			t.Logf("restarted %s: pid %d → %d", name, before.PID, after.PID)
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("%s did not restart", name)
}
