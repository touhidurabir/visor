package mcp

import (
	"context"
	"time"
)

// Backend is Visor as the MCP server sees it.
type Backend interface {
	// Machines lists this computer first, then every saved server, with the
	// levels set on the MCP page. It is read on every call.
	Machines() []Machine
	// Target returns m's tab, opening a saved server's tab when needed.
	Target(ctx context.Context, m Machine) (Target, error)
	// Ask shows q in Visor and waits for the person (at most a minute; no
	// answer is false).
	Ask(ctx context.Context, q Question) (bool, error)
	// AllowEnv is the "Let AI read environment values" setting.
	AllowEnv() bool
	// Recorded is told of every call, for the MCP page's Recent calls.
	Recorded(c Call)
}

// Question is a change waiting for the person: "Claude Code wants to
// restart horizon on prod".
type Question struct {
	ID         string `json:"id"`
	Client     string `json:"client"`
	Verb       string `json:"verb"`
	Subject    string `json:"subject"`
	Machine    string `json:"machine"`
	MachineID  string `json:"machineId"`
	Production bool   `json:"production"`
	// Force: it asks even where asking is off (KILL), so "Allow for 10
	// minutes" doesn't cover it.
	Force bool `json:"force"`
}

// Call is one tool call, for Recent calls.
type Call struct {
	At      time.Time `json:"at"`
	Client  string    `json:"client"`
	Tool    string    `json:"tool"`
	Machine string    `json:"machine"`
	// Result is "ok", "refused" (access, asking, rate limit) or "error".
	Result string `json:"result"`
	Detail string `json:"detail,omitempty"`
}

// Call results.
const (
	ResultOK      = "ok"
	ResultRefused = "refused"
	ResultError   = "error"
)

// Target is one machine's supervisord, through its tab. Every result is a
// JSON object. Changes take client, the caller's label, for Activity.
type Target interface {
	Status() (any, error)
	Processes(ProcessesIn) (any, error)
	Process(ProcessIn) (any, error)
	Logs(LogsIn) (any, error)
	SearchLogs(SearchIn) (any, error)
	SupervisordLog(SupervisordLogIn) (any, error)
	Issues(IssuesIn) (any, error)
	Activity(ActivityIn) (any, error)
	Configs() (any, error)
	ReadConfig(in PathIn, env bool) (any, error)
	CheckConfig(in CheckIn, env bool) (any, error)
	PendingChanges(OptPathIn) (any, error)
	DraftProgram(DraftIn) (any, error)
	Backups(PathIn) (any, error)
	Diagnose() (any, error)

	Start(client string, in TargetIn) (any, error)
	Stop(client string, in TargetIn) (any, error)
	Restart(client string, in TargetIn) (any, error)
	Signal(client string, in SignalIn) (any, error)
	Apply(client string, in OptPathIn) (any, error)

	SaveConfig(client string, in SaveIn) (any, error)
	CreateConfig(client string, in CreateIn) (any, error)
	RemoveConfig(client string, in PathIn) (any, error)
	RestoreConfig(client string, in RestoreIn) (any, error)
}

// Every input has an optional server. The jsonschema tags are the
// descriptions agents see.

// ServerIn is a tool that takes nothing but the machine.
type ServerIn struct {
	Server string `json:"server,omitempty" jsonschema:"the server's name or id from list_servers; leave it out for this computer"`
}

type ProcessesIn struct {
	Server string `json:"server,omitempty" jsonschema:"the server's name or id from list_servers; leave it out for this computer"`
	State  string `json:"state,omitempty" jsonschema:"only processes in this state: RUNNING, STARTING, BACKOFF, STOPPED, EXITED, FATAL, STOPPING or UNKNOWN"`
	Config string `json:"config,omitempty" jsonschema:"only processes from this config file (its path or file name)"`
	Name   string `json:"name,omitempty" jsonschema:"only processes whose name contains this text"`
}

type ProcessIn struct {
	Server string `json:"server,omitempty" jsonschema:"the server's name or id from list_servers; leave it out for this computer"`
	Name   string `json:"name" jsonschema:"the process as supervisord names it: group:name, or name when the group has the same name"`
}

type LogsIn struct {
	Server     string `json:"server,omitempty" jsonschema:"the server's name or id from list_servers; leave it out for this computer"`
	Process    string `json:"process" jsonschema:"the process: group:name, or name"`
	Stream     string `json:"stream,omitempty" jsonschema:"stdout (the default) or stderr"`
	Lines      int    `json:"lines,omitempty" jsonschema:"how many lines from the end, 100 by default, at most 500"`
	Before     int    `json:"before,omitempty" jsonschema:"the cursor from a previous call, to read the lines before it"`
	ErrorsOnly bool   `json:"errors_only,omitempty" jsonschema:"only error lines and the stack traces under them"`
}

type SearchIn struct {
	Server    string `json:"server,omitempty" jsonschema:"the server's name or id from list_servers; leave it out for this computer"`
	Process   string `json:"process" jsonschema:"the process: group:name, or name"`
	Stream    string `json:"stream,omitempty" jsonschema:"stdout (the default) or stderr"`
	Query     string `json:"query" jsonschema:"the text to find, ignoring case"`
	Limit     int    `json:"limit,omitempty" jsonschema:"at most this many matches, 50 by default, at most 200"`
	Before    int    `json:"before,omitempty" jsonschema:"the cursor from a previous call, to search further back"`
	WholeFile bool   `json:"whole_file,omitempty" jsonschema:"search the whole log file, not only its last megabyte; only when Visor's Settings allow it"`
}

type SupervisordLogIn struct {
	Server string `json:"server,omitempty" jsonschema:"the server's name or id from list_servers; leave it out for this computer"`
	Lines  int    `json:"lines,omitempty" jsonschema:"how many lines from the end, 100 by default, at most 500"`
}

type IssuesIn struct {
	Server   string `json:"server,omitempty" jsonschema:"the server's name or id from list_servers; leave it out for this computer"`
	Resolved bool   `json:"resolved,omitempty" jsonschema:"also list resolved issues"`
	Process  string `json:"process,omitempty" jsonschema:"only issues of this process"`
}

type ActivityIn struct {
	Server  string `json:"server,omitempty" jsonschema:"the server's name or id from list_servers; leave it out for this computer"`
	Since   string `json:"since,omitempty" jsonschema:"only events after this: an RFC 3339 time, or a duration back from now such as 2h or 30m"`
	Process string `json:"process,omitempty" jsonschema:"only events of this process"`
	Kind    string `json:"kind,omitempty" jsonschema:"only processes, programs, files or daemon events"`
	Limit   int    `json:"limit,omitempty" jsonschema:"at most this many events, 50 by default, at most 200"`
}

type PathIn struct {
	Server string `json:"server,omitempty" jsonschema:"the server's name or id from list_servers; leave it out for this computer"`
	Path   string `json:"path" jsonschema:"the config file's full path, as list_configs shows it"`
}

type OptPathIn struct {
	Server string `json:"server,omitempty" jsonschema:"the server's name or id from list_servers; leave it out for this computer"`
	Path   string `json:"path,omitempty" jsonschema:"one config file's full path; leave it out for every changed file"`
}

type CheckIn struct {
	Server   string `json:"server,omitempty" jsonschema:"the server's name or id from list_servers; leave it out for this computer"`
	Path     string `json:"path,omitempty" jsonschema:"a config file to check, as list_configs shows it"`
	Content  string `json:"content,omitempty" jsonschema:"draft text to check instead of a file"`
	FileName string `json:"file_name,omitempty" jsonschema:"the file name the draft would have, such as emails.conf"`
}

type DraftIn struct {
	Server   string            `json:"server,omitempty" jsonschema:"the server's name or id from list_servers; leave it out for this computer"`
	Template string            `json:"template,omitempty" jsonschema:"queue (Laravel queue worker), horizon, scheduler, node or script; leave it out for plain values"`
	Values   map[string]string `json:"values,omitempty" jsonschema:"program settings, such as name, command, directory, user, numprocs; they replace the template's"`
}

type TargetIn struct {
	Server string `json:"server,omitempty" jsonschema:"the server's name or id from list_servers; leave it out for this computer"`
	Target string `json:"target" jsonschema:"a process (group:name or name), a whole group (group:*), or a config file's full path for all its processes"`
}

type SignalIn struct {
	Server string `json:"server,omitempty" jsonschema:"the server's name or id from list_servers; leave it out for this computer"`
	Name   string `json:"name" jsonschema:"the process: group:name, or name"`
	Signal string `json:"signal" jsonschema:"HUP, USR1, USR2, TERM, INT, QUIT or KILL"`
}

type SaveIn struct {
	Server  string `json:"server,omitempty" jsonschema:"the server's name or id from list_servers; leave it out for this computer"`
	Path    string `json:"path" jsonschema:"the config file's full path"`
	Content string `json:"content" jsonschema:"the whole new text of the file"`
	Hash    string `json:"hash" jsonschema:"the hash read_config returned; the save is refused when the file changed since"`
}

type CreateIn struct {
	Server   string `json:"server,omitempty" jsonschema:"the server's name or id from list_servers; leave it out for this computer"`
	FileName string `json:"file_name" jsonschema:"the new file's name in the include folder, such as emails.conf"`
	Content  string `json:"content" jsonschema:"the file's text, for example from draft_program"`
}

type RestoreIn struct {
	Server string `json:"server,omitempty" jsonschema:"the server's name or id from list_servers; leave it out for this computer"`
	Path   string `json:"path" jsonschema:"the config file's full path"`
	Backup string `json:"backup,omitempty" jsonschema:"the backup id from list_backups; leave it out to put back a removed file"`
}

func (i ServerIn) server() string         { return i.Server }
func (i ProcessesIn) server() string      { return i.Server }
func (i ProcessIn) server() string        { return i.Server }
func (i LogsIn) server() string           { return i.Server }
func (i SearchIn) server() string         { return i.Server }
func (i SupervisordLogIn) server() string { return i.Server }
func (i IssuesIn) server() string         { return i.Server }
func (i ActivityIn) server() string       { return i.Server }
func (i PathIn) server() string           { return i.Server }
func (i OptPathIn) server() string        { return i.Server }
func (i CheckIn) server() string          { return i.Server }
func (i DraftIn) server() string          { return i.Server }
func (i TargetIn) server() string         { return i.Server }
func (i SignalIn) server() string         { return i.Server }
func (i SaveIn) server() string           { return i.Server }
func (i CreateIn) server() string         { return i.Server }
func (i RestoreIn) server() string        { return i.Server }
