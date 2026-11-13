package app

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/touhidurabir/visor/internal/doctor"
	"github.com/touhidurabir/visor/internal/remote"
	"github.com/touhidurabir/visor/internal/remote/remotetest"
	"github.com/touhidurabir/visor/internal/settings"
	"github.com/touhidurabir/visor/internal/supervisor/supervisortest"
)

type remoteSvcFixture struct {
	svc   *RemoteService
	hub   *Hub
	ssh   *remotetest.Server
	state string
	k     *settings.MemoryKeychain
}

func newRemoteSvc(t *testing.T) remoteSvcFixture {
	t.Helper()
	noSSHConfig(t)
	state := t.TempDir()
	ssh := remotetest.Start(t, remotetest.Options{Password: "pw"})
	k := &settings.MemoryKeychain{}
	hk := remote.HostKeys{VisorFile: filepath.Join(state, "known_hosts")}
	local := NewRuntime(RuntimeOptions{ConfigPath: "/nonexistent/supervisord.conf", StateDir: state})
	prefs := settings.Defaults()
	hub := NewHub(local, HubOptions{StateDir: state, Keychain: k, HostKeys: hk, Settings: func() settings.Settings { return prefs }})
	if err := hub.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = hub.Shutdown() })
	return remoteSvcFixture{svc: NewRemoteService(hub, state, k, hk, func() settings.Settings { return prefs }), hub: hub, ssh: ssh, state: state, k: k}
}

func rowStatus(rows []doctor.ReportRow, id string) string {
	for _, r := range rows {
		if r.ID == id {
			return r.Status
		}
	}
	return "missing"
}

func TestRemoteServiceTestTrustSaveOpen(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Sleeper)
	f := newRemoteSvc(t)
	in := ServerInput{
		Server:   settings.Server{Name: "vm", Host: f.ssh.Host, Port: f.ssh.Port, User: "visor", ConfigPath: inst.ConfigPath()},
		Password: SecretInput{Value: "pw"}, KeyPassphrase: SecretInput{Keep: true}, SudoPassword: SecretInput{Keep: true},
	}
	rep, err := f.svc.Test(in)
	if err != nil {
		t.Fatal(err)
	}
	if rep.HostKey == nil || rep.HostKey.Status != "unknown" || !rep.Blocked {
		t.Fatalf("first test should ask about the host key: %+v", rep)
	}
	if err := f.svc.Trust(rep.HostKey.Host, rep.HostKey.Fingerprint); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Trust(rep.HostKey.Host, rep.HostKey.Fingerprint); err == nil {
		t.Fatal("a key can be trusted once per prompt")
	}
	rep, err = f.svc.Test(in)
	if err != nil || rep.Blocked || rep.HostKey != nil {
		t.Fatalf("second test: %+v %v", rep, err)
	}
	for _, id := range []string{"ssh", "config", "socket", "read", "version"} {
		if st := rowStatus(rep.Rows, id); st != doctor.RowOK {
			t.Errorf("row %s: %s (%+v)", id, st, rep.Rows)
		}
	}

	sv, err := f.svc.Save(in)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(f.state, "servers.json"))
	if len(raw) == 0 || strings.Contains(string(raw), `"pw"`) {
		t.Fatalf("servers.json: %s", raw)
	}
	list, err := f.svc.List()
	if err != nil || len(list) != 1 || !list[0].HasPassword || list[0].Open {
		t.Fatalf("list: %+v %v", list, err)
	}
	// A second server with the same name is refused on the name field.
	dup := in
	dup.Server.ID = ""
	var fe *settings.FieldError
	if _, err := f.svc.Save(dup); !errors.As(err, &fe) || fe.Field != "name" {
		t.Fatalf("duplicate: %v", err)
	}

	ov, err := f.svc.Open(sv.ID)
	if err != nil || ov.Overview == nil || ov.HostKey != nil || ov.Report != nil {
		t.Fatalf("open: %+v %v", ov, err)
	}
	if tabs, _ := f.svc.Tabs(); len(tabs) != 1 || tabs[0].ID != sv.ID {
		t.Fatalf("tabs: %+v", tabs)
	}
	if err := f.svc.Close(sv.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Delete(sv.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := f.svc.List(); len(list) != 0 {
		t.Fatalf("after delete: %+v", list)
	}
	if v, _ := settings.ServerSecret(f.k, sv.ID, settings.SecretPassword); v != "" {
		t.Fatal("secret left behind")
	}
}

func TestRemoteServiceReportGaps(t *testing.T) {
	f := newRemoteSvc(t)
	if err := f.svc.hk.Trust(f.ssh.Host, f.ssh.Port, f.ssh.HostKey); err != nil {
		t.Fatal(err)
	}
	base := settings.Server{Name: "vm", Host: f.ssh.Host, Port: f.ssh.Port, User: "visor"}
	// Wrong password: the SSH row fails, the rest are skipped.
	rep, _ := f.svc.Test(ServerInput{Server: base, Password: SecretInput{Value: "bad"}})
	if rowStatus(rep.Rows, "ssh") != doctor.RowError || rowStatus(rep.Rows, "socket") != doctor.RowSkipped || !rep.Blocked {
		t.Fatalf("bad password: %+v", rep.Rows)
	}
	// No config at the path.
	base.ConfigPath = "/nonexistent/supervisord.conf"
	rep, _ = f.svc.Test(ServerInput{Server: base, Password: SecretInput{Value: "pw"}})
	if rowStatus(rep.Rows, "config") != doctor.RowError || !rep.Blocked {
		t.Fatalf("no config: %+v", rep.Rows)
	}
}

func TestRemoteServiceOpenAsksAboutUnknownKey(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Sleeper)
	f := newRemoteSvc(t)
	sv, err := f.svc.Save(ServerInput{
		Server:   settings.Server{Name: "vm", Host: f.ssh.Host, Port: f.ssh.Port, User: "visor", ConfigPath: inst.ConfigPath()},
		Password: SecretInput{Value: "pw"},
	})
	if err != nil {
		t.Fatal(err)
	}
	ov, err := f.svc.Open(sv.ID)
	if err != nil || ov.HostKey == nil || ov.Overview != nil {
		t.Fatalf("open: %+v %v", ov, err)
	}
	if len(f.hub.Open()) != 0 {
		t.Fatal("a tab stayed open for an unknown host")
	}
	if err := f.svc.Trust(ov.HostKey.Host, ov.HostKey.Fingerprint); err != nil {
		t.Fatal(err)
	}
	ov, err = f.svc.Open(sv.ID)
	if err != nil || ov.Overview == nil || ov.Report != nil {
		t.Fatalf("open after trust: %+v %v", ov, err)
	}
}

// A refused sudo password on Open is reported from the failed connection,
// not by connecting again (each try counts against the lockout).
func TestOpenDoesNotRetryARefusedSudoPassword(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not installed")
	}
	inst := supervisortest.Start(t, supervisortest.Sleeper)
	noSSHConfig(t)
	dir := t.TempDir()
	log := filepath.Join(dir, "sudo.log")
	script := "#!/bin/sh\necho run >> \"" + log + "\"\nprintf '[visor-sudo]' >&2\nread -r pw\necho 'Sorry, try again.' >&2\nprintf '[visor-sudo]' >&2\nread -r pw\nexit 1\n"
	if err := os.WriteFile(filepath.Join(dir, "sudo"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	state := t.TempDir()
	ssh := remotetest.Start(t, remotetest.Options{Password: "pw", Env: []string{"PATH=" + dir + ":" + os.Getenv("PATH")}})
	k := &settings.MemoryKeychain{}
	hk := remote.HostKeys{VisorFile: filepath.Join(state, "known_hosts")}
	if err := hk.Trust(ssh.Host, ssh.Port, ssh.HostKey); err != nil {
		t.Fatal(err)
	}
	local := NewRuntime(RuntimeOptions{ConfigPath: "/nonexistent/supervisord.conf", StateDir: state})
	hub := NewHub(local, HubOptions{StateDir: state, Keychain: k, HostKeys: hk})
	if err := hub.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = hub.Shutdown() })
	svc := NewRemoteService(hub, state, k, hk, settings.Defaults)
	sv, err := svc.Save(ServerInput{
		Server:   settings.Server{Name: "vm", Host: ssh.Host, Port: ssh.Port, User: "visor", UseSudo: true, ConfigPath: inst.ConfigPath()},
		Password: SecretInput{Value: "pw"},
	})
	if err != nil {
		t.Fatal(err)
	}
	ov, err := svc.Open(sv.ID)
	if err != nil || ov.Report == nil || !ov.Report.Credentials || rowStatus(ov.Report.Rows, "sudo") != doctor.RowError {
		t.Fatalf("open: %+v %v", ov.Report, err)
	}
	b, _ := os.ReadFile(log)
	if n := strings.Count(string(b), "run"); n != 1 {
		t.Fatalf("sudo ran %d times, want 1", n)
	}
}
