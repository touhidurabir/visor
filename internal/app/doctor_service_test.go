package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/touhidurabir/visor/internal/doctor"
	"github.com/touhidurabir/visor/internal/platform"
	"github.com/touhidurabir/visor/internal/settings"
)

// newDoctor builds a DoctorService over a runtime that starts with conf
// saved. supervisord always reads as installed, and the bundle as moved.
func newDoctor(t *testing.T, conf, envPath string, li platform.LoginItem) (*DoctorService, *Runtime, string) {
	t.Helper()
	stateDir := t.TempDir()
	start := settings.Defaults()
	start.ConfigPath = conf
	rt, _ := newRuntimeWith(t, RuntimeOptions{StateDir: stateDir, Settings: start})
	set := NewSettingsService(rt, stateDir, envPath, nil, nil)
	var login *LoginItemService
	if li != nil {
		login = NewLoginItemService(li, nil)
	}
	d := NewDoctorService(rt, set, login)
	d.find = func() (string, string, error) { return "/opt/homebrew/bin/supervisord", "4.2.5", nil }
	d.moved = func() bool { return true }
	d.brewAt = func(string) string { return "" } // never look for this Mac's brew
	return d, rt, stateDir
}

func TestDoctorViewMapsReport(t *testing.T) {
	conf := deadConf(t) // a socket nobody listens on, no [include]
	d, _, _ := newDoctor(t, conf, "", nil)
	v := d.Run()
	if v.Situation != string(doctor.Down) || v.ConfigPath != conf || len(v.Steps) != 5 {
		t.Fatalf("view = %+v", v)
	}
	if v.Steps[0].Status != doctor.OK || v.Steps[2].Status != doctor.Bad || v.Version != "4.2.5" {
		t.Errorf("steps = %+v", v.Steps)
	}
	if v.Onboarded || !v.Notify || !v.Moved || v.Overridden || v.User == "" {
		t.Errorf("view = %+v", v)
	}
	if len(v.Looked) != 1 || v.Looked[0] != conf {
		t.Errorf("Looked = %v, want only the saved path", v.Looked)
	}
}

func TestUseConfigRejectsNonSupervisordFile(t *testing.T) {
	conf := deadConf(t)
	d, rt, stateDir := newDoctor(t, conf, "", nil)
	php := filepath.Join(t.TempDir(), "php.ini")
	if err := os.WriteFile(php, []byte("[PHP]\nmemory_limit=128M\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := d.UseConfig(php); err == nil || !strings.Contains(err.Error(), "not a supervisord config") {
		t.Fatalf("UseConfig(php.ini) = %v, want it refused", err)
	}
	if got := rt.Settings().ConfigPath; got != conf {
		t.Errorf("config path in use = %q, want %q kept", got, conf)
	}
	if saved, _ := settings.Load(stateDir); saved.ConfigPath == php {
		t.Error("php.ini was saved")
	}
}

func TestUseConfigSaves(t *testing.T) {
	a, b := deadConf(t), deadConf(t)
	d, rt, _ := newDoctor(t, a, "", nil)
	v, err := d.UseConfig(" " + b + " ")
	if err != nil {
		t.Fatal(err)
	}
	if rt.Settings().ConfigPath != b || v.ConfigPath != b {
		t.Errorf("in use %q, view %q; want %q", rt.Settings().ConfigPath, v.ConfigPath, b)
	}
}

func TestUseConfigRefusedUnderOverride(t *testing.T) {
	env, other := deadConf(t), deadConf(t)
	d, rt, _ := newDoctor(t, "", env, nil)
	if v := d.Run(); !v.Overridden || v.ConfigPath != env {
		t.Fatalf("view = %+v, want the VISOR_CONFIG path", v)
	}
	if _, err := d.UseConfig(other); err == nil || !strings.Contains(err.Error(), "VISOR_CONFIG") {
		t.Fatalf("UseConfig under VISOR_CONFIG = %v, want it refused", err)
	}
	if rt.Settings().ConfigPath != "" {
		t.Errorf("saved %q although VISOR_CONFIG wins", rt.Settings().ConfigPath)
	}
}

func TestFinishSetsOnboarded(t *testing.T) {
	li := &fakeLogin{st: platform.LoginDisabled}
	d, rt, stateDir := newDoctor(t, deadConf(t), "", li)
	warn, err := d.Finish(true, false)
	if err != nil || warn != "" {
		t.Fatalf("Finish = %q, %v", warn, err)
	}
	if s := rt.Settings(); !s.Onboarded || s.Notify.Enabled {
		t.Errorf("settings in use = %+v", s)
	}
	if saved, _ := settings.Load(stateDir); !saved.Onboarded || saved.Notify.Enabled {
		t.Errorf("settings.json = %+v", saved)
	}
	if len(li.set) != 1 || !li.set[0] {
		t.Errorf("launch at login calls = %v, want [true]", li.set)
	}
	if !d.Run().Onboarded {
		t.Error("view still says not onboarded")
	}
}

func TestFinishLeavesLaunchAtLoginAloneWhenOff(t *testing.T) {
	li := &fakeLogin{st: platform.LoginEnabled}
	d, _, _ := newDoctor(t, deadConf(t), "", li)
	if _, err := d.Finish(false, true); err != nil {
		t.Fatal(err)
	}
	if len(li.set) != 0 {
		t.Errorf("launch at login calls = %v, want none", li.set)
	}
}

func TestFinishWarnsWhenLaunchAtLoginFails(t *testing.T) {
	li := &fakeLogin{st: platform.LoginNotMoved, err: platform.ErrNotMoved}
	d, rt, _ := newDoctor(t, deadConf(t), "", li)
	warn, err := d.Finish(true, true)
	if err != nil {
		t.Fatalf("Finish = %v; setup is saved, so want a warning", err)
	}
	if !strings.Contains(warn, "Settings") || !rt.Settings().Onboarded {
		t.Errorf("warning %q, onboarded %v", warn, rt.Settings().Onboarded)
	}
}

func TestSettingsSaveKeepsOnboarded(t *testing.T) {
	d, rt, stateDir := newDoctor(t, deadConf(t), "", nil)
	stale := d.settings.Get().Settings // a page loaded before setup finished
	if _, err := d.Finish(false, true); err != nil {
		t.Fatal(err)
	}
	if _, err := d.settings.Save(stale); err != nil {
		t.Fatal(err)
	}
	if !rt.Settings().Onboarded {
		t.Error("a Settings save from a stale page undid onboarding")
	}
	if saved, _ := settings.Load(stateDir); !saved.Onboarded {
		t.Error("settings.json lost onboarded")
	}
}

func TestDoctorSaysWhenTheKeychainRefuses(t *testing.T) {
	conf := deadConf(t)
	stateDir := t.TempDir()
	start := settings.Defaults()
	start.ConfigPath, start.Username = conf, "me"
	kc := &countingKeychain{fail: errors.New("user canceled")}
	rt, _ := newRuntimeWith(t, RuntimeOptions{StateDir: stateDir, Settings: start})
	d := NewDoctorService(rt, NewSettingsService(rt, stateDir, "", kc, nil), nil)
	d.find = func() (string, string, error) { return "/opt/homebrew/bin/supervisord", "4.2.5", nil }
	d.brewAt = func(string) string { return "" }
	v := d.Run()
	if v.Situation != string(doctor.Auth) || !strings.Contains(v.Steps[2].Detail, secretStoreWord()) {
		t.Fatalf("situation %s, detail %q", v.Situation, v.Steps[2].Detail)
	}
}

func TestDoctorReportsAMissingConfigFolder(t *testing.T) {
	dir := shortDir(t)
	conf := filepath.Join(dir, "supervisord.conf")
	body := "[unix_http_server]\nfile=" + dir + "/dead.sock\n\n[include]\nfiles=" + dir + "/conf.d/*.ini\n"
	if err := os.WriteFile(conf, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	d, _, _ := newDoctor(t, conf, "", nil)
	v := d.Run()
	if !v.IncludeMissing || v.IncludeDir != filepath.Join(dir, "conf.d") {
		t.Fatalf("view = %+v", v)
	}
}

// downConf is a config whose socket nobody listens on, with a log file.
func downConf(t *testing.T) (conf, logFile string) {
	t.Helper()
	dir := shortDir(t)
	conf = filepath.Join(dir, "supervisord.conf")
	logFile = filepath.Join(dir, "supervisord.log")
	body := "[unix_http_server]\nfile=" + dir + "/dead.sock\n\n[supervisord]\nlogfile=" + logFile + "\n\n[include]\nfiles=" + dir + "/*.ini\n"
	if err := os.WriteFile(conf, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return conf, logFile
}

func TestDoctorOffersToStartAStoppedSupervisord(t *testing.T) {
	conf, _ := downConf(t)
	d, _, _ := newDoctor(t, conf, "", nil)
	if v := d.Run(); v.Situation != string(doctor.Down) || v.StartCommand != "supervisord -c "+conf || !v.CanStart {
		t.Fatalf("view = %+v", v)
	}
}

func TestStartSupervisordSaysWhyItDidNotStart(t *testing.T) {
	conf, logFile := downConf(t)
	if err := os.WriteFile(logFile, []byte("INFO an old run, days ago\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	d, _, _ := newDoctor(t, conf, "", nil)
	d.startWait = 0
	var ran doctor.StartPlan
	d.start = func(_ context.Context, p doctor.StartPlan, _ []string) (string, error) {
		ran = p
		f, _ := os.OpenFile(logFile, os.O_APPEND|os.O_WRONLY, 0)
		_, _ = f.WriteString("CRIT another program is already listening on a port\n")
		_ = f.Close()
		return "Error: .ini file does not include supervisord section", errors.New("exit status 2")
	}
	v, err := d.StartSupervisord()
	if err != nil {
		t.Fatal(err)
	}
	if ran.Command != "supervisord -c "+conf {
		t.Errorf("ran %+v", ran)
	}
	f := v.StartFailure
	if f == nil || !strings.Contains(f.Output, "does not include supervisord section") || f.Error != "exit status 2" {
		t.Fatalf("failure = %+v", f)
	}
	if f.LogPath != logFile || !strings.Contains(f.Log, "already listening") || strings.Contains(f.Log, "old run") {
		t.Errorf("log = %q from %q, want only this start's lines", f.Log, f.LogPath)
	}
}

func TestStartSupervisordOnlyWhenStopped(t *testing.T) {
	d, _, _ := newDoctor(t, deadConf(t), "", nil) // no [include]: Down, but also no log
	d.start = func(context.Context, doctor.StartPlan, []string) (string, error) {
		t.Fatal("started although the situation has no start command")
		return "", nil
	}
	dir := shortDir(t)
	conf := filepath.Join(dir, "supervisord.conf")
	if err := os.WriteFile(conf, []byte("[supervisord]\n"), 0o600); err != nil { // no socket: nothing to connect to after a start
		t.Fatal(err)
	}
	if _, err := d.UseConfig(conf); err != nil {
		t.Fatal(err)
	}
	if v, err := d.StartSupervisord(); err != nil || v.StartCommand != "" || v.StartFailure != nil {
		t.Errorf("StartSupervisord = %+v, %v", v, err)
	}
}

func TestStartSupervisordSaysWhenItNeverAnswered(t *testing.T) {
	conf, _ := downConf(t)
	d, _, _ := newDoctor(t, conf, "", nil)
	d.startWait = 0
	d.start = func(context.Context, doctor.StartPlan, []string) (string, error) {
		return "==> Successfully started `supervisor`", nil
	}
	v, _ := d.StartSupervisord()
	if v.StartFailure == nil || !strings.Contains(v.StartFailure.Error, "didn't answer") {
		t.Fatalf("failure = %+v", v.StartFailure)
	}
}

func TestForegroundConfigIsNotStartedByVisor(t *testing.T) {
	dir := shortDir(t)
	conf := filepath.Join(dir, "supervisord.conf")
	body := "[unix_http_server]\nfile=" + dir + "/dead.sock\n\n[supervisord]\nnodaemon=true\n\n[include]\nfiles=" + dir + "/*.ini\n"
	if err := os.WriteFile(conf, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	d, _, _ := newDoctor(t, conf, "", nil)
	d.start = func(context.Context, doctor.StartPlan, []string) (string, error) {
		t.Fatal("started a foreground supervisord")
		return "", nil
	}
	v := d.Run()
	if v.CanStart || !v.Foreground || v.StartCommand != "supervisord -c "+conf {
		t.Fatalf("view = %+v", v)
	}
	if v, _ := d.StartSupervisord(); v.StartFailure != nil {
		t.Errorf("failure = %+v", v.StartFailure)
	}
}
