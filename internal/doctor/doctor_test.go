package doctor

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/touhidurabir/visor/internal/config"
	"github.com/touhidurabir/visor/internal/supervisor"
)

var looked = []string{"/opt/homebrew/etc/supervisord.conf", "/usr/local/etc/supervisord.conf"}

// healthy is a probe where every check passes.
func healthy() Probe {
	d := config.Discovery{
		ConfigPath:   "/opt/homebrew/etc/supervisord.conf",
		Transport:    supervisor.UnixTransport{Path: "/opt/homebrew/var/run/supervisor.sock"},
		IncludeGlobs: []string{"/opt/homebrew/etc/supervisor.d/*.ini"},
	}
	return Probe{
		FindSupervisord: func() (string, string, error) { return "/opt/homebrew/bin/supervisord", "4.2.5", nil },
		Resolve:         func() (config.Discovery, []string, error) { return d, looked, nil },
		Ping:            func(config.Discovery) (int, error) { return 11, nil },
		Writable:        func(string) bool { return true },
		Inventory:       func(config.Discovery) (int, error) { return 4, nil },
	}
}

func statuses(r Report) string {
	var b strings.Builder
	for _, s := range r.Steps {
		b.WriteString(string(s.Status)[:1])
	}
	return b.String()
}

func TestRunReady(t *testing.T) {
	r := Run(healthy())
	if r.Situation != Ready || statuses(r) != "ooooo" {
		t.Fatalf("situation %s, steps %s", r.Situation, statuses(r))
	}
	want := []Step{
		{ID: "installed", Title: "supervisord installed", Detail: "/opt/homebrew/bin/supervisord, version 4.2.5", Status: OK},
		{ID: "config", Title: "Config file found", Detail: "/opt/homebrew/etc/supervisord.conf", Status: OK},
		{ID: "running", Title: "supervisord running", Detail: "Connected through supervisor.sock", Status: OK},
		{ID: "writable", Title: "Config folder writable", Detail: "/opt/homebrew/etc/supervisor.d", Status: OK},
		{ID: "processes", Title: "11 processes in 4 configs", Status: OK},
	}
	for i, s := range want {
		if r.Steps[i] != s {
			t.Errorf("step %d = %+v, want %+v", i, r.Steps[i], s)
		}
	}
	if r.Version != "4.2.5" || r.Processes != 11 || r.Configs != 4 || r.ConfigPath != "/opt/homebrew/etc/supervisord.conf" || !r.Moved {
		t.Errorf("report = %+v", r)
	}
}

func TestRunNoSup(t *testing.T) {
	p := healthy()
	p.FindSupervisord = func() (string, string, error) { return "", "", errors.New("not found") }
	p.Ping = func(config.Discovery) (int, error) { return 0, supervisor.ErrUnreachable }
	r := Run(p)
	if r.Situation != NoSup || statuses(r) != "bwwww" {
		t.Fatalf("situation %s, steps %s", r.Situation, statuses(r))
	}
	if r.Steps[0].Detail != "Not on your PATH or in Homebrew" || r.Steps[1].Detail != waitDetail {
		t.Errorf("details = %q, %q", r.Steps[0].Detail, r.Steps[1].Detail)
	}
}

// supervisord installed somewhere the login PATH doesn't reach (a virtualenv)
// but running: the server answering proves it's installed.
func TestRunNotOnPathButRunning(t *testing.T) {
	p := healthy()
	p.FindSupervisord = func() (string, string, error) { return "", "", errors.New("not found") }
	r := Run(p)
	if r.Situation != Ready || statuses(r) != "ooooo" {
		t.Fatalf("situation %s, steps %s", r.Situation, statuses(r))
	}
	if r.Steps[0].Detail != "Not on your PATH, but it's running" {
		t.Errorf("detail %q", r.Steps[0].Detail)
	}
}

func TestRunNoSupWithNoConfig(t *testing.T) {
	p := healthy()
	p.FindSupervisord = func() (string, string, error) { return "", "", errors.New("not found") }
	p.Resolve = func() (config.Discovery, []string, error) { return config.Discovery{}, looked, config.ErrNoConfig }
	if r := Run(p); r.Situation != NoSup || statuses(r) != "bwwww" {
		t.Fatalf("situation %s, steps %s", r.Situation, statuses(r))
	}
}

func TestRunNoConf(t *testing.T) {
	p := healthy()
	p.Resolve = func() (config.Discovery, []string, error) {
		return config.Discovery{}, looked, fmt.Errorf("%w (looked in …)", config.ErrNoConfig)
	}
	r := Run(p)
	if r.Situation != NoConf || statuses(r) != "obwww" {
		t.Fatalf("situation %s, steps %s", r.Situation, statuses(r))
	}
	if r.Steps[1].Detail != "Not in any of the usual places" || len(r.Looked) != 2 {
		t.Errorf("detail %q, looked %v", r.Steps[1].Detail, r.Looked)
	}
}

func TestRunNoConfSaysWhyAChosenFileFails(t *testing.T) {
	p := healthy()
	p.Override = func() (string, bool) { return "/x/super.conf", true }
	p.Resolve = func() (config.Discovery, []string, error) {
		return config.Discovery{}, []string{"/x/super.conf"}, errors.New("/x/super.conf:3: bad line")
	}
	r := Run(p)
	if r.Situation != NoConf || r.Steps[1].Detail != "/x/super.conf:3: bad line" {
		t.Fatalf("situation %s, detail %q", r.Situation, r.Steps[1].Detail)
	}
}

func TestRunDown(t *testing.T) {
	p := healthy()
	p.Ping = func(config.Discovery) (int, error) {
		return 0, fmt.Errorf("%w: %w", supervisor.ErrUnreachable, &net.OpError{Op: "dial", Err: os.NewSyscallError("connect", errors.New("connection refused"))})
	}
	r := Run(p)
	if r.Situation != Down || statuses(r) != "oobow" {
		t.Fatalf("situation %s, steps %s", r.Situation, statuses(r))
	}
	if r.Steps[2].Detail != "Nothing is listening on supervisor.sock" {
		t.Errorf("detail %q", r.Steps[2].Detail)
	}
}

func TestRunDeniedIsNotDown(t *testing.T) {
	p := healthy()
	p.Ping = func(config.Discovery) (int, error) {
		return 0, fmt.Errorf("%w: %w", supervisor.ErrUnreachable, &net.OpError{Op: "dial", Err: os.NewSyscallError("connect", os.ErrPermission)})
	}
	r := Run(p)
	if r.Situation != Denied || statuses(r) != "oobow" {
		t.Fatalf("situation %s, steps %s", r.Situation, statuses(r))
	}
	if r.Steps[2].Detail != "supervisor.sock isn't open to your user (it's usually owned by root)" {
		t.Errorf("detail %q", r.Steps[2].Detail)
	}
}

func TestRunWrongPasswordIsItsOwnSituation(t *testing.T) {
	p := healthy()
	p.Ping = func(config.Discovery) (int, error) { return 0, supervisor.ErrUnauthorized }
	r := Run(p)
	if r.Situation != Auth || statuses(r) != "oobow" {
		t.Fatalf("situation %s, steps %s", r.Situation, statuses(r))
	}
	if r.Steps[2].Detail != "supervisord turned down the username and password" {
		t.Errorf("detail %q", r.Steps[2].Detail)
	}
}

func TestRunReportsTheConfigFolder(t *testing.T) {
	p := healthy()
	p.Writable = func(string) bool { return false }
	if r := Run(p); r.IncludeDir != "/opt/homebrew/etc/supervisor.d" {
		t.Errorf("IncludeDir = %q", r.IncludeDir)
	}
}

func TestRunUnreadablePasswordIsAuthWithTheReason(t *testing.T) {
	p := healthy()
	p.Ping = func(config.Discovery) (int, error) {
		return 0, fmt.Errorf("%w: the Keychain refused (user canceled)", ErrCredentials)
	}
	r := Run(p)
	if r.Situation != Auth || !strings.Contains(r.Steps[2].Detail, "Keychain refused") {
		t.Fatalf("situation %s, detail %q", r.Situation, r.Steps[2].Detail)
	}
}

func TestRunPingsOnceWhenNotOnPath(t *testing.T) {
	p := healthy()
	p.FindSupervisord = func() (string, string, error) { return "", "", errors.New("not found") }
	pings := 0
	p.Ping = func(config.Discovery) (int, error) { pings++; return 3, nil }
	if r := Run(p); r.Situation != Ready || r.Processes != 3 {
		t.Fatalf("report = %+v", r)
	}
	if pings != 1 {
		t.Errorf("pinged %d times, want 1", pings)
	}
}

func TestRunMissingConfigFolder(t *testing.T) {
	p := healthy()
	p.Writable = func(string) bool { return false }
	p.Exists = func(string) bool { return false }
	r := Run(p)
	if r.Situation != NotWritable || !r.IncludeMissing {
		t.Fatalf("situation %s, missing %v", r.Situation, r.IncludeMissing)
	}
	if r.Steps[3].Detail != "/opt/homebrew/etc/supervisor.d doesn't exist yet" {
		t.Errorf("detail %q", r.Steps[3].Detail)
	}
}

func TestRunNoSocketInConfig(t *testing.T) {
	p := healthy()
	d, _, _ := p.Resolve()
	d.Transport = nil
	p.Resolve = func() (config.Discovery, []string, error) { return d, looked, nil }
	p.Ping = func(config.Discovery) (int, error) { t.Fatal("pinged with no transport"); return 0, nil }
	r := Run(p)
	if r.Situation != Down || r.Steps[2].Detail != "supervisord.conf has no [unix_http_server] or [inet_http_server]" {
		t.Fatalf("situation %s, detail %q", r.Situation, r.Steps[2].Detail)
	}
}

func TestRunNotWritable(t *testing.T) {
	p := healthy()
	p.Writable = func(string) bool { return false }
	r := Run(p)
	if r.Situation != NotWritable || statuses(r) != "ooobo" {
		t.Fatalf("situation %s, steps %s", r.Situation, statuses(r))
	}
	if r.Steps[3].Detail != "/opt/homebrew/etc/supervisor.d isn't writable for you" {
		t.Errorf("detail %q", r.Steps[3].Detail)
	}
}

func TestRunNoInclude(t *testing.T) {
	p := healthy()
	d, _, _ := p.Resolve()
	d.IncludeGlobs = nil
	p.Resolve = func() (config.Discovery, []string, error) { return d, looked, nil }
	r := Run(p)
	if r.Situation != NotWritable || r.Steps[3].Detail != "supervisord.conf has no [include] files=" {
		t.Fatalf("situation %s, detail %q", r.Situation, r.Steps[3].Detail)
	}
}

func TestDependentsWait(t *testing.T) {
	p := healthy()
	p.Ping = func(config.Discovery) (int, error) { return 0, supervisor.ErrUnreachable }
	p.Writable = func(string) bool { return false }
	r := Run(p)
	// Step 4 doesn't depend on step 3; step 5 does. The first failure
	// decides the situation.
	if statuses(r) != "oobbw" || r.Situation != Down {
		t.Fatalf("situation %s, steps %s", r.Situation, statuses(r))
	}
	if r.Steps[4].Detail != waitDetail || r.Steps[4].Title != "Processes found" {
		t.Errorf("step 5 = %+v", r.Steps[4])
	}
}

func TestOverrideReported(t *testing.T) {
	p := healthy()
	p.Override = func() (string, bool) { return "/tmp/visor-sandbox/supervisord.conf", true }
	r := Run(p)
	if !r.Overridden || r.ConfigPath != "/opt/homebrew/etc/supervisord.conf" {
		t.Fatalf("report = %+v", r)
	}
}

func TestOverrideShownEvenWhenMissing(t *testing.T) {
	p := healthy()
	p.Override = func() (string, bool) { return "/tmp/visor-x/missing.conf", true }
	p.Resolve = func() (config.Discovery, []string, error) {
		return config.Discovery{}, []string{"/tmp/visor-x/missing.conf"}, config.ErrNoConfig
	}
	r := Run(p)
	if r.Situation != NoConf || !r.Overridden || r.ConfigPath != "/tmp/visor-x/missing.conf" {
		t.Fatalf("report = %+v", r)
	}
}

func TestNotMovedReported(t *testing.T) {
	p := healthy()
	p.BundleMoved = func() bool { return false }
	if r := Run(p); r.Moved || r.Situation != Ready {
		t.Fatalf("report = %+v", r)
	}
}

func TestFindUsesLoginPATH(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "supervisord")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho 4.2.5\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	find := FindIn(func() []string { return []string{"/nonexistent", dir} })
	path, version, err := find()
	if err != nil || path != bin || version != "4.2.5" {
		t.Fatalf("find = %q, %q, %v", path, version, err)
	}
	if _, _, err := FindIn(func() []string { return []string{"/nonexistent"} })(); err == nil {
		t.Fatal("found supervisord on an empty PATH")
	}
}
