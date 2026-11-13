package app

import (
	"context"
	"os"
	"os/exec"
	"strings"
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

// TestLiveActivity changes things on a test server behind Visor's back and
// checks each shows as activity and notifies: VISOR_SSH_ACTIVITY=1 with
// VISOR_SSH_TEST and VISOR_SSH_SUDO=1 (NOPASSWD). It restarts sleeper:sleeper,
// appends a comment to /etc/supervisor/conf.d/sleeper.conf (and takes it
// out again) and restarts supervisor. Only on a test machine.
func TestLiveActivity(t *testing.T) {
	alias := os.Getenv("VISOR_SSH_TEST")
	if alias == "" || os.Getenv("VISOR_SSH_ACTIVITY") != "1" {
		t.Skip("set VISOR_SSH_TEST and VISOR_SSH_ACTIVITY=1 to run")
	}
	oldWait := activityGroupWait
	activityGroupWait = time.Second
	t.Cleanup(func() { activityGroupWait = oldWait })
	state := t.TempDir()
	k := &settings.MemoryKeychain{}
	s := settings.Server{ID: settings.NewServerID(), Name: alias, Host: alias, UseSudo: true, SudoPassword: settings.SudoNone}.Normalize()
	sender := &fakeSender{}
	rt := NewRuntime(RuntimeOptions{
		StateDir: settings.ServerStateDir(state, s.ID), Interval: time.Second, Keychain: k, Notifier: sender,
		Remote: &RemoteSource{Server: s, Connect: ConnectServer(k, remote.DefaultHostKeys(state))},
	})
	if err := rt.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer rt.Stop()
	svc := NewServerService(rt)
	waitRunning(t, svc, "sleeper:sleeper")
	run := func(cmd string) {
		t.Helper()
		if out, err := exec.Command("ssh", "-o", "BatchMode=yes", alias, cmd).CombinedOutput(); err != nil {
			t.Fatalf("%s: %v %s", cmd, err, out)
		}
	}
	waitKind := func(kind string) ActivityEventView {
		t.Helper()
		deadline := time.Now().Add(40 * time.Second)
		for time.Now().Before(deadline) {
			for _, e := range NewIssueService(rt).Activity(ActivityQuery{}).Events {
				if e.Kind == kind {
					return e
				}
			}
			time.Sleep(300 * time.Millisecond)
		}
		t.Fatalf("no %s event", kind)
		return ActivityEventView{}
	}
	// Files are checked every 10 s on a server: let the first check see
	// things as they are, so later changes are changes.
	time.Sleep(remoteFileCheck + 2*time.Second)
	run("sudo -n systemctl restart supervisor")
	e := waitKind("daemon_restarted")
	t.Logf("event: %s", e.Text)

	waitRunning(t, svc, "sleeper:sleeper")
	run("sudo -n supervisorctl restart sleeper")
	e = waitKind("process_restarted")
	t.Logf("event: %s (by Visor %v)", e.Text, e.ByVisor)

	time.Sleep(remoteFileCheck + 2*time.Second)
	conf := "/etc/supervisor/conf.d/sleeper.conf"
	run("echo '# visor live test' | sudo -n tee -a " + conf + " >/dev/null")
	t.Cleanup(func() { run("sudo -n sed -i '/^# visor live test$/d' " + conf) })
	e = waitKind("file_changed")
	t.Logf("event: %s", e.Text)

	time.Sleep(2 * time.Second)
	for _, m := range sender.messages() {
		if strings.HasPrefix(m.ID, "activity-") {
			t.Logf("notified: %s · %s", m.Title, m.Body)
		}
	}
}
