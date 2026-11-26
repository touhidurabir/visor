package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeBackend is one or more machines whose targets record what was called.
type fakeBackend struct {
	mu       sync.Mutex
	machines []Machine
	asked    []Question
	allow    bool
	calls    []string // "machine:method:client:arg"
	recorded []Call
	env      bool
	block    chan struct{} // Status waits on it when set
	panics   bool          // Process panics when set
	panicsAt string        // Target panics for this machine id when set
}

func (b *fakeBackend) Machines() []Machine {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]Machine(nil), b.machines...)
}

func (b *fakeBackend) setLevel(id string, l Level) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.machines {
		if b.machines[i].ID == id {
			b.machines[i].Level = l
		}
	}
}

func (b *fakeBackend) Target(_ context.Context, m Machine) (Target, error) {
	if b.panicsAt == m.ID {
		panic("dial failed oddly")
	}
	return &fakeTarget{b: b, m: m.ID}, nil
}

func (b *fakeBackend) Ask(_ context.Context, q Question) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.asked = append(b.asked, q)
	return b.allow, nil
}

func (b *fakeBackend) AllowEnv() bool { return b.env }

func (b *fakeBackend) Recorded(c Call) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.recorded = append(b.recorded, c)
}

func (b *fakeBackend) log(m, method, client, arg string) (any, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls = append(b.calls, m+":"+method+":"+client+":"+arg)
	return map[string]any{"ok": true, "method": method}, nil
}

type fakeTarget struct {
	b *fakeBackend
	m string
}

func (t *fakeTarget) Status() (any, error) {
	if t.b.block != nil {
		<-t.b.block
	}
	return t.b.log(t.m, "Status", "", "")
}
func (t *fakeTarget) Processes(in ProcessesIn) (any, error) {
	return t.b.log(t.m, "Processes", "", in.State)
}
func (t *fakeTarget) Process(in ProcessIn) (any, error) {
	if t.b.panics {
		panic("boom")
	}
	return t.b.log(t.m, "Process", "", in.Name)
}
func (t *fakeTarget) Usage(in UsageIn) (any, error)     { return t.b.log(t.m, "Usage", "", in.Process) }
func (t *fakeTarget) Logs(in LogsIn) (any, error)       { return t.b.log(t.m, "Logs", "", in.Process) }
func (t *fakeTarget) SearchLogs(in SearchIn) (any, error) {
	return t.b.log(t.m, "SearchLogs", "", in.Query)
}
func (t *fakeTarget) SupervisordLog(in SupervisordLogIn) (any, error) {
	return t.b.log(t.m, "SupervisordLog", "", "")
}
func (t *fakeTarget) Issues(in IssuesIn) (any, error)     { return t.b.log(t.m, "Issues", "", "") }
func (t *fakeTarget) Activity(in ActivityIn) (any, error) { return t.b.log(t.m, "Activity", "", "") }
func (t *fakeTarget) Configs() (any, error)               { return t.b.log(t.m, "Configs", "", "") }
func (t *fakeTarget) ReadConfig(in PathIn, env bool) (any, error) {
	return t.b.log(t.m, "ReadConfig", "", fmt.Sprintf("%s %v", in.Path, env))
}
func (t *fakeTarget) CheckConfig(in CheckIn, env bool) (any, error) {
	return t.b.log(t.m, "CheckConfig", "", in.Path)
}
func (t *fakeTarget) PendingChanges(in OptPathIn) (any, error) {
	return t.b.log(t.m, "PendingChanges", "", in.Path)
}
func (t *fakeTarget) DraftProgram(in DraftIn) (any, error) {
	return t.b.log(t.m, "DraftProgram", "", in.Template)
}
func (t *fakeTarget) Backups(in PathIn) (any, error) { return t.b.log(t.m, "Backups", "", in.Path) }
func (t *fakeTarget) Diagnose() (any, error)         { return t.b.log(t.m, "Diagnose", "", "") }
func (t *fakeTarget) Start(c string, in TargetIn) (any, error) {
	return t.b.log(t.m, "Start", c, in.Target)
}
func (t *fakeTarget) Stop(c string, in TargetIn) (any, error) {
	return t.b.log(t.m, "Stop", c, in.Target)
}
func (t *fakeTarget) Restart(c string, in TargetIn) (any, error) {
	return t.b.log(t.m, "Restart", c, in.Target)
}
func (t *fakeTarget) Signal(c string, in SignalIn) (any, error) {
	return t.b.log(t.m, "Signal", c, in.Signal)
}
func (t *fakeTarget) Apply(c string, in OptPathIn) (any, error) {
	return t.b.log(t.m, "Apply", c, in.Path)
}
func (t *fakeTarget) SaveConfig(c string, in SaveIn) (any, error) {
	return t.b.log(t.m, "SaveConfig", c, in.Path)
}
func (t *fakeTarget) CreateConfig(c string, in CreateIn) (any, error) {
	return t.b.log(t.m, "CreateConfig", c, in.FileName)
}
func (t *fakeTarget) RemoveConfig(c string, in PathIn) (any, error) {
	return t.b.log(t.m, "RemoveConfig", c, in.Path)
}
func (t *fakeTarget) RestoreConfig(c string, in RestoreIn) (any, error) {
	return t.b.log(t.m, "RestoreConfig", c, in.Path)
}

func newFake() *fakeBackend {
	return &fakeBackend{machines: []Machine{
		{ID: "local", Name: "This Mac", Local: true, Connected: true, Level: Control},
		{ID: "s1", Name: "staging", Tag: "staging", Level: Read, Ask: true},
		{ID: "s2", Name: "prod", Tag: "production", Connected: true, Level: Control},
		{ID: "s3", Name: "hidden", Tag: "development", Level: Off, Ask: true},
	}}
}

// connect starts s and an in-memory client that calls itself clientName.
func connect(t *testing.T, s *Server, clientName string) *sdk.ClientSession {
	t.Helper()
	ct, st := sdk.NewInMemoryTransports()
	ctx := context.Background()
	ss, err := s.SDK.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })
	c := sdk.NewClient(&sdk.Implementation{Name: clientName, Version: "1"}, nil)
	cs, err := c.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

// call runs a tool and returns its text and whether it was an error.
func call(t *testing.T, cs *sdk.ClientSession, tool string, args map[string]any) (string, bool) {
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

func TestToolTable(t *testing.T) {
	want := []string{
		"list_servers", "get_status", "list_processes", "get_process", "get_resource_usage", "read_logs", "search_logs",
		"read_supervisord_log", "list_issues", "get_activity", "list_configs", "read_config",
		"check_config", "get_pending_changes", "draft_program", "list_backups", "diagnose",
		"start", "stop", "restart", "signal_process", "apply_changes",
		"save_config", "create_config", "remove_config", "restore_config",
	}
	if len(Tools) != len(want) {
		t.Fatalf("%d tools, want %d", len(Tools), len(want))
	}
	destructive := map[string]bool{"stop": true, "restart": true, "signal_process": true, "remove_config": true, "save_config": true}
	for i, ti := range Tools {
		if ti.Name != want[i] {
			t.Errorf("tool %d is %s, want %s", i, ti.Name, want[i])
		}
		wantLevel := Read
		switch {
		case i >= 22:
			wantLevel = Configs
		case i >= 17:
			wantLevel = Control
		}
		if ti.Level != wantLevel {
			t.Errorf("%s needs %v, want %v", ti.Name, ti.Level, wantLevel)
		}
		if ti.Destructive != destructive[ti.Name] {
			t.Errorf("%s destructive = %v", ti.Name, ti.Destructive)
		}
		if ti.Description == "" {
			t.Errorf("%s has no description", ti.Name)
		}
	}

	cs := connect(t, NewServer(newFake(), "test"), "test")
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Tools) != len(want) {
		t.Fatalf("server lists %d tools", len(res.Tools))
	}
	for _, tl := range res.Tools {
		a := tl.Annotations
		if a == nil || a.DestructiveHint == nil || a.OpenWorldHint == nil {
			t.Fatalf("%s: hints not all set: %+v", tl.Name, a)
		}
		if *a.OpenWorldHint {
			t.Errorf("%s: openWorldHint true", tl.Name)
		}
		read := slicesIndex(want, tl.Name) < 17
		if a.ReadOnlyHint != read {
			t.Errorf("%s: readOnlyHint = %v", tl.Name, a.ReadOnlyHint)
		}
		if *a.DestructiveHint != destructive[tl.Name] {
			t.Errorf("%s: destructiveHint = %v", tl.Name, *a.DestructiveHint)
		}
		if tl.Title == "" {
			t.Errorf("%s: no title", tl.Name)
		}
		schema, _ := json.Marshal(tl.InputSchema)
		if !strings.Contains(string(schema), "leave it out for this computer") {
			t.Errorf("%s: input schema has no server description: %s", tl.Name, schema)
		}
		if tl.Name == "read_logs" && !strings.Contains(string(schema), `"required":["process"]`) {
			t.Errorf("read_logs: process isn't required: %s", schema)
		}
	}
}

func slicesIndex(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

func TestAccessRefusals(t *testing.T) {
	b := newFake()
	cs := connect(t, NewServer(b, "test"), "claude-code")

	text, isErr := call(t, cs, "restart", map[string]any{"server": "staging", "target": "web"})
	if !isErr || text != "staging is set to Read for MCP. Raise it on Visor's MCP page." {
		t.Fatalf("restart on a Read server: %q (error %v)", text, isErr)
	}
	if _, isErr := call(t, cs, "list_processes", map[string]any{"server": "staging"}); isErr {
		t.Fatal("list_processes on a Read server failed")
	}
	if text, isErr := call(t, cs, "get_status", map[string]any{"server": "hidden"}); !isErr ||
		text != `No server named "hidden". list_servers shows the ones Visor can use.` {
		t.Fatalf("an Off server: %q", text)
	}
	if text, isErr := call(t, cs, "get_status", map[string]any{"server": "nope"}); !isErr || !strings.HasPrefix(text, `No server named "nope"`) {
		t.Fatalf("an unknown server: %q", text)
	}
	if text, isErr := call(t, cs, "save_config", map[string]any{"path": "/x.conf", "content": "", "hash": "h"}); !isErr ||
		text != "This Mac is set to Control for MCP. Raise it on Visor's MCP page." {
		t.Fatalf("save on Control: %q", text)
	}
	// By id too, and a missing server is this computer.
	if _, isErr := call(t, cs, "get_status", map[string]any{"server": "s2"}); isErr {
		t.Fatal("server by id failed")
	}
	if _, isErr := call(t, cs, "get_status", nil); isErr {
		t.Fatal("get_status on this computer failed")
	}

	text, _ = call(t, cs, "list_servers", nil)
	var got struct {
		Servers []struct {
			Name   string `json:"name"`
			Access string `json:"access"`
		} `json:"servers"`
	}
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("list_servers: %v: %s", err, text)
	}
	var names []string
	for _, s := range got.Servers {
		names = append(names, s.Name+"="+s.Access)
	}
	if strings.Join(names, ",") != "This Mac=Control,staging=Read,prod=Control" {
		t.Fatalf("list_servers = %v", names)
	}

	b.machines[0].ReadOnly = true // never for this computer, but the clamp is generic
	b.machines[1].ReadOnly = true
	b.machines[1].Level = Configs
	if text, _ := call(t, cs, "restart", map[string]any{"server": "staging", "target": "web"}); text !=
		"staging is a Read only server, so MCP can only read it." {
		t.Fatalf("Read only clamp: %q", text)
	}
}

func TestAskFlow(t *testing.T) {
	b := newFake()
	cs := connect(t, NewServer(b, "test"), "claude-code")

	text, isErr := call(t, cs, "restart", map[string]any{"server": "prod", "target": "horizon"})
	if !isErr || text != "The person declined in Visor." {
		t.Fatalf("declined: %q", text)
	}
	if len(b.calls) != 0 {
		t.Fatalf("declined call reached the target: %v", b.calls)
	}
	q := b.asked[0]
	if q.Client != "Claude Code" || q.Verb != "restart" || q.Subject != "horizon" || q.Machine != "prod" || !q.Production || q.ID == "" {
		t.Fatalf("question = %+v", q)
	}

	b.allow = true
	if _, isErr := call(t, cs, "restart", map[string]any{"server": "prod", "target": "horizon"}); isErr {
		t.Fatal("allowed restart failed")
	}
	if b.calls[0] != "s2:Restart:Claude Code:horizon" {
		t.Fatalf("calls = %v", b.calls)
	}

	// KILL asks on a machine that doesn't ask; HUP doesn't.
	b.asked = nil
	b.allow = false
	call(t, cs, "signal_process", map[string]any{"name": "web", "signal": "HUP"})
	if len(b.asked) != 0 {
		t.Fatal("HUP asked on a machine that doesn't ask")
	}
	if text, _ := call(t, cs, "signal_process", map[string]any{"name": "web", "signal": "KILL"}); text != "The person declined in Visor." {
		t.Fatalf("KILL without asking: %q", text)
	}
	if len(b.asked) != 1 || b.asked[0].Verb != "send KILL to" || !b.asked[0].Force {
		t.Fatalf("KILL question: %+v", b.asked)
	}
	if text, _ := call(t, cs, "signal_process", map[string]any{"name": "web", "signal": "STOP"}); !strings.Contains(text, "HUP, USR1, USR2, TERM, INT, QUIT or KILL") {
		t.Fatalf("an unknown signal: %q", text)
	}

	// Reads never ask, even on production.
	b.asked = nil
	call(t, cs, "read_logs", map[string]any{"server": "prod", "process": "horizon"})
	if len(b.asked) != 0 {
		t.Fatal("a read asked")
	}
}

func TestLevelChangesMidSession(t *testing.T) {
	b := newFake()
	cs := connect(t, NewServer(b, "test"), "codex")
	if _, isErr := call(t, cs, "restart", map[string]any{"target": "web"}); isErr {
		t.Fatal("first restart failed")
	}
	b.setLevel("local", Read)
	if text, isErr := call(t, cs, "restart", map[string]any{"target": "web"}); !isErr || !strings.Contains(text, "set to Read") {
		t.Fatalf("restart after lowering the level: %q", text)
	}
	b.setLevel("local", Off)
	if text, _ := call(t, cs, "get_status", nil); text != "This Mac is set to Off for MCP. Raise it on Visor's MCP page." {
		t.Fatalf("this computer Off: %q", text)
	}
}

func TestCallsRing(t *testing.T) {
	b := newFake()
	s := NewServer(b, "test")
	tick := time.Now()
	s.lim = NewLimiter(func() time.Time { tick = tick.Add(time.Second); return tick })
	cs := connect(t, s, "cursor")
	for i := 0; i < 101; i++ {
		call(t, cs, "get_status", nil)
	}
	call(t, cs, "restart", map[string]any{"server": "staging", "target": "web"})
	calls := s.Calls()
	if len(calls) != 100 {
		t.Fatalf("%d calls kept, want 100", len(calls))
	}
	if c := calls[0]; c.Tool != "restart" || c.Result != "refused" || c.Client != "Cursor" || c.Machine != "staging" {
		t.Fatalf("newest call = %+v", c)
	}
	if c := calls[1]; c.Tool != "get_status" || c.Result != "ok" || c.Machine != "This Mac" {
		t.Fatalf("a read = %+v", c)
	}
	if len(b.recorded) != 102 {
		t.Fatalf("backend told of %d calls", len(b.recorded))
	}
}

func TestChangeGapIsWaitedOut(t *testing.T) {
	b := newFake()
	cs := connect(t, NewServer(b, "test"), "x")
	call(t, cs, "restart", map[string]any{"target": "a"})
	start := time.Now()
	text, isErr := call(t, cs, "restart", map[string]any{"target": "b"})
	if isErr || time.Since(start) < 900*time.Millisecond {
		t.Fatalf("second change at once: %q after %s, want it to wait out the second", text, time.Since(start))
	}
}

func TestReadsTimeOut(t *testing.T) {
	old := readTimeout
	readTimeout = 200 * time.Millisecond
	t.Cleanup(func() { readTimeout = old })
	b := newFake()
	b.block = make(chan struct{})
	defer close(b.block)
	cs := connect(t, NewServer(b, "test"), "x")
	start := time.Now()
	text, isErr := call(t, cs, "get_status", nil)
	if !isErr || !strings.Contains(text, "didn't answer within") || time.Since(start) > 2*time.Second {
		t.Fatalf("a stuck read: %q after %s", text, time.Since(start))
	}
}

func TestClientLabel(t *testing.T) {
	for in, want := range map[string]string{
		"claude-code": "Claude Code", "codex-mcp-client": "Codex", "codex": "Codex",
		"Cursor": "Cursor", "my-agent": "my-agent", "": "An AI tool",
	} {
		if got := ClientLabel(in); got != want {
			t.Errorf("ClientLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEnvPassedThrough(t *testing.T) {
	b := newFake()
	cs := connect(t, NewServer(b, "test"), "x")
	call(t, cs, "read_config", map[string]any{"path": "/a.conf"})
	b.env = true
	call(t, cs, "read_config", map[string]any{"path": "/a.conf"})
	if b.calls[0] != "local:ReadConfig::/a.conf false" || b.calls[1] != "local:ReadConfig::/a.conf true" {
		t.Fatalf("calls = %v", b.calls)
	}
}

// A tool that panics (a bug, or a reply from a server it didn't expect) fails
// that one call; it must not take the whole app down.
func TestToolPanicIsAnError(t *testing.T) {
	b := newFake()
	b.panics = true
	s := NewServer(b, "test")
	cs := connect(t, s, "test")
	text, isErr := call(t, cs, "get_process", map[string]any{"name": "web"})
	if !isErr || !strings.Contains(text, "internal error") {
		t.Fatalf("get_process = %q, error %v", text, isErr)
	}
	if got := s.Calls(); len(got) != 1 || got[0].Result != ResultError {
		t.Fatalf("recorded %+v", got)
	}
	b.panics = false
	if _, isErr := call(t, cs, "get_process", map[string]any{"name": "web"}); isErr {
		t.Fatal("the server stopped working after a panic")
	}
}

// A panic while opening the machine (before the tool runs) fails that call too.
func TestTargetPanicIsAnError(t *testing.T) {
	b := newFake()
	b.panicsAt = "local"
	cs := connect(t, NewServer(b, "test"), "test")
	text, isErr := call(t, cs, "restart", map[string]any{"target": "web"})
	if !isErr || !strings.Contains(text, "internal error") {
		t.Fatalf("restart = %q, error %v", text, isErr)
	}
	b.panicsAt = ""
	if _, isErr := call(t, cs, "restart", map[string]any{"target": "web"}); isErr {
		t.Fatal("the server stopped working after a panic")
	}
}
