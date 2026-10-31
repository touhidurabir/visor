package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/touhidurabir/visor/internal/settings"
	"github.com/touhidurabir/visor/internal/supervisor"
	"github.com/touhidurabir/visor/internal/supervisor/supervisortest"
)

// shortDir is a /tmp folder short enough for a unix socket path.
func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "visor-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// authServer is a minimal supervisord on a unix socket that answers getState
// and getAllProcessInfo only with HTTP basic auth user:pass. It returns a
// config pointing at it whose [supervisorctl] credentials are wrong.
func authServer(t *testing.T, user, pass string) string {
	t.Helper()
	dir := shortDir(t)
	sock := filepath.Join(dir, "s.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != user || p != pass {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		value := `<array><data></data></array>`
		if strings.Contains(string(body), "supervisor.getState") {
			value = `<struct><member><name>statecode</name><value><int>1</int></value></member>` +
				`<member><name>statename</name><value><string>RUNNING</string></value></member></struct>`
		}
		w.Header().Set("Content-Type", "text/xml")
		_, _ = fmt.Fprintf(w, `<?xml version='1.0'?><methodResponse><params><param><value>%s</value></param></params></methodResponse>`, value)
	})}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	conf := filepath.Join(dir, "supervisord.conf")
	body := "[unix_http_server]\nfile=" + sock + "\n\n[supervisorctl]\nusername=cfg\npassword=wrong\n"
	if err := os.WriteFile(conf, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return conf
}

// deadConf is a config whose socket nobody listens on.
func deadConf(t *testing.T) string {
	t.Helper()
	dir := shortDir(t)
	conf := filepath.Join(dir, "supervisord.conf")
	if err := os.WriteFile(conf, []byte("[unix_http_server]\nfile="+dir+"/dead.sock\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return conf
}

func newRuntimeWith(t *testing.T, o RuntimeOptions) (*Runtime, *recorder) {
	t.Helper()
	rec := &recorder{}
	o.Emit = rec.emit
	if o.StateDir == "" {
		o.StateDir = t.TempDir()
	}
	rt := NewRuntime(o)
	if err := rt.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rt.Stop)
	return rt, rec
}

// settleGoroutines waits for the goroutine count to drop to at most want.
func settleGoroutines(t *testing.T, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		n := runtime.NumGoroutine()
		if n <= want {
			return
		}
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<20)
			t.Fatalf("%d goroutines, want at most %d:\n%s", n, want, buf[:runtime.Stack(buf, true)])
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestReconnectRebuildsWithoutLeaks(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Sleeper)
	time.Sleep(100 * time.Millisecond)
	baseline := runtime.NumGoroutine()

	rec := &recorder{}
	rt := NewRuntime(RuntimeOptions{ConfigPath: confOf(inst), StateDir: t.TempDir(), Interval: 50 * time.Millisecond, Emit: rec.emit})
	if err := rt.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	svc := NewServerService(rt)
	waitOverview(t, svc, 5*time.Second, "reachable", func(ov OverviewView) bool { return ov.Server.Reachable })
	rt.mu.Lock()
	firstMon := rt.mon
	rt.mu.Unlock()

	for i := range 2 {
		ov, err := svc.Reconnect()
		if err != nil {
			t.Fatalf("Reconnect %d: %v", i+1, err)
		}
		if !ov.Server.Reachable {
			t.Errorf("Reconnect %d returned an unreachable overview: %+v", i+1, ov.Server)
		}
	}
	rt.mu.Lock()
	mon := rt.mon
	rt.mu.Unlock()
	if mon == firstMon {
		t.Error("Reconnect kept the old monitor")
	}
	// Reconnect emits a fresh overview straight away, before its first poll.
	sawFresh := false
	for _, ov := range rec.overviews() {
		if ov.Server.Health == "unknown" {
			sawFresh = true
		}
	}
	if !sawFresh {
		t.Error("no fresh (not yet polled) overview emitted on reconnect")
	}

	rt.Stop()
	settleGoroutines(t, baseline+1)
	if err := rt.Reconnect(); err == nil {
		t.Error("Reconnect after Stop: want error")
	}
}

func TestReconnectAfterStartupError(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Sleeper)
	// VISOR_CONFIG names a file that does not exist yet: the app starts
	// with a connection error.
	conf := filepath.Join(t.TempDir(), "supervisord.conf")
	rt, _ := newRuntimeWith(t, RuntimeOptions{ConfigPath: conf, Interval: 50 * time.Millisecond})
	svc := NewServerService(rt)
	if ov := svc.Overview(); ov.Server.Error == "" || ov.Server.Health != "error" {
		t.Fatalf("overview with a missing config = %+v", ov.Server)
	}
	b, err := os.ReadFile(confOf(inst))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(conf, b, 0o600); err != nil {
		t.Fatal(err)
	}
	ov, err := svc.Reconnect()
	if err != nil {
		t.Fatal(err)
	}
	if !ov.Server.Reachable || ov.Server.Error != "" {
		t.Fatalf("after fixing the config and reconnecting: %+v", ov.Server)
	}
	if _, err := rt.conn(); err != nil {
		t.Errorf("actions still refused: %v", err)
	}
}

func TestSaveConnectionFixesStartupError(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Sleeper)
	stateDir := t.TempDir()
	bad := settings.Defaults()
	bad.ConfigPath = filepath.Join(t.TempDir(), "gone.conf")
	rt, _ := newRuntimeWith(t, RuntimeOptions{StateDir: stateDir, Settings: bad})
	svc := NewSettingsService(rt, stateDir, "", nil, nil)
	if ov := rt.Overview(); ov.Server.Health != "error" {
		t.Fatalf("overview with a missing config = %+v", ov.Server)
	}
	v, err := svc.SaveConnection(ConnectionInput{ConfigPath: confOf(inst)})
	if err != nil {
		t.Fatal(err)
	}
	if v.ConfigPath != confOf(inst) || v.Socket == "" || !strings.HasSuffix(v.Socket, ".sock") {
		t.Errorf("view = %+v", v)
	}
	waitOverview(t, NewServerService(rt), 5*time.Second, "reachable", func(ov OverviewView) bool { return ov.Server.Reachable })
	if got, _ := settings.Load(stateDir); got.ConfigPath != confOf(inst) {
		t.Errorf("saved config path = %q", got.ConfigPath)
	}
}

func TestSaveConnectionRejectsMissingPath(t *testing.T) {
	stateDir := t.TempDir()
	conf := deadConf(t)
	start := settings.Defaults()
	start.ConfigPath = conf
	rt, _ := newRuntimeWith(t, RuntimeOptions{StateDir: stateDir, Settings: start})
	svc := NewSettingsService(rt, stateDir, "", nil, nil)
	for name, path := range map[string]string{
		"missing":    filepath.Join(t.TempDir(), "nope.conf"),
		"folder":     t.TempDir(),
		"not an ini": writeConf(t, "this is [not\n"),
	} {
		_, err := svc.SaveConnection(ConnectionInput{ConfigPath: path})
		if err == nil {
			t.Errorf("%s: want error", name)
			continue
		}
		if !strings.Contains(err.Error(), path) {
			t.Errorf("%s: error %q does not name the path", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(stateDir, "settings.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a refused path was saved: %v", err)
	}
	if got := rt.Settings().ConfigPath; got != conf {
		t.Errorf("runtime config path = %q, want unchanged %q", got, conf)
	}
}

func TestEnvConfigOverridesSetting(t *testing.T) {
	envConf := deadConf(t)
	stateDir := t.TempDir()
	saved := settings.Defaults()
	saved.ConfigPath = deadConf(t)
	rt, _ := newRuntimeWith(t, RuntimeOptions{ConfigPath: envConf, StateDir: stateDir, Settings: saved})
	svc := NewSettingsService(rt, stateDir, envConf, nil, nil)
	v := svc.Get()
	if v.EnvConfigPath != envConf || v.ConfigPath != envConf {
		t.Errorf("view = %+v, want the VISOR_CONFIG path in use", v)
	}
	if v.Settings.ConfigPath != saved.ConfigPath {
		t.Errorf("saved path = %q, want %q kept", v.Settings.ConfigPath, saved.ConfigPath)
	}
	// The page cannot change the path while VISOR_CONFIG is set; the saved
	// one is kept, and the override stays in use.
	v, err := svc.SaveConnection(ConnectionInput{ConfigPath: "/elsewhere.conf", Username: "me"})
	if err != nil {
		t.Fatal(err)
	}
	if v.ConfigPath != envConf || v.Settings.ConfigPath != saved.ConfigPath || v.Settings.Username != "me" {
		t.Errorf("view after SaveConnection = %+v", v)
	}
}

func TestSettingsSaveClampsPersistsAndAppliesLive(t *testing.T) {
	stateDir := t.TempDir()
	conf := deadConf(t)
	start := settings.Defaults()
	start.ConfigPath = conf
	rt, _ := newRuntimeWith(t, RuntimeOptions{StateDir: stateDir, Settings: start})
	svc := NewSettingsService(rt, stateDir, "", nil, errors.New("old load error"))
	if svc.Get().LoadError == "" {
		t.Error("load error not shown")
	}
	rt.mu.Lock()
	mon, tracker := rt.mon, rt.tracker
	rt.mu.Unlock()

	in := svc.Get().Settings
	in.PollInterval = 100 * time.Millisecond // below the minimum
	in.CrashLoop = settings.CrashLoopRules{Restarts: 3, Window: 4 * time.Minute}
	in.RetentionDays = 1 // below the minimum
	in.Notify.Fatal = false
	in.ConfigPath, in.Username = "/ignored.conf", "ignored" // SaveConnection's job
	v, err := svc.Save(in)
	if err != nil {
		t.Fatal(err)
	}
	if v.Settings.PollInterval != time.Second || v.Settings.RetentionDays != 7 || v.Settings.Notify.Fatal {
		t.Errorf("saved view = %+v, want clamped values and Fatal off", v.Settings)
	}
	if v.Settings.ConfigPath != conf || v.Settings.Username != "" {
		t.Errorf("Save changed the connection: %+v", v.Settings)
	}
	if v.LoadError != "" {
		t.Errorf("load error still shown after a save: %q", v.LoadError)
	}
	got, err := settings.Load(stateDir)
	if err != nil || got != v.Settings {
		t.Errorf("settings.json = %+v, %v; want %+v", got, err, v.Settings)
	}

	// Applied in place: the same monitor and tracker, with the new values.
	rt.mu.Lock()
	sameMon, sameTracker := rt.mon == mon, rt.tracker == tracker
	rt.mu.Unlock()
	if !sameMon || !sameTracker {
		t.Error("Save rebuilt the connection; poll, crash-loop and retention should apply in place")
	}
	if mon.Interval() != time.Second {
		t.Errorf("monitor interval = %v, want 1s", mon.Interval())
	}
	if r := tracker.Rules(); r.CrashLoopRestarts != 3 || r.CrashLoopWindow != 4*time.Minute {
		t.Errorf("tracker rules = %+v", r)
	}
	if g := rt.actionGrace(expectation{}); g != minActionGrace {
		t.Errorf("actionGrace = %v, want %v", g, minActionGrace)
	}
}

func TestSettingsRetentionCompactsStore(t *testing.T) {
	stateDir := t.TempDir()
	old := time.Now().Add(-30 * 24 * time.Hour).UTC().Format(time.RFC3339Nano)
	rec := fmt.Sprintf(`{"id":"old","kind":"fatal","process":"g:w","message":"x","openedAt":%q,"closedAt":%q,"status":"resolved_auto"}`+"\n", old, old)
	if err := os.WriteFile(filepath.Join(stateDir, "issues.jsonl"), []byte(rec), 0o600); err != nil {
		t.Fatal(err)
	}
	start := settings.Defaults()
	start.ConfigPath = deadConf(t)
	rt, _ := newRuntimeWith(t, RuntimeOptions{StateDir: stateDir, Settings: start})
	issuesSvc := NewIssueService(rt)
	if list, _ := issuesSvc.List("all", 0, "", 0, ""); len(list) != 1 {
		t.Fatalf("issues before = %+v, want the 30-day-old one (kept 90 days)", list)
	}
	svc := NewSettingsService(rt, stateDir, "", nil, nil)
	in := svc.Get().Settings
	in.RetentionDays = 14
	if _, err := svc.Save(in); err != nil {
		t.Fatal(err)
	}
	if list, _ := issuesSvc.List("all", 0, "", 0, ""); len(list) != 0 {
		t.Errorf("issues after 14-day retention = %+v, want none", list)
	}
}

func TestSavedCredentialsOverrideConfig(t *testing.T) {
	keyring.MockInit() // never the real Keychain
	conf := authServer(t, "me", "s3cret")
	stateDir := t.TempDir()
	start := settings.Defaults()
	start.ConfigPath = conf
	rt, _ := newRuntimeWith(t, RuntimeOptions{StateDir: stateDir, Settings: start, Interval: 50 * time.Millisecond, Keychain: settings.SystemKeychain{}})
	server := NewServerService(rt)
	// The config's [supervisorctl] credentials are wrong.
	waitOverview(t, server, 5*time.Second, "unauthorized", func(ov OverviewView) bool {
		return !ov.Server.Reachable && ov.Server.Error != ""
	})

	svc := NewSettingsService(rt, stateDir, "", settings.SystemKeychain{}, nil)
	if _, err := svc.SaveConnection(ConnectionInput{ConfigPath: conf, Password: "s3cret", SetPassword: true}); err == nil {
		t.Error("a password without a username: want error")
	}
	v, err := svc.SaveConnection(ConnectionInput{ConfigPath: conf, Username: "me", Password: "s3cret", SetPassword: true})
	if err != nil {
		t.Fatal(err)
	}
	if !v.HasPassword || v.Settings.Username != "me" {
		t.Errorf("view = %+v", v)
	}
	if pw, err := keyring.Get(settings.KeychainService, conf); err != nil || pw != "s3cret" {
		t.Errorf("Keychain entry = %q, %v", pw, err)
	}
	waitOverview(t, server, 5*time.Second, "reachable with saved credentials", func(ov OverviewView) bool { return ov.Server.Reachable })
	b, _ := os.ReadFile(filepath.Join(stateDir, "settings.json"))
	if strings.Contains(string(b), "s3cret") {
		t.Error("the password was written to settings.json")
	}
	var onDisk map[string]any
	if err := json.Unmarshal(b, &onDisk); err != nil || onDisk["username"] != "me" {
		t.Errorf("settings.json = %s, %v", b, err)
	}

	// Clearing the username goes back to the config's (wrong) credentials;
	// removing the password removes the Keychain entry.
	v, err = svc.SaveConnection(ConnectionInput{ConfigPath: conf, SetPassword: true})
	if err != nil {
		t.Fatal(err)
	}
	if v.HasPassword {
		t.Error("HasPassword after removing it")
	}
	if _, err := keyring.Get(settings.KeychainService, conf); !errors.Is(err, keyring.ErrNotFound) {
		t.Errorf("Keychain entry after removing = %v", err)
	}
	waitOverview(t, server, 5*time.Second, "unauthorized again", func(ov OverviewView) bool {
		return !ov.Server.Reachable && ov.Server.Error != ""
	})
}

func TestKeychainErrorIsAConnectionError(t *testing.T) {
	keyring.MockInitWithError(errors.New("user interaction is not allowed"))
	t.Cleanup(keyring.MockInit)
	start := settings.Defaults()
	start.ConfigPath, start.Username = deadConf(t), "me"
	rt, _ := newRuntimeWith(t, RuntimeOptions{Settings: start, Keychain: settings.SystemKeychain{}})
	ov := rt.Overview()
	if ov.Server.Health != "error" || !strings.Contains(ov.Server.Error, "Keychain") {
		t.Errorf("server = %+v, want a Keychain error", ov.Server)
	}
}

func TestFollowEndsWithMessageOnReconnect(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Spammer)
	inst.WaitFor(t, "spammer:spammer", supervisor.StateRunning)
	rt, _ := startRuntime(t, confOf(inst), t.TempDir(), 100*time.Millisecond)
	waitOverview(t, NewServerService(rt), 5*time.Second, "reachable", func(ov OverviewView) bool { return ov.Server.Reachable })
	ls, rec := newLogService(t, rt)
	id, err := ls.Follow("spammer:spammer", "stdout")
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Reconnect(); err != nil {
		t.Fatal(err)
	}
	cs := waitChunks(t, rec, id, func(cs []LogChunkView) bool { return len(cs) > 0 && cs[len(cs)-1].Error != "" })
	if got := cs[len(cs)-1].Error; got != errReconnected {
		t.Errorf("last chunk error = %q, want %q", got, errReconnected)
	}
	if n := ls.active(); n != 0 {
		t.Errorf("%d follows still active", n)
	}
	// Unfollowing a follow ends it silently.
	id, err = ls.Follow("spammer:spammer", "stdout")
	if err != nil {
		t.Fatal(err)
	}
	ls.Unfollow(id)
	if err := rt.Reconnect(); err != nil {
		t.Fatal(err)
	}
	for _, c := range rec.logChunks(id) {
		if c.Error != "" {
			t.Errorf("an unfollowed log got %q", c.Error)
		}
	}
}

func TestTransportAddress(t *testing.T) {
	cases := map[string]supervisor.Transport{
		"":                      nil,
		"/tmp/s.sock":           supervisor.UnixTransport{Path: "/tmp/s.sock"},
		"http://127.0.0.1:9001": supervisor.TCPTransport{Addr: "127.0.0.1:9001"},
	}
	for want, tr := range cases {
		if got := transportAddress(tr); got != want {
			t.Errorf("transportAddress(%#v) = %q, want %q", tr, got, want)
		}
	}
}
