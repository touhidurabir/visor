package app

import (
	"context"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/touhidurabir/visor/internal/mcp"
	"github.com/touhidurabir/visor/internal/remote"
	"github.com/touhidurabir/visor/internal/remote/remotetest"
	"github.com/touhidurabir/visor/internal/settings"
	"github.com/touhidurabir/visor/internal/supervisor/supervisortest"
)

type mcpFixture struct {
	svc    *McpService
	rec    *recorder
	k      *settings.MemoryKeychain
	state  string
	hub    *Hub
	remote *RemoteService
	ssh    *remotetest.Server
	inst   *supervisortest.Instance
	shown  *atomic.Int32
	sender *fakeSender
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// newMcpFixture is this computer on a real supervisord, a test SSH server
// for a saved server, and an McpService on a free port.
func newMcpFixture(t *testing.T) mcpFixture {
	t.Helper()
	noSSHConfig(t)
	inst := supervisortest.Start(t, supervisortest.Sleeper)
	state := t.TempDir()
	ssh := remotetest.Start(t, remotetest.Options{Password: "pw"})
	k := &settings.MemoryKeychain{}
	hk := remote.HostKeys{VisorFile: filepath.Join(state, "known_hosts")}
	if err := hk.Trust(ssh.Host, ssh.Port, ssh.HostKey); err != nil {
		t.Fatal(err)
	}
	rec := &recorder{}
	prefs := settings.Defaults()
	prefs.PollInterval = 100 * time.Millisecond
	prefs.MCP.Port = freePort(t)
	local := NewRuntime(RuntimeOptions{ConfigPath: confOf(inst), StateDir: state, Settings: prefs, Interval: 100 * time.Millisecond, Emit: rec.emit})
	hub := NewHub(local, HubOptions{StateDir: state, Keychain: k, HostKeys: hk, Emit: rec.emit, Settings: local.Settings})
	if err := hub.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = hub.Shutdown() })
	rs := NewRemoteService(hub, state, k, hk, local.Settings)
	ss := NewSettingsService(local, state, "", k, nil)
	shown := &atomic.Int32{}
	sender := &fakeSender{}
	svc := NewMcpService(McpOptions{
		Hub: hub, Remote: rs, Settings: ss, Keychain: k, Emit: rec.emit,
		Notifier: sender, Show: func() { shown.Add(1) }, Version: "test",
	})
	if err := svc.ServiceStartup(context.Background(), application.ServiceOptions{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.ServiceShutdown() })
	waitRunning(t, NewServerService(local), "sleeper:sleeper")
	return mcpFixture{svc: svc, rec: rec, k: k, state: state, hub: hub, remote: rs, ssh: ssh, inst: inst, shown: shown, sender: sender}
}

// saveServer saves a server on the test SSH server, with its password.
func (f mcpFixture) saveServer(t *testing.T, name, tag string, readOnly bool) settings.Server {
	t.Helper()
	sv, err := f.remote.Save(ServerInput{
		Server:   settings.Server{Name: name, Tag: tag, ReadOnly: readOnly, Host: f.ssh.Host, Port: f.ssh.Port, User: "visor", ConfigPath: f.inst.ConfigPath()},
		Password: SecretInput{Value: "pw"}, KeyPassphrase: SecretInput{Keep: true}, SudoPassword: SecretInput{Keep: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	return sv
}

type bearer string

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+string(b))
	return http.DefaultTransport.RoundTrip(r)
}

// client connects an MCP client named name over HTTP with token.
func (f mcpFixture) client(t *testing.T, name, token string) (*sdk.ClientSession, error) {
	t.Helper()
	c := sdk.NewClient(&sdk.Implementation{Name: name, Version: "1"}, nil)
	return c.Connect(context.Background(), &sdk.StreamableClientTransport{
		Endpoint: f.svc.Get().URL, HTTPClient: &http.Client{Transport: bearer(token), Timeout: 30 * time.Second},
	}, nil)
}

func callText(t *testing.T, cs *sdk.ClientSession, tool string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String(), res.IsError
}

func TestMcpServiceLifecycle(t *testing.T) {
	f := newMcpFixture(t)
	v := f.svc.Get()
	if v.Status != McpOff || v.Enabled || v.Token == "" || len(v.Tools) != 25 || !strings.HasPrefix(v.URL, "http://127.0.0.1:") {
		t.Fatalf("before enabling: %+v", v)
	}
	if v, err := f.svc.SetEnabled(true); err != nil || v.Status != McpRunning {
		t.Fatalf("enable: %+v %v", v, err)
	}
	token := f.svc.Get().Token

	cs, err := f.client(t, "claude-code", token)
	if err != nil {
		t.Fatal(err)
	}
	text, isErr := callText(t, cs, "list_processes", nil)
	if isErr || !strings.Contains(text, "sleeper:sleeper") {
		t.Fatalf("list_processes: %s", text)
	}
	if calls := f.svc.Get().Calls; len(calls) != 1 || calls[0].Tool != "list_processes" || calls[0].Client != "Claude Code" || calls[0].Machine != localName() {
		t.Fatalf("recent calls: %+v", calls)
	}
	cs.Close()

	if _, err := f.client(t, "x", "wrong"); err == nil {
		t.Fatal("a wrong token connected")
	}

	// The token survives a new service on the same keychain.
	again := NewMcpService(McpOptions{Hub: f.hub, Remote: f.remote, Settings: f.svc.o.Settings, Keychain: f.k, Emit: f.rec.emit})
	if again.loadToken() != token {
		t.Fatal("the token wasn't kept")
	}

	nv, err := f.svc.RegenerateToken()
	if err != nil || nv.Token == token || nv.Status != McpRunning {
		t.Fatalf("regenerate: %+v %v", nv, err)
	}
	if _, err := f.client(t, "x", token); err == nil {
		t.Fatal("the old token still connects")
	}
	if _, err := f.client(t, "x", nv.Token); err != nil {
		t.Fatalf("the new token: %v", err)
	}

	if v, _ := f.svc.SetEnabled(false); v.Status != McpOff {
		t.Fatalf("disable: %+v", v)
	}
	if _, err := http.Get(f.svc.Get().URL); err == nil {
		t.Fatal("still listening after disable")
	}
	if f.svc.o.Settings.rt.Settings().MCP.Enabled {
		t.Fatal("the setting wasn't saved")
	}
}

func TestMcpPortInUse(t *testing.T) {
	f := newMcpFixture(t)
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := busy.Addr().(*net.TCPAddr).Port
	if _, err := f.svc.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	v, err := f.svc.SetPort(port)
	if err != nil || v.Status != McpPortInUse || v.Port != port || !strings.Contains(v.Error, strconv.Itoa(port)) {
		t.Fatalf("a busy port: %+v %v", v, err)
	}
	busy.Close()
	if v, err := f.svc.SetPort(port); err != nil || v.Status != McpRunning {
		t.Fatalf("after the port is free: %+v %v", v, err)
	}
	if _, err := f.svc.SetPort(80); err == nil {
		t.Fatal("port 80 accepted")
	}
}

func TestMcpAsk(t *testing.T) {
	f := newMcpFixture(t)
	f.svc.askTimeout = 300 * time.Millisecond
	ctx := context.Background()
	q := mcp.Question{ID: "ask-1", Client: "Claude Code", Verb: "restart", Subject: "horizon", Machine: "prod", MachineID: "p1", Production: true}

	// No answer: no.
	if ok, err := f.svc.ask(ctx, q); ok || err != nil {
		t.Fatalf("unanswered: %v %v", ok, err)
	}
	if f.shown.Load() == 0 || len(f.sender.messages()) == 0 || !strings.Contains(f.sender.messages()[0].Title, "Claude Code wants to restart horizon on prod") {
		t.Fatalf("not shown or notified: shown %d, %+v", f.shown.Load(), f.sender.messages())
	}

	// Two at once, each with its own answer.
	f.svc.askTimeout = 5 * time.Second
	var wg sync.WaitGroup
	results := map[string]bool{}
	var mu sync.Mutex
	for _, id := range []string{"ask-2", "ask-3"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			qq := q
			qq.ID = id
			ok, _ := f.svc.ask(ctx, qq)
			mu.Lock()
			results[id] = ok
			mu.Unlock()
		}(id)
	}
	waitAsks(t, f.svc, 2)
	if got := f.svc.Get().Asks; got[0].ID != "ask-2" && got[0].ID != "ask-3" {
		t.Fatalf("waiting asks: %+v", got)
	}
	if err := f.svc.Answer("ask-3", true, 0); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Answer("ask-2", false, 0); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	if !results["ask-3"] || results["ask-2"] {
		t.Fatalf("answers: %v", results)
	}
	if err := f.svc.Answer("ask-2", true, 0); err == nil {
		t.Fatal("answered a prompt twice")
	}

	// Allow for 10 minutes: the next change there doesn't ask.
	go func() {
		waitAsks(t, f.svc, 1)
		_ = f.svc.Answer("ask-4", true, 10)
	}()
	q.ID = "ask-4"
	if ok, _ := f.svc.ask(ctx, q); !ok {
		t.Fatal("allowed for 10 minutes: no")
	}
	q.ID = "ask-5"
	start := time.Now()
	if ok, _ := f.svc.ask(ctx, q); !ok || time.Since(start) > 100*time.Millisecond {
		t.Fatal("the next ask within 10 minutes asked again")
	}
	q.Client = "Codex"
	q.ID = "ask-6"
	f.svc.askTimeout = 200 * time.Millisecond
	if ok, _ := f.svc.ask(ctx, q); ok {
		t.Fatal("another client inherited the 10 minutes")
	}

	// Quitting says no to a waiting prompt.
	f.svc.askTimeout = time.Minute
	done := make(chan bool)
	go func() {
		qq := q
		qq.ID = "ask-7"
		ok, _ := f.svc.ask(ctx, qq)
		done <- ok
	}()
	waitAsks(t, f.svc, 1)
	_ = f.svc.ServiceShutdown()
	select {
	case ok := <-done:
		if ok {
			t.Fatal("quitting allowed the change")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("quitting left the prompt waiting")
	}
}

func waitAsks(t *testing.T, s *McpService, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(s.Get().Asks) == n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%d prompts waiting, want %d", len(s.Get().Asks), n)
}

func TestMcpSetAccessClamps(t *testing.T) {
	f := newMcpFixture(t)
	prod := f.saveServer(t, "prod", settings.TagProduction, false)
	ro := f.saveServer(t, "ro", "development", true)
	dev := f.saveServer(t, "dev", "development", false)

	v := f.svc.Get()
	byID := func(v McpView, id string) McpMachineView {
		for _, m := range v.Machines {
			if m.ID == id {
				return m
			}
		}
		t.Fatalf("no machine %s in %+v", id, v.Machines)
		return McpMachineView{}
	}
	if m := byID(v, "local"); m.Level != "control" || m.Ask || !m.Local {
		t.Fatalf("this computer by default: %+v", m)
	}
	if m := byID(v, prod.ID); m.Level != "off" || !m.Ask || !m.AskLocked {
		t.Fatalf("production by default: %+v", m)
	}

	if v, err := f.svc.SetAccess(prod.ID, "configs", false); err != nil || byID(v, prod.ID).Level != "configs" || !byID(v, prod.ID).Ask {
		t.Fatalf("production without asking: %+v %v", v, err)
	}
	if v, err := f.svc.SetAccess(ro.ID, "configs", true); err != nil || byID(v, ro.ID).Level != "read" || byID(v, ro.ID).MaxLevel != "read" {
		t.Fatalf("a Read only server above Read: %+v %v", v, err)
	}
	if v, err := f.svc.SetAccess(dev.ID, "control", false); err != nil || byID(v, dev.ID).Ask {
		t.Fatalf("dev without asking: %+v %v", v, err)
	}
	if _, err := f.svc.SetAccess("nope", "read", false); err == nil {
		t.Fatal("an unknown machine accepted")
	}
	if _, err := f.svc.SetAccess("local", "admin", false); err == nil {
		t.Fatal("an unknown level accepted")
	}
	if got := f.svc.o.Settings.rt.Settings().MCP.Access[prod.ID]; got != (settings.MCPAccess{Level: "configs", Ask: true}) {
		t.Fatalf("stored: %+v", got)
	}
	// The Settings page saving its own copy keeps MCP's.
	s := f.svc.o.Settings.Get().Settings
	s.MCP = settings.MCPSettings{}
	if _, err := f.svc.o.Settings.Save(s); err != nil {
		t.Fatal(err)
	}
	if f.svc.o.Settings.rt.Settings().MCP.Access[prod.ID].Level != "configs" {
		t.Fatal("a Settings save dropped the MCP settings")
	}
}

func TestMcpOpensASavedServer(t *testing.T) {
	f := newMcpFixture(t)
	sv := f.saveServer(t, "vm", "development", false)
	if _, err := f.svc.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	cs, err := f.client(t, "codex", f.svc.Get().Token)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	// Off: invisible.
	if text, isErr := callText(t, cs, "get_status", map[string]any{"server": "vm"}); !isErr || !strings.Contains(text, `No server named "vm"`) {
		t.Fatalf("an Off server: %s", text)
	}
	if _, err := f.svc.SetAccess(sv.ID, "read", false); err != nil {
		t.Fatal(err)
	}
	text, isErr := callText(t, cs, "get_status", map[string]any{"server": "vm"})
	if isErr || !strings.Contains(text, `"Reachable":true`) {
		t.Fatalf("get_status on a server opened by MCP: %s", text)
	}
	if !containsID(f.hub.Open(), sv.ID) {
		t.Fatal("no tab opened for the server")
	}
	if text, isErr := callText(t, cs, "restart", map[string]any{"server": "vm", "target": "sleeper"}); !isErr || !strings.Contains(text, "set to Read") {
		t.Fatalf("restart on a Read server: %s", text)
	}

	// A server whose password isn't saved isn't opened.
	if err := f.hub.CloseServer(sv.ID); err != nil {
		t.Fatal(err)
	}
	if err := settings.DeleteServerSecrets(f.k, sv.ID); err != nil {
		t.Fatal(err)
	}
	text, isErr = callText(t, cs, "get_status", map[string]any{"server": "vm"})
	if !isErr || !strings.Contains(text, "Open it in Visor first") {
		t.Fatalf("without a saved password: %s", text)
	}
}

func containsID(ids []string, id string) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// A change through the MCP endpoint shows in this computer's Activity as
// made via that client.
func TestMcpChangeShowsViaInActivity(t *testing.T) {
	old := activityGroupWait
	activityGroupWait = 200 * time.Millisecond
	t.Cleanup(func() { activityGroupWait = old })
	f := newMcpFixture(t)
	if _, err := f.svc.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	cs, err := f.client(t, "e2e-test", f.svc.Get().Token)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	if text, isErr := callText(t, cs, "restart", map[string]any{"target": "sleeper"}); isErr {
		t.Fatalf("restart: %s", text)
	}
	e := waitEvent(t, NewIssueService(f.hub.Local().RT), "sleeper restarted", func(e ActivityEventView) bool {
		return e.Process == "sleeper:sleeper" && e.Kind == "process_restarted"
	})
	if e.Via != "e2e-test" {
		t.Fatalf("restart through MCP: %+v", e)
	}
}
