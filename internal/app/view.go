package app

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/touhidurabir/visor/internal/config"
	"github.com/touhidurabir/visor/internal/issues"
	"github.com/touhidurabir/visor/internal/monitor"
	"github.com/touhidurabir/visor/internal/supervisor"
	"github.com/touhidurabir/visor/internal/usage"
)

// NoConfigFile names the group of processes that no include file defines
// (e.g. its file was deleted but supervisord still runs it).
const NoConfigFile = "(no config file)"

// OverviewView is everything the dashboard shows for one server. It is
// sent as the visor:overview event after every poll and every action.
type OverviewView struct {
	// Host is the machine this tab shows: this computer or a server.
	Host    HostView
	Server  ServerView
	Stats   StatsView
	Configs []ConfigView
}

// ServerView describes the supervisord connection. Health is "ok",
// "warning", "error" or "unknown" (before the first poll).
type ServerView struct {
	Name       string
	Version    string
	UptimeMs   int64 // from the pidfile's mtime; 0 if unknown
	ConfigPath string
	Reachable  bool
	Error      string
	Health     string
}

// StatsView counts processes. Failing is FATAL, BACKOFF and EXITED with an
// unexpected status; Stopped is STOPPED and EXITED with an expected status.
type StatsView struct {
	Configs    int
	Processes  int
	Running    int
	Failing    int
	Stopped    int
	OpenIssues int
}

// ConfigView is one include file and the processes it defines. Path is ""
// for the NoConfigFile group. Changed means the file differs from what
// supervisord runs (or is new); Uncertain means Visor cannot tell.
type ConfigView struct {
	Path      string
	FileName  string
	Changed   bool
	Uncertain bool
	Watched   bool
	Summary   string
	Processes []ProcessView
	// NotLoaded are the file's programs supervisord doesn't know yet (the
	// file was added or extended outside Visor and nobody ran update):
	// Load adds them. Empty while supervisord can't be reached.
	NotLoaded []NotLoadedView
}

// NotLoadedView is a program in a config file that supervisord hasn't
// loaded, with the group it would be added as.
type NotLoadedView struct {
	Program string
	Group   string
}

// ProcessView is the frontend's view of one supervised process. Times are
// Unix milliseconds, 0 when supervisord reports none. Since is "up 6d 3h",
// "failing 2h 12m" or "stopped 5d" ("" when it does not apply); Detail is
// the spawn error, exit status or crash note; Program is the [program:x]
// name ("" when no file defines it). Status is "running", "failing" (FATAL,
// BACKOFF, or EXITED with an unexpected status), "stopped" (STOPPED, or
// EXITED as expected) or "other" (STARTING, STOPPING, UNKNOWN).
type ProcessView struct {
	Name        string
	Group       string
	FullName    string
	Program     string
	State       string
	Status      string
	Description string
	SpawnErr    string
	PID         int
	ExitStatus  int
	StartMs     int64
	StopMs      int64
	NowMs       int64
	Since       string
	Detail      string
	// CPU is a percentage of one core and MemBytes resident memory, each
	// with the process's children and -1 when unknown (not running, not
	// measurable, or CPU before a second sample). Procs is how many
	// processes they count; Spark is the last 10 minutes of CPU.
	CPU      float64
	MemBytes int64
	Procs    int
	Spark    []float64
}

// IssueView is one issue. Status is "open", "resolved_auto",
// "resolved_user" or "dismissed"; ClosedMs is 0 while open. Note says why
// Visor closed it, e.g. "program removed" ("" otherwise).
type IssueView struct {
	// Server is the tab the issue belongs to (set on events).
	Server   string
	ID       string
	Kind     string
	Process  string
	Config   string
	Message  string
	OpenedMs int64
	ClosedMs int64
	Status   string
	Notified bool
	Note     string
}

// TransitionView is one process state change.
type TransitionView struct {
	AtMs    int64
	Process string
	From    string
	To      string
	Note    string
}

// ApplyResultView is the outcome of applying config changes: the groups
// supervisord added, changed and removed, and any per-group errors.
type ApplyResultView struct {
	Added, Changed, Removed []string
	Errors                  []string
}

// ConfigDetailView is the config detail page: the config with its live
// processes, the paths it refers to, each process's log files and, when a
// process is failing, its last error.
type ConfigDetailView struct {
	Config    ConfigView
	Programs  int // [program:x] sections in the file
	Paths     []PathView
	Logs      []ProcessLogView
	LastError *LastErrorView // nil when nothing in the file is failing
	// Explain is the file on disk explained line by line; ExplainError
	// says why it could not be read.
	Explain      ExplainView
	ExplainError string
}

// PathView is one path a config refers to. Kind is "dir" (a working
// directory), "bin" (a command binary) or "log"; Exists is false when it
// is missing (a binary not found on PATH keeps its name as written).
type PathView struct {
	Label  string
	Path   string
	Kind   string
	Exists bool
}

// ProcessLogView is where one process's output goes. Stdout is "" when it
// has no stdout log; Stderr is the stdout log when stderr is redirected
// into it, and "" when stderr has no log.
type ProcessLogView struct {
	Process string // group:name
	Stdout  string
	Stderr  string
}

// LastErrorView is the most recent failure among a config's processes.
// Since is how long it has been failing ("2h 12m"); StderrTail is the end
// of its stderr (at most 20 lines); StderrLog is the file stderr goes to
// ("" when it has none). ExitStatus is supervisord's, which stays 0 when a
// process exits within startsecs (FATAL or BACKOFF after "Exited too
// quickly"), so Message only quotes it for EXITED.
//
// Line is the config line the failure comes from, when Visor can tell
// (see ExplainView), else 0.
type LastErrorView struct {
	Process    string
	Line       int
	State      string
	Since      string
	Message    string
	ExitStatus int
	StderrTail string
	StderrLog  string
}

func toProcessView(p supervisor.ProcessInfo) ProcessView {
	return ProcessView{
		Name:        p.Name,
		Group:       p.Group,
		FullName:    p.FullName(),
		State:       p.State.String(),
		Description: p.Description,
		SpawnErr:    p.SpawnErr,
		PID:         p.PID,
		ExitStatus:  p.ExitStatus,
		StartMs:     ms(p.Start),
		StopMs:      ms(p.Stop),
		NowMs:       ms(p.Now),
		CPU:         -1,
		MemBytes:    -1,
	}
}

func toIssueView(i issues.Issue) IssueView {
	return IssueView{
		ID:       i.ID,
		Kind:     string(i.Kind),
		Process:  i.Process,
		Config:   i.Config,
		Message:  i.Message,
		OpenedMs: ms(i.OpenedAt),
		ClosedMs: ms(i.ClosedAt),
		Status:   string(i.Status),
		Notified: i.Notified,
		Note:     i.Note,
	}
}

func toTransitionView(t issues.Transition) TransitionView {
	return TransitionView{AtMs: ms(t.At), Process: t.Process, From: t.From, To: t.To, Note: t.Note}
}

func ms(t time.Time) int64 {
	if t.IsZero() || t.UnixMilli() == 0 {
		return 0
	}
	return t.UnixMilli()
}

// overviewInput is what buildOverview needs; gathering it is Runtime's job.
type overviewInput struct {
	now        time.Time
	snap       monitor.Snapshot
	inv        config.Inventory
	changes    config.ChangeSet
	watched    func(path string) bool
	open       []issues.Issue // newest first
	version    string
	configPath string
	uptime     time.Duration
	err        error // no usable connection at all
	// failedPolls counts consecutive unreachable polls up to snap.
	failedPolls int
	host        HostView
	usage       *usage.History // nil: no figures
}

// buildOverview turns the latest snapshot, inventory and issues into the
// dashboard view. It is pure apart from calling in.watched.
func buildOverview(in overviewInput) OverviewView {
	ov := OverviewView{
		Host: in.host,
		Server: ServerView{
			Name:       "Local",
			Version:    in.version,
			ConfigPath: in.configPath,
			Reachable:  in.snap.Reachable,
			Health:     monitor.HealthOf(in.snap, in.inv.ExpectedExit).String(),
		},
		Configs: []ConfigView{},
	}
	switch {
	case in.err != nil:
		ov.Server.Error = in.err.Error()
		ov.Server.Health = monitor.HealthError.String()
	case !in.snap.Reachable && in.snap.Err != nil:
		ov.Server.Error = "cannot reach supervisord: " + in.snap.Err.Error()
	}
	// Unreachable is an error from the poll that opens the unreachable
	// issue; one failed poll alone is a warning.
	if in.err == nil && !in.snap.Reachable && !in.snap.At.IsZero() && in.failedPolls < issues.DefaultRules.UnreachablePolls {
		ov.Server.Health = monitor.HealthWarning.String()
	}
	if in.snap.Reachable && in.uptime > 0 {
		ov.Server.UptimeMs = in.uptime.Milliseconds()
	}

	openByProc := map[string][]issues.Issue{}
	for _, is := range in.open {
		if is.Process != "" {
			openByProc[is.Process] = append(openByProc[is.Process], is)
		}
		// A crash loop is an error. An open unexpected exit warns: the
		// process may be running again, but it crashed.
		switch {
		case ov.Server.Health == monitor.HealthUnknown.String():
		case is.Kind == issues.KindCrashLoop:
			ov.Server.Health = monitor.HealthError.String()
		case is.Kind == issues.KindUnexpectedExit && ov.Server.Health == monitor.HealthOK.String():
			ov.Server.Health = monitor.HealthWarning.String()
		}
	}

	changed := setOf(in.changes.Changed, in.changes.Added)
	uncertain := setOf(in.changes.Uncertain)
	byFile := map[string][]ProcessView{}
	classes := map[string]class{} // by full name
	var orphans []ProcessView
	for _, p := range in.snap.Processes {
		v := toProcessView(p)
		path, prog, ok := in.inv.Lookup(v.FullName)
		if !ok {
			path, ok = in.inv.FileForGroup(p.Group)
		}
		v.Program = prog.Name
		if in.usage != nil {
			cur := in.usage.Current(v.FullName)
			v.CPU, v.MemBytes = cur.CPU, cur.RSS
			v.Procs = in.usage.Summary(v.FullName).Procs
			v.Spark = in.usage.Spark(v.FullName, sparkPoints)
		}
		expected := expectedExit(in.inv, v.FullName, p.ExitStatus)
		v.Since, v.Detail = sinceAndDetail(p, expected, openByProc[v.FullName], in.now)

		ov.Stats.Processes++
		classes[v.FullName] = classify(p.State, expected)
		v.Status = classes[v.FullName].String()
		switch classes[v.FullName] {
		case classRunning:
			ov.Stats.Running++
		case classFailing:
			ov.Stats.Failing++
		case classStopped:
			ov.Stats.Stopped++
		}
		if ok {
			byFile[path] = append(byFile[path], v)
		} else {
			orphans = append(orphans, v)
		}
	}

	loadedGroups := map[string]bool{}
	for _, p := range in.snap.Processes {
		loadedGroups[p.Group] = true
	}
	for _, f := range in.inv.Files {
		procs := byFile[f.Path]
		if procs == nil {
			procs = []ProcessView{}
		}
		ov.Configs = append(ov.Configs, ConfigView{
			Path:      f.Path,
			FileName:  filepath.Base(f.Path),
			Changed:   changed[f.Path],
			Uncertain: uncertain[f.Path],
			Watched:   in.watched == nil || in.watched(f.Path),
			Summary:   summary(f, procs, classes, in.snap.Reachable),
			Processes: procs,
			NotLoaded: notLoaded(f, loadedGroups, in.snap.Reachable),
		})
	}
	if len(orphans) > 0 {
		ov.Configs = append(ov.Configs, ConfigView{
			FileName:  NoConfigFile,
			Summary:   countSummary(orphans, classes),
			Processes: orphans,
			NotLoaded: []NotLoadedView{},
		})
	}
	ov.Stats.Configs = len(in.inv.Files)
	ov.Stats.OpenIssues = len(in.open)
	return ov
}

type class int

const (
	classOther class = iota
	classRunning
	classFailing
	classStopped
)

func (c class) String() string {
	switch c {
	case classRunning:
		return "running"
	case classFailing:
		return "failing"
	case classStopped:
		return "stopped"
	}
	return "other"
}

func classify(st supervisor.ProcessState, expectedExit bool) class {
	switch st {
	case supervisor.StateRunning:
		return classRunning
	case supervisor.StateFatal, supervisor.StateBackoff:
		return classFailing
	case supervisor.StateStopped:
		return classStopped
	case supervisor.StateExited:
		if expectedExit {
			return classStopped
		}
		return classFailing
	}
	return classOther
}

func expectedExit(inv config.Inventory, fullName string, status int) bool {
	return slices.Contains(inv.ExpectedExit(fullName), status)
}

// sinceAndDetail labels how long a process has been in its condition and
// why. A failing process counts from its latest open fatal or unexpected
// exit issue (the current failure), else from when it last stopped.
func sinceAndDetail(p supervisor.ProcessInfo, expected bool, open []issues.Issue, now time.Time) (since, detail string) {
	daemonNow := p.Now
	if daemonNow.IsZero() {
		daemonNow = now
	}
	switch classify(p.State, expected) {
	case classRunning:
		if !p.Start.IsZero() {
			since = sinceLabel("up", daemonNow.Sub(p.Start))
		}
	case classFailing:
		var latest time.Time
		for _, is := range open {
			if (is.Kind == issues.KindFatal || is.Kind == issues.KindUnexpectedExit) && is.OpenedAt.After(latest) {
				latest = is.OpenedAt
			}
		}
		switch {
		case !latest.IsZero():
			since = sinceLabel("failing", now.Sub(latest))
		case !p.Stop.IsZero():
			since = sinceLabel("failing", daemonNow.Sub(p.Stop))
		case !p.Start.IsZero():
			since = sinceLabel("failing", daemonNow.Sub(p.Start))
		}
	case classStopped:
		if !p.Stop.IsZero() {
			since = sinceLabel("stopped", daemonNow.Sub(p.Stop))
		}
	}

	var parts []string
	failed := p.State == supervisor.StateFatal || p.State == supervisor.StateBackoff
	switch {
	case failed && p.SpawnErr != "": // spawnerr is stale in other states
		parts = append(parts, p.SpawnErr)
	case p.State == supervisor.StateFatal:
		parts = append(parts, "exited too quickly")
	case p.State == supervisor.StateExited && expected:
		parts = append(parts, fmt.Sprintf("exited %d", p.ExitStatus))
	case p.State == supervisor.StateExited:
		parts = append(parts, fmt.Sprintf("exited %d (unexpected)", p.ExitStatus))
	}
	for _, is := range open {
		if is.Kind == issues.KindCrashLoop {
			parts = append(parts, lowerFirst(is.Message))
		}
	}
	return since, strings.Join(parts, " · ")
}

// sinceLabel is prefix plus the duration in its two largest units, dropping
// a zero second unit and seconds once it reaches a minute: "up 59s",
// "up 41m", "failing 3h 2m", "up 6d 3h", "stopped 5d".
func sinceLabel(prefix string, d time.Duration) string {
	d = max(d, 0)
	days := int(d / (24 * time.Hour))
	hours := int(d/time.Hour) % 24
	mins := int(d/time.Minute) % 60
	secs := int(d/time.Second) % 60
	var s string
	switch {
	case days > 0:
		s = pair(days, "d", hours, "h")
	case hours > 0:
		s = pair(hours, "h", mins, "m")
	case mins > 0:
		s = fmt.Sprintf("%dm", mins)
	default:
		s = fmt.Sprintf("%ds", secs)
	}
	return prefix + " " + s
}

func pair(a int, au string, b int, bu string) string {
	if b == 0 {
		return fmt.Sprintf("%d%s", a, au)
	}
	return fmt.Sprintf("%d%s %d%s", a, au, b, bu)
}

// summary is "2 programs · 5 of 5 running", "2 programs · 2 running ·
// 1 failing", or "1 program · not loaded" when supervisord runs none of it.
// Without a reachable snapshot it is only the program count.
func summary(f config.ConfigFile, procs []ProcessView, classes map[string]class, reachable bool) string {
	if f.Err != nil {
		return "Cannot read this file: " + f.Err.Error()
	}
	head := plural(len(f.Programs), "program", "programs")
	if !reachable {
		return head
	}
	if len(procs) == 0 {
		if len(f.Programs) == 0 {
			return head
		}
		return head + " · not loaded"
	}
	return head + " · " + countSummary(procs, classes)
}

// notLoaded lists f's programs whose group supervisord doesn't run (none
// while it can't be reached, or f can't be read).
func notLoaded(f config.ConfigFile, loaded map[string]bool, reachable bool) []NotLoadedView {
	out := []NotLoadedView{}
	if !reachable || f.Err != nil {
		return out
	}
	for _, p := range f.Programs {
		if !loaded[p.Group] {
			out = append(out, NotLoadedView{Program: p.Name, Group: p.Group})
		}
	}
	return out
}

func countSummary(procs []ProcessView, classes map[string]class) string {
	var running, failing, stopped int
	for _, p := range procs {
		switch classes[p.FullName] {
		case classRunning:
			running++
		case classFailing:
			failing++
		case classStopped:
			stopped++
		}
	}
	if running == len(procs) {
		return fmt.Sprintf("%d of %d running", running, len(procs))
	}
	var parts []string
	for _, c := range []struct {
		n    int
		what string
	}{{running, "running"}, {failing, "failing"}, {stopped, "stopped"}} {
		if c.n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", c.n, c.what))
		}
	}
	if len(parts) == 0 {
		return plural(len(procs), "process", "processes")
	}
	return strings.Join(parts, " · ")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

func lowerFirst(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return s
	}
	return string(unicode.ToLower(r)) + s[n:]
}

func setOf(lists ...[]string) map[string]bool {
	m := map[string]bool{}
	for _, l := range lists {
		for _, s := range l {
			m[s] = true
		}
	}
	return m
}
