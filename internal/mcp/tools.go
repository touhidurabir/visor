package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ToolInfo is a tool as the MCP page lists it.
type ToolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Level       Level  `json:"-"`
	LevelName   string `json:"level"` // Level.String(), for the frontend
	Destructive bool   `json:"destructive"`
}

// Server is Visor's MCP server: the SDK server with Visor's tools, the rate
// limiter and the recent calls.
type Server struct {
	SDK   *sdk.Server
	b     Backend
	lim   *Limiter
	calls calls
	seq   atomic.Uint64
}

// ErrDeclined: the person said no in Visor, or didn't answer in time.
var ErrDeclined = errors.New("The person declined in Visor.") //nolint:staticcheck // shown to the agent as a sentence

// Signals agents may send (spec §3.2).
var Signals = []string{"HUP", "USR1", "USR2", "TERM", "INT", "QUIT", "KILL"}

type askSpec struct {
	verb, subject string
	force         bool // ask even where asking is off (KILL)
}

type entry struct {
	info       ToolInfo
	title      string
	idempotent bool
	register   func(s *Server, t *sdk.Tool)
}

type input interface{ server() string }

// read is a tool that changes nothing.
func read[In input](name, title, desc string, run func(t Target, in In, env bool) (any, error)) entry {
	return entry{
		info: ToolInfo{Name: name, Description: desc, Level: Read}, title: title, idempotent: true,
		register: func(s *Server, t *sdk.Tool) {
			sdk.AddTool(s.SDK, t, func(ctx context.Context, req *sdk.CallToolRequest, in In) (*sdk.CallToolResult, any, error) {
				out, err := s.handle(ctx, req, name, Read, in.server(), nil, func(tg Target, _ string) (any, error) {
					return withTimeout(readTimeout, func() (any, error) { return run(tg, in, s.b.AllowEnv()) })
				})
				return nil, out, err
			})
		},
	}
}

// change is a tool that changes something: it needs level, may ask first,
// and counts against the one-change-per-second limit.
func change[In input](name, title, desc string, level Level, destructive, idempotent bool,
	ask func(In) (askSpec, error), run func(t Target, client string, in In) (any, error)) entry {
	return entry{
		info:  ToolInfo{Name: name, Description: desc, Level: level, Destructive: destructive},
		title: title, idempotent: idempotent,
		register: func(s *Server, t *sdk.Tool) {
			sdk.AddTool(s.SDK, t, func(ctx context.Context, req *sdk.CallToolRequest, in In) (*sdk.CallToolResult, any, error) {
				a, err := ask(in)
				if err != nil {
					s.record(req, name, "", ResultError, err)
					return nil, nil, err
				}
				out, err := s.handle(ctx, req, name, level, in.server(), &a, func(tg Target, client string) (any, error) {
					return run(tg, client, in)
				})
				return nil, out, err
			})
		},
	}
}

func subjectOf(target string) string {
	if strings.Contains(target, "/") {
		return filepath.Base(target)
	}
	return target
}

func onTarget(verb string) func(TargetIn) (askSpec, error) {
	return func(in TargetIn) (askSpec, error) {
		if strings.TrimSpace(in.Target) == "" {
			return askSpec{}, errors.New("target is empty: name a process, a group:* or a config file's path")
		}
		return askSpec{verb: verb, subject: subjectOf(in.Target)}, nil
	}
}

func onPath(verb string) func(PathIn) (askSpec, error) {
	return func(in PathIn) (askSpec, error) {
		if strings.TrimSpace(in.Path) == "" {
			return askSpec{}, errors.New("path is empty: give the config file's full path, as list_configs shows it")
		}
		return askSpec{verb: verb, subject: filepath.Base(in.Path)}, nil
	}
}

var entries = []entry{
	{
		info:  ToolInfo{Name: "list_servers", Level: Read, Description: "This computer and the saved servers AI tools may use: name, id, tag, whether connected, the MCP access level, and Read only. Use a name or id as the server argument of the other tools. Servers set to Off aren't listed."},
		title: "List servers", idempotent: true,
		register: func(s *Server, t *sdk.Tool) {
			sdk.AddTool(s.SDK, t, func(_ context.Context, req *sdk.CallToolRequest, _ ServerIn) (*sdk.CallToolResult, any, error) {
				out, err := safeRun(func() (any, error) { return s.listServers(), nil })
				if err != nil {
					s.record(req, "list_servers", "", ResultError, err)
					return nil, nil, err
				}
				s.record(req, "list_servers", "", ResultOK, nil)
				return nil, out, nil
			})
		},
	},
	read("get_status", "Get status", "supervisord's state, version and uptime, process counts, health, the failing processes, the open issue count, whether config files changed on disk but aren't applied yet, and the machine's CPU cores and total memory (to weigh get_resource_usage against). Start here.",
		func(t Target, _ ServerIn, _ bool) (any, error) { return t.Status() }),
	read("list_processes", "List processes", "Every process: name, group, state, PID, uptime or time since it stopped, last exit status, config file and description. Filter by state, config or name.",
		func(t Target, in ProcessesIn, _ bool) (any, error) { return t.Processes(in) }),
	read("get_process", "Get a process", "One process in depth: state, last error, recent state history, its open issues, and its program settings (environment values hidden unless allowed).",
		func(t Target, in ProcessIn, _ bool) (any, error) { return t.Process(in) }),
	read("get_resource_usage", "Get CPU and memory", "CPU (100 % is one core) and resident memory of each running process with the children it starts, now and over the last hour (peak and average), sampled every 10 seconds. Answers which process uses the most CPU or memory. Unknown values are null: a stopped process, or one the OS won't measure.",
		func(t Target, in UsageIn, _ bool) (any, error) { return t.Usage(in) }),
	read("read_logs", "Read logs", "The end of a process's stdout or stderr log, 100 lines by default and at most 500. Use the returned cursor as before to page back; atStart means the start of the log was reached. errors_only keeps error lines and the stack traces under them.",
		func(t Target, in LogsIn, _ bool) (any, error) { return t.Logs(in) }),
	read("search_logs", "Search logs", "Lines matching text (ignoring case) in a process's log, newest first, with line numbers. By default the last megabyte; page back with the returned cursor. whole_file only when Visor's Settings allow searching whole log files.",
		func(t Target, in SearchIn, _ bool) (any, error) { return t.SearchLogs(in) }),
	read("read_supervisord_log", "Read supervisord's log", "The end of supervisord's own log: why it stopped, failed to start, or gave up on a process.",
		func(t Target, in SupervisordLogIn, _ bool) (any, error) { return t.SupervisordLog(in) }),
	read("list_issues", "List issues", "Open issues (can't start, exited unexpectedly, keeps restarting, not reachable) with counts and times; resolved ones too when asked.",
		func(t Target, in IssuesIn, _ bool) (any, error) { return t.Issues(in) }),
	read("get_activity", "Get activity", "What changed and who did it: processes started, stopped or restarted, programs added or removed, configs changed on disk, supervisord down or back. Each says by Visor, outside Visor, or via an AI tool.",
		func(t Target, in ActivityIn, _ bool) (any, error) { return t.Activity(in) }),
	read("list_configs", "List configs", "Config files in supervisord's include folder with their programs, and whether each changed on disk, is watched, or isn't loaded.",
		func(t Target, _ ServerIn, _ bool) (any, error) { return t.Configs() }),
	read("read_config", "Read a config", "A config file's text and its hash (pass the hash to save_config). Environment values are hidden unless Visor allows AI to read them.",
		func(t Target, in PathIn, env bool) (any, error) { return t.ReadConfig(in, env) }),
	read("check_config", "Check a config", "Visor's plain-English explanation of a config file or draft text, and its problems (errors, warnings, suggestions), checked on that machine. Nothing is written.",
		func(t Target, in CheckIn, env bool) (any, error) { return t.CheckConfig(in, env) }),
	read("get_pending_changes", "Get pending changes", "What applying would do, as supervisorctl update would: groups to add, restart or remove, for one file or all. Nothing is applied.",
		func(t Target, in OptPathIn, _ bool) (any, error) { return t.PendingChanges(in) }),
	read("draft_program", "Draft a program", "Fills one of Visor's templates (queue for a Laravel queue worker, horizon, scheduler, node, script) or plain values into a [program:x] block and checks it. Nothing is written: pass the text to create_config.",
		func(t Target, in DraftIn, _ bool) (any, error) { return t.DraftProgram(in) }),
	read("list_backups", "List backups", "Saved versions of a config file, newest first, with their ids for restore_config.",
		func(t Target, in PathIn, _ bool) (any, error) { return t.Backups(in) }),
	read("diagnose", "Diagnose", "Visor's checks: Doctor on this computer, or the server report on a server. What works, what doesn't, and how to fix it. Nothing is fixed.",
		func(t Target, _ ServerIn, _ bool) (any, error) { return t.Diagnose() }),

	change("start", "Start", "Starts a process, a whole group (group:*), or every process of a config file (its path).", Control, false, true,
		onTarget("start"), func(t Target, c string, in TargetIn) (any, error) { return t.Start(c, in) }),
	change("stop", "Stop", "Stops a process, a whole group (group:*), or every process of a config file (its path).", Control, true, true,
		onTarget("stop"), func(t Target, c string, in TargetIn) (any, error) { return t.Stop(c, in) }),
	change("restart", "Restart", "Restarts a process, a whole group (group:*), or every process of a config file (its path).", Control, true, false,
		onTarget("restart"), func(t Target, c string, in TargetIn) (any, error) { return t.Restart(c, in) }),
	change("signal_process", "Send a signal", "Sends a signal to a process: HUP, USR1, USR2, TERM, INT, QUIT or KILL, for example HUP to reload. KILL always asks in Visor.", Control, true, false,
		func(in SignalIn) (askSpec, error) {
			sig := strings.ToUpper(strings.TrimPrefix(strings.ToUpper(in.Signal), "SIG"))
			if !slices.Contains(Signals, sig) {
				return askSpec{}, fmt.Errorf("signal %q isn't allowed: use HUP, USR1, USR2, TERM, INT, QUIT or KILL", in.Signal)
			}
			if strings.TrimSpace(in.Name) == "" {
				return askSpec{}, errors.New("name is empty: give the process as group:name or name")
			}
			return askSpec{verb: "send " + sig + " to", subject: in.Name, force: sig == "KILL"}, nil
		},
		func(t Target, c string, in SignalIn) (any, error) {
			in.Signal = strings.TrimPrefix(strings.ToUpper(in.Signal), "SIG")
			return t.Signal(c, in)
		}),
	change("apply_changes", "Apply changes", "Applies config files changed on disk, as supervisorctl update does, for one file or every changed file, and reports what was added, restarted or removed. A changed program in a [group:] restarts the whole group.", Control, false, true,
		func(in OptPathIn) (askSpec, error) {
			if in.Path == "" {
				return askSpec{verb: "apply", subject: "every changed config"}, nil
			}
			return askSpec{verb: "apply", subject: filepath.Base(in.Path)}, nil
		},
		func(t Target, c string, in OptPathIn) (any, error) { return t.Apply(c, in) }),

	change("save_config", "Save a config", "Replaces a config file's text when it still has the hash read_config gave. Visor checks it, keeps a backup, writes it and applies that file. Lines left as read_config showed them, «hidden» values included, keep their real values. Refused when the checks find errors, the file changed since it was read, or a hidden line was changed.", Configs, true, false,
		func(in SaveIn) (askSpec, error) {
			if strings.TrimSpace(in.Path) == "" || in.Hash == "" {
				return askSpec{}, errors.New("path and hash are required: call read_config first")
			}
			return askSpec{verb: "save", subject: filepath.Base(in.Path)}, nil
		},
		func(t Target, c string, in SaveIn) (any, error) { return t.SaveConfig(c, in) }),
	change("create_config", "Create a config", "Writes a new config file into supervisord's include folder, checks it, and loads its programs. Refuses a file that already exists or a program name already in use.", Configs, false, false,
		func(in CreateIn) (askSpec, error) {
			if strings.TrimSpace(in.FileName) == "" {
				return askSpec{}, errors.New("file_name is empty: give a name such as emails.conf")
			}
			return askSpec{verb: "create", subject: filepath.Base(in.FileName)}, nil
		},
		func(t Target, c string, in CreateIn) (any, error) { return t.CreateConfig(c, in) }),
	change("remove_config", "Remove a config", "Removes a config file from supervisor: stops its processes and moves the file to Visor's backups, so restore_config can put it back.", Configs, true, false,
		onPath("remove"), func(t Target, c string, in PathIn) (any, error) { return t.RemoveConfig(c, in) }),
	change("restore_config", "Restore a config", "Puts back a backup of a config file (its id from list_backups), or a removed file, then checks and applies it.", Configs, false, false,
		func(in RestoreIn) (askSpec, error) {
			if strings.TrimSpace(in.Path) == "" {
				return askSpec{}, errors.New("path is empty: give the config file's full path")
			}
			return askSpec{verb: "restore", subject: filepath.Base(in.Path)}, nil
		},
		func(t Target, c string, in RestoreIn) (any, error) { return t.RestoreConfig(c, in) }),
}

// Tools is the tool table in the order the MCP page shows it.
var Tools = func() []ToolInfo {
	out := make([]ToolInfo, len(entries))
	for i, e := range entries {
		out[i] = e.info
		out[i].LevelName = e.info.Level.String()
	}
	return out
}()

// NewServer returns Visor's MCP server over b.
func NewServer(b Backend, version string) *Server {
	s := &Server{
		SDK: sdk.NewServer(&sdk.Implementation{Name: "visor", Title: "Visor", Version: version}, &sdk.ServerOptions{
			Instructions: "Visor manages supervisord on this computer and on saved servers. Call get_status first; list_servers names the servers. Changes may wait while the person answers in Visor; \"The person declined in Visor.\" means no, so don't retry it.",
		}),
		b:   b,
		lim: NewLimiter(time.Now),
	}
	f := false
	for _, e := range entries {
		ro := e.info.Level == Read
		d := e.info.Destructive
		e.register(s, &sdk.Tool{
			Name: e.info.Name, Title: e.title, Description: e.info.Description,
			Annotations: &sdk.ToolAnnotations{
				Title: e.title, ReadOnlyHint: ro, DestructiveHint: &d,
				IdempotentHint: e.idempotent, OpenWorldHint: &f,
			},
		})
	}
	return s
}

// Calls returns the recent calls, newest first.
func (s *Server) Calls() []Call { return s.calls.newest() }

// Handler is the HTTP handler for this server (see Handler).
func (s *Server) Handler(token func() string) http.Handler { return Handler(s.SDK, token) }

// ClientLabel is the name Visor shows for an MCP client's clientInfo name.
func ClientLabel(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "":
		return "An AI tool"
	case "claude-code", "claude code":
		return "Claude Code"
	case "codex", "codex-mcp-client", "codex_cli", "codex-cli":
		return "Codex"
	case "cursor", "cursor-vscode":
		return "Cursor"
	case "visual studio code", "vscode":
		return "VS Code"
	case "opencode":
		return "OpenCode"
	}
	// Any other name is the client's own: no control characters, at most 40.
	name = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, name))
	if r := []rune(name); len(r) > 40 {
		name = string(r[:40]) + "…"
	}
	if name == "" {
		return "An AI tool"
	}
	return name
}

func clientOf(req *sdk.CallToolRequest) string {
	if req != nil && req.Session != nil {
		if p := req.Session.InitializeParams(); p != nil && p.ClientInfo != nil {
			return ClientLabel(p.ClientInfo.Name)
		}
	}
	return ClientLabel("")
}

// refusal is an error that counts as "refused" in Recent calls.
type refusal struct{ msg string }

func (r *refusal) Error() string { return r.msg }

func refuse(format string, a ...any) error { return &refusal{fmt.Sprintf(format, a...)} }

func (s *Server) record(req *sdk.CallToolRequest, tool, machine, result string, err error) {
	c := Call{At: time.Now(), Client: clientOf(req), Tool: tool, Machine: machine, Result: result}
	if err != nil {
		c.Detail = err.Error()
	}
	s.calls.add(c)
	s.b.Recorded(c)
}

// machine finds the machine a tool is aimed at: this computer when name is
// empty, else a server by id or name. A server set to Off is invisible, so
// another of the same name is found past it.
func (s *Server) machine(name string) (Machine, error) {
	ms := s.b.Machines()
	name = strings.TrimSpace(name)
	for _, m := range ms {
		if name == "" && m.Local || name != "" && (m.ID == name || strings.EqualFold(m.Name, name)) {
			if !m.Local && m.Level == Off {
				continue
			}
			return m, nil
		}
	}
	return Machine{}, refuse("No server named %q. list_servers shows the ones Visor can use.", name)
}

// handle is every tool call after its input is read: find the machine,
// check its level, rate-limit, ask when needed, run, record.
func (s *Server) handle(ctx context.Context, req *sdk.CallToolRequest, tool string, need Level, server string, a *askSpec, run func(Target, string) (any, error)) (out any, err error) {
	// The SDK runs tools on goroutines nothing else recovers: a panic here
	// (opening a server, asking, the tool itself) would end the app.
	defer func() {
		if r := recover(); r != nil {
			out, err = nil, panicError(r)
			s.record(req, tool, server, ResultError, err)
		}
	}()
	client := clientOf(req)
	m, err := s.machine(server)
	if err == nil {
		err = s.allowed(m, need)
	}
	if err == nil {
		err = s.lim.Allow()
	}
	if err == nil && a != nil {
		err = s.ask(ctx, client, m, *a)
	}
	if err == nil && a != nil {
		if wait := s.lim.Change(m.ID); wait > 0 {
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				err = ctx.Err()
			}
		}
	}
	if err != nil {
		s.record(req, tool, m.Name, ResultRefused, err)
		return nil, err
	}
	t, err := s.b.Target(ctx, m)
	if err == nil {
		out, err = run(t, client)
	}
	if err != nil {
		s.record(req, tool, m.Name, ResultError, err)
		return nil, err
	}
	s.record(req, tool, m.Name, ResultOK, nil)
	return out, nil
}

// safeRun turns a panic in run into its error, for work on a goroutine of
// its own (withTimeout) or outside handle.
func safeRun(run func() (any, error)) (out any, err error) {
	defer func() {
		if r := recover(); r != nil {
			out, err = nil, panicError(r)
		}
	}()
	return run()
}

func panicError(r any) error {
	return fmt.Errorf("Visor hit an internal error (%v). Visor itself is still running.", r) //nolint:staticcheck // shown to the agent as a sentence
}

func (s *Server) allowed(m Machine, need Level) error {
	if eff, _ := Effective(m); eff >= need {
		return nil
	}
	if m.ReadOnly && need > Read && m.Level > Read {
		return refuse("%s is a Read only server, so MCP can only read it.", m.Name)
	}
	return refuse("%s is set to %s for MCP. Raise it on Visor's MCP page.", m.Name, m.Level.Label())
}

func (s *Server) ask(ctx context.Context, client string, m Machine, a askSpec) error {
	if _, asks := Effective(m); !asks && !a.force {
		return nil
	}
	q := Question{
		ID: fmt.Sprintf("ask-%d", s.seq.Add(1)), Client: client, Verb: a.verb, Subject: a.subject,
		Machine: m.Name, MachineID: m.ID, Production: m.Production(), Force: a.force,
	}
	ok, err := s.b.Ask(ctx, q)
	if err != nil {
		return refuse("Visor couldn't ask: %v", err)
	}
	if !ok {
		return ErrDeclined
	}
	return nil
}

// readTimeout is how long a read waits for its machine (spec §7: 20 s); the
// call goes on in the background and is dropped.
var readTimeout = 20 * time.Second

func withTimeout(d time.Duration, fn func() (any, error)) (any, error) {
	type result struct {
		out any
		err error
	}
	ch := make(chan result, 1)
	go func() {
		out, err := safeRun(fn)
		ch <- result{out, err}
	}()
	select {
	case r := <-ch:
		return r.out, r.err
	case <-time.After(d):
		return nil, fmt.Errorf("the machine didn't answer within %s: it may be slow, or its connection down", d)
	}
}

// serverView is one machine in list_servers.
type serverView struct {
	Name      string `json:"name"`
	ID        string `json:"id"`
	Tag       string `json:"tag,omitempty"`
	Local     bool   `json:"thisComputer,omitempty"`
	Connected bool   `json:"connected"`
	Access    string `json:"access"`
	Asks      bool   `json:"asksBeforeChanges"`
	ReadOnly  bool   `json:"readOnly,omitempty"`
}

func (s *Server) listServers() any {
	out := []serverView{}
	for _, m := range s.b.Machines() {
		l, asks := Effective(m)
		if l == Off {
			continue
		}
		out = append(out, serverView{
			Name: m.Name, ID: m.ID, Tag: m.Tag, Local: m.Local, Connected: m.Connected,
			Access: l.Label(), Asks: asks, ReadOnly: m.ReadOnly,
		})
	}
	return map[string]any{"servers": out}
}
