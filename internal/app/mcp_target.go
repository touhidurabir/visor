package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/touhidurabir/visor/internal/config"
	"github.com/touhidurabir/visor/internal/logs"
	"github.com/touhidurabir/visor/internal/mcp"
	"github.com/touhidurabir/visor/internal/supervisor"
	"github.com/touhidurabir/visor/internal/validate"
)

// MCP output limits (spec §3).
const (
	mcpLines      = 100
	mcpMaxLines   = 500
	mcpMaxBytes   = 64 * 1024
	mcpReadWindow = 256 * 1024 // bytes read back to find the lines asked for
	mcpSearchMax  = 200
	mcpWholePages = 32 // a whole-file search reads at most 32 MiB (it downloads it on a server)
	mcpEvents     = 50
)

// tabTarget is mcp.Target over one tab's services.
type tabTarget struct {
	t        *Tab
	name     string              // the machine's name, for errors
	diagnose func() (any, error) // Doctor on this computer, the server report on a server
	env      bool                // "Let AI read environment values", for get_process
}

var _ mcp.Target = (*tabTarget)(nil)

// live fails at once while a server tab is reconnecting, instead of
// waiting on a dead link.
func (g *tabTarget) live() error {
	if g.t.RT.Remote() && g.t.RT.Overview().Host.Conn == ConnReconnecting {
		return fmt.Errorf("%s is disconnected; Visor is reconnecting", g.name)
	}
	return nil
}

// fullName is a process as supervisord's group:name.
func fullName(name string) string {
	if strings.Contains(name, ":") {
		return name
	}
	return name + ":" + name
}

type mcpProcess struct {
	Name        string `json:"name"`
	Group       string `json:"group"`
	State       string `json:"state"`
	PID         int    `json:"pid,omitempty"`
	Since       string `json:"since,omitempty"`
	ExitStatus  int    `json:"exitStatus,omitempty"`
	Description string `json:"description,omitempty"`
	Detail      string `json:"detail,omitempty"`
	Config      string `json:"config,omitempty"`
}

func toMCPProcess(p ProcessView, conf string) mcpProcess {
	return mcpProcess{
		Name: p.FullName, Group: p.Group, State: p.State, PID: p.PID, Since: p.Since,
		ExitStatus: p.ExitStatus, Description: p.Description, Detail: p.Detail, Config: conf,
	}
}

func (g *tabTarget) Status() (any, error) {
	if err := g.live(); err != nil {
		return nil, err
	}
	ov := g.t.Server.Overview()
	out := map[string]any{"machine": g.name, "supervisord": ov.Server, "counts": ov.Stats}
	fail, changed, notLoaded := []mcpProcess{}, []string{}, []string{}
	for _, c := range ov.Configs {
		for _, p := range c.Processes {
			if p.Status == classFailing.String() { // as the dashboard counts it: an expected exit isn't failing
				fail = append(fail, toMCPProcess(p, c.Path))
			}
		}
		if c.Changed {
			changed = append(changed, c.Path)
		}
		for _, n := range c.NotLoaded {
			notLoaded = append(notLoaded, n.Program)
		}
	}
	out["failing"], out["changedOnDisk"] = fail, changed
	if m, ok := g.t.RT.machineSize(); ok {
		size := map[string]any{"cpuCores": m.Cores}
		if m.MemBytes > 0 {
			size["memoryBytes"] = m.MemBytes
		}
		out["machineSize"] = size
	}
	if len(notLoaded) > 0 {
		out["notLoaded"] = notLoaded
	}
	return out, nil
}

func (g *tabTarget) Processes(in mcp.ProcessesIn) (any, error) {
	if err := g.live(); err != nil {
		return nil, err
	}
	out := []mcpProcess{}
	for _, c := range g.t.Server.Overview().Configs {
		if in.Config != "" && c.Path != in.Config && c.FileName != in.Config {
			continue
		}
		for _, p := range c.Processes {
			if in.State != "" && !strings.EqualFold(p.State, in.State) ||
				in.Name != "" && !strings.Contains(strings.ToLower(p.FullName), strings.ToLower(in.Name)) {
				continue
			}
			out = append(out, toMCPProcess(p, c.Path))
		}
	}
	return map[string]any{"processes": out}, nil
}

// find returns a process and its config from the overview.
func (g *tabTarget) find(name string) (ProcessView, ConfigView, error) {
	full := fullName(name)
	for _, c := range g.t.Server.Overview().Configs {
		for _, p := range c.Processes {
			if p.FullName == full {
				return p, c, nil
			}
		}
	}
	return ProcessView{}, ConfigView{}, fmt.Errorf("no process named %q on %s: list_processes shows them", name, g.name)
}

// Usage is each process's CPU and memory now and over the hour; null for
// an unknown value.
func (g *tabTarget) Usage(in mcp.UsageIn) (any, error) {
	if err := g.live(); err != nil {
		return nil, err
	}
	var procs []ProcessView
	if in.Process != "" {
		p, _, err := g.find(in.Process)
		if err != nil {
			return nil, err
		}
		procs = []ProcessView{p}
	} else {
		for _, c := range g.t.Server.Overview().Configs {
			procs = append(procs, c.Processes...)
		}
	}
	known := func(v float64) any {
		if v < 0 {
			return nil
		}
		return v
	}
	out := make([]map[string]any, 0, len(procs))
	for _, p := range procs {
		row := map[string]any{"name": p.FullName, "state": p.State, "cpu_percent": known(p.CPU), "memory_bytes": known(float64(p.MemBytes)), "processes": nil, "hour": nil}
		if p.MemBytes >= 0 {
			row["processes"] = p.Procs
		}
		if s := g.t.RT.usage.Summary(p.FullName); s.PeakRSS > 0 {
			row["hour"] = map[string]any{"peak_cpu_percent": s.PeakCPU, "avg_cpu_percent": s.AvgCPU, "peak_memory_bytes": s.PeakRSS, "avg_memory_bytes": s.AvgRSS}
		}
		out = append(out, row)
	}
	return map[string]any{"processes": out, "sample_every_s": usageEvery.Seconds()}, nil
}

func (g *tabTarget) Process(in mcp.ProcessIn) (any, error) {
	if err := g.live(); err != nil {
		return nil, err
	}
	p, c, err := g.find(in.Name)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"process": toMCPProcess(p, c.Path)}
	if prog, ok := g.t.RT.program(p.FullName); ok {
		settings := map[string]string{}
		for _, k := range prog.Section.Keys {
			v := k.Value
			if k.Name == "environment" && !g.allowEnv() {
				v = validate.HideEnvValue(v)
			}
			settings[k.Name] = v
		}
		out["settings"] = settings
	}
	if hist, err := g.t.Issue.History(p.FullName, 20); err == nil {
		out["history"] = hist
	}
	if list, err := g.t.Issue.List(IssueQuery{Status: "open"}); err == nil {
		mine := []IssueView{}
		for _, is := range list {
			if is.Process == p.FullName {
				mine = append(mine, is)
			}
		}
		out["openIssues"] = mine
	}
	if c.Path != "" {
		if d, err := g.t.Server.ConfigDetail(c.Path); err == nil && d.LastError != nil && d.LastError.Process == p.FullName {
			out["lastError"] = d.LastError
		}
	}
	return out, nil
}

func (g *tabTarget) allowEnv() bool { return g.env }

// streamName is stdout when the agent names none.
func streamName(s string) string {
	if s == "" {
		return string(logs.Stdout)
	}
	return s
}

func (g *tabTarget) Logs(in mcp.LogsIn) (any, error) {
	if err := g.live(); err != nil {
		return nil, err
	}
	st, err := g.t.Log.checkStream(fullName(in.Process), streamName(in.Stream))
	if err != nil {
		return nil, err
	}
	n := in.Lines
	if n <= 0 {
		n = mcpLines
	}
	n = min(n, mcpMaxLines)
	before := in.Before
	if before <= 0 {
		before = -1
	}
	ac, err := g.t.RT.conn()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ac.ctx, searchTimeout)
	defer cancel()
	text, start, err := logs.ReadBack(ctx, ac.client, fullName(in.Process), st, before, mcpReadWindow)
	if err != nil {
		return nil, err
	}
	// Whole lines with their offsets; errors only keeps the error lines.
	lines := strings.SplitAfter(text, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	offs := make([]int, len(lines))
	for i, off := 0, 0; i < len(lines); i++ {
		offs[i], off = off, off+len(lines[i])
	}
	if in.ErrorsOnly {
		mask := logs.ErrorMask(lines)
		var kl []string
		var ko []int
		for i, keep := range mask {
			if keep {
				kl, ko = append(kl, lines[i]), append(ko, offs[i])
			}
		}
		lines, offs = kl, ko
	}
	// The last n lines, at most 64 KB; the cursor is where the first one
	// starts in the log (the window's start when none is kept).
	first := max(len(lines)-n, 0)
	kept, cut := logs.CutFront(strings.Join(lines[first:], ""), mcpMaxBytes)
	cursor := start
	if first < len(lines) {
		dropped := len(strings.Join(lines[first:], "")) - len(kept)
		for first < len(lines)-1 && dropped >= len(lines[first]) {
			dropped -= len(lines[first])
			first++
		}
		cursor = start + offs[first]
	}
	out := map[string]any{"process": fullName(in.Process), "stream": string(st), "text": kept}
	if cursor == 0 {
		out["atStart"] = true
	} else {
		out["cursor"] = cursor
	}
	if cut {
		out["cut"] = true
		out["note"] = "Cut at 64 KB: pass cursor as before for the lines before."
	}
	return out, nil
}

func (g *tabTarget) SearchLogs(in mcp.SearchIn) (any, error) {
	if err := g.live(); err != nil {
		return nil, err
	}
	if in.WholeFile && !g.t.RT.Settings().SearchWholeLog {
		return nil, errors.New(`whole-file searches are off: turn on "Search the whole log file" in Visor's Settings, or search the last megabyte`)
	}
	if strings.TrimSpace(in.Query) == "" {
		return nil, errors.New("query is empty")
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 50
	}
	limit = min(limit, mcpSearchMax)
	before := in.Before
	if before <= 0 {
		before = -1
	}
	out := LogSearchView{}
	for page := 0; ; page++ {
		v, err := g.t.Log.Search(fullName(in.Process), streamName(in.Stream), in.Query, before, limit-len(out.Lines))
		if err != nil {
			return nil, err
		}
		out.Lines = append(out.Lines, v.Lines...)
		out.Next, out.Capped = v.Next, v.Capped
		if !in.WholeFile || v.Next == 0 || v.Capped || len(out.Lines) >= limit || page+1 == mcpWholePages {
			break
		}
		before = v.Next
	}
	res := map[string]any{"matches": out.Lines, "cursor": out.Next}
	if out.Next > 0 {
		res["note"] = "Older lines weren't searched: pass cursor as before to go on."
	}
	return res, nil
}

func (g *tabTarget) SupervisordLog(in mcp.SupervisordLogIn) (any, error) {
	if err := g.live(); err != nil {
		return nil, err
	}
	n := in.Lines
	if n <= 0 {
		n = mcpLines
	}
	res, err := g.t.Server.MainLogLines(MainLogQuery{Limit: min(n, mcpMaxLines)})
	if err != nil {
		return nil, err
	}
	return map[string]any{"lines": res.Lines, "fromFile": res.FromFile}, nil
}

func (g *tabTarget) Issues(in mcp.IssuesIn) (any, error) {
	status := "open"
	if in.Resolved {
		status = ""
	}
	list, err := g.t.Issue.List(IssueQuery{Status: status, Limit: 100})
	if err != nil {
		return nil, err
	}
	if in.Process != "" {
		list = slices.DeleteFunc(list, func(is IssueView) bool { return is.Process != fullName(in.Process) })
	}
	stats, _ := g.t.Issue.Stats()
	return map[string]any{"issues": list, "counts": stats}, nil
}

// since reads an RFC 3339 time or a duration back from now.
func since(s string, now time.Time) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	if d, err := time.ParseDuration(s); err == nil {
		return now.Add(-d), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("since %q: use an RFC 3339 time or a duration such as 2h", s)
}

func (g *tabTarget) Activity(in mcp.ActivityIn) (any, error) {
	from, err := since(in.Since, time.Now())
	if err != nil {
		return nil, err
	}
	limit := in.Limit
	if limit <= 0 {
		limit = mcpEvents
	}
	q := ActivityQuery{Kind: in.Kind, Limit: min(limit, mcpSearchMax)}
	if in.Process != "" {
		q.Process = fullName(in.Process)
	}
	events := []map[string]any{}
	for _, e := range g.t.Issue.Activity(q).Events {
		if !from.IsZero() && e.AtMs < from.UnixMilli() {
			continue
		}
		by := "outside Visor"
		switch {
		case e.Via != "":
			by = "via " + e.Via
		case e.ByVisor:
			by = "by Visor"
		}
		events = append(events, map[string]any{
			"at": time.UnixMilli(e.AtMs).Format(time.RFC3339), "kind": e.Kind, "text": e.Text,
			"process": e.Process, "config": e.Config, "by": by,
		})
	}
	return map[string]any{"events": events}, nil
}

func (g *tabTarget) Configs() (any, error) {
	if err := g.live(); err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for _, c := range g.t.Server.Overview().Configs {
		if c.Path == "" {
			continue
		}
		procs := []string{}
		for _, p := range c.Processes {
			procs = append(procs, p.FullName)
		}
		notLoaded := []string{}
		for _, n := range c.NotLoaded {
			notLoaded = append(notLoaded, n.Program)
		}
		out = append(out, map[string]any{
			"path": c.Path, "processes": procs, "changedOnDisk": c.Changed, "uncertain": c.Uncertain,
			"watched": c.Watched, "notLoaded": notLoaded, "summary": c.Summary,
		})
	}
	f := g.t.Config.Folder()
	return map[string]any{"configs": out, "includeFolder": f.Dir, "folderWritable": f.Writable}, nil
}

func (g *tabTarget) ReadConfig(in mcp.PathIn, env bool) (any, error) {
	if err := g.live(); err != nil {
		return nil, err
	}
	f, err := g.t.Config.Read(in.Path)
	if err != nil {
		return nil, err
	}
	if !env {
		f.Content = validate.HideEnv(f.Content)
	}
	return map[string]any{"path": f.Path, "content": f.Content, "hash": f.Hash, "writable": f.Writable}, nil
}

func (g *tabTarget) CheckConfig(in mcp.CheckIn, env bool) (any, error) {
	if err := g.live(); err != nil {
		return nil, err
	}
	content, name, self := in.Content, in.FileName, ""
	if in.Path != "" && content == "" {
		f, err := g.t.Config.Read(in.Path)
		if err != nil {
			return nil, err
		}
		content, name, self = f.Content, filepath.Base(in.Path), in.Path
		if !env {
			content = validate.HideEnv(content)
		}
	}
	if content == "" {
		return nil, errors.New("give a path or draft content to check")
	}
	if name == "" {
		name = "draft" + g.t.Config.IncludeExt()
	}
	return g.t.Config.Explain(name, content, self), nil
}

func (g *tabTarget) PendingChanges(in mcp.OptPathIn) (any, error) {
	if err := g.live(); err != nil {
		return nil, err
	}
	if g.t.RT.CheckAction() != nil {
		// A Read only server is never asked anything (confirm.go): answer
		// from the files Visor already found changed.
		changed := []string{}
		for _, c := range g.t.Server.Overview().Configs {
			if c.Changed {
				changed = append(changed, c.Path)
			}
		}
		return map[string]any{
			"changedOnDisk": changed,
			"note":          "This server is set to Read only, so Visor didn't ask supervisord to re-read its configs: these files differ from what it runs.",
		}, nil
	}
	var diff supervisor.ConfigDiff
	g.t.RT.applyMu.Lock() // not in the middle of an apply
	defer g.t.RT.applyMu.Unlock()
	err := g.t.RT.call(expectation{}, func(ctx context.Context, c *supervisor.Client) error {
		var err error
		diff, err = c.ReloadConfig(ctx)
		return wrap("check for changes", err)
	})
	if err != nil {
		return nil, err
	}
	if in.Path != "" {
		groups, err := g.t.RT.groupsInFile(in.Path)
		if err != nil {
			return nil, err
		}
		keep := func(list []string) []string {
			return slices.DeleteFunc(slices.Clone(list), func(gr string) bool { return !slices.Contains(groups, gr) })
		}
		diff = supervisor.ConfigDiff{Added: keep(diff.Added), Changed: keep(diff.Changed), Removed: keep(diff.Removed)}
	}
	return map[string]any{
		"add": nonNil(diff.Added), "restart": nonNil(diff.Changed), "remove": nonNil(diff.Removed),
		"note": "Applying restarts every process of a changed group, as supervisorctl update does.",
	}, nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func (g *tabTarget) DraftProgram(in mcp.DraftIn) (any, error) {
	values := map[string]string{}
	if in.Template != "" {
		i := slices.IndexFunc(config.Templates(), func(t config.Template) bool { return t.ID == in.Template })
		if i < 0 {
			var ids []string
			for _, t := range config.Templates() {
				ids = append(ids, t.ID)
			}
			return nil, fmt.Errorf("no template %q: use %s", in.Template, strings.Join(ids, ", "))
		}
		for k, v := range config.Templates()[i].Values {
			values[k] = v
		}
	}
	for k, v := range in.Values {
		values[k] = v
	}
	name := values["name"]
	if name == "" {
		return nil, errors.New(`values need a "name" for the [program:name] section`)
	}
	content := config.RenderProgram(name, values)
	file := name + g.t.Config.IncludeExt()
	check, err := g.CheckConfig(mcp.CheckIn{Content: content, FileName: file}, true)
	if err != nil {
		return nil, err
	}
	return map[string]any{"fileName": file, "content": content, "check": check}, nil
}

func (g *tabTarget) Backups(in mcp.PathIn) (any, error) {
	list, err := g.t.Config.Backups(in.Path)
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for _, b := range list {
		out = append(out, map[string]any{"id": b.ID, "at": b.At.Format(time.RFC3339), "removed": b.Removed})
	}
	return map[string]any{"backups": out}, nil
}

func (g *tabTarget) Diagnose() (any, error) {
	if g.diagnose == nil {
		return nil, errors.New("diagnose isn't available here")
	}
	return g.diagnose()
}

// act runs a change with what it covers marked as made through MCP by client.
func (g *tabTarget) act(client string, e expectation, files bool, do func() (any, error)) (any, error) {
	if err := g.live(); err != nil {
		return nil, err
	}
	defer g.t.RT.markVia(client, e, files)()
	return do()
}

// onTarget runs process, group or file forms of start, stop and restart.
func (g *tabTarget) onTarget(client, target string, proc, group, file func(string) error) (any, error) {
	switch {
	case strings.Contains(target, "/"):
		return g.act(client, g.t.RT.expectFile(target), false, func() (any, error) { return done(file(target)) })
	case strings.HasSuffix(target, ":*"):
		gr := strings.TrimSuffix(target, ":*")
		return g.act(client, expectGroups(gr), false, func() (any, error) { return done(group(gr)) })
	}
	return g.act(client, expectProcess(target), false, func() (any, error) { return done(proc(target)) })
}

func done(err error) (any, error) {
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true}, nil
}

func (g *tabTarget) Start(client string, in mcp.TargetIn) (any, error) {
	s := g.t.Server
	return g.onTarget(client, in.Target, s.Start, s.StartGroup, s.StartFile)
}

func (g *tabTarget) Stop(client string, in mcp.TargetIn) (any, error) {
	s := g.t.Server
	return g.onTarget(client, in.Target, s.Stop, s.StopGroup, s.StopFile)
}

func (g *tabTarget) Restart(client string, in mcp.TargetIn) (any, error) {
	s := g.t.Server
	return g.onTarget(client, in.Target, s.Restart, s.RestartGroup, s.RestartFile)
}

func (g *tabTarget) Signal(client string, in mcp.SignalIn) (any, error) {
	return g.act(client, expectProcess(in.Name), false, func() (any, error) {
		return done(g.t.Server.Signal(in.Name, in.Signal))
	})
}

func (g *tabTarget) Apply(client string, in mcp.OptPathIn) (any, error) {
	if in.Path != "" {
		return g.act(client, g.t.RT.expectFile(in.Path), true, func() (any, error) { return g.t.Config.ApplyFile(in.Path) })
	}
	return g.act(client, expectAll(), true, func() (any, error) { return g.t.Server.ApplyChanges(nil) })
}

func (g *tabTarget) SaveConfig(client string, in mcp.SaveIn) (any, error) {
	content := in.Content
	if validate.HasHidden(content) {
		// Lines left as read_config showed them get their real values back
		// from the file (its hash is checked again when it is saved).
		f, err := g.t.Config.Read(in.Path)
		if err != nil {
			return nil, err
		}
		if content, err = validate.RestoreEnv(content, f.Content); err != nil {
			return nil, err
		}
	}
	return g.act(client, g.t.RT.expectFile(in.Path), true, func() (any, error) {
		return g.t.Config.Save(in.Path, content, in.Hash)
	})
}

func (g *tabTarget) CreateConfig(client string, in mcp.CreateIn) (any, error) {
	if validate.HasHidden(in.Content) {
		return nil, errors.New("the text holds «hidden» values: write the real ones")
	}
	path := filepath.Join(g.t.Config.IncludeDir(), filepath.Base(in.FileName))
	return g.act(client, g.t.RT.expectFile(path), true, func() (any, error) {
		return g.t.Config.Create(in.FileName, in.Content)
	})
}

func (g *tabTarget) RemoveConfig(client string, in mcp.PathIn) (any, error) {
	return g.act(client, g.t.RT.expectFile(in.Path), true, func() (any, error) {
		return g.t.Server.RemoveFromSupervisor(in.Path)
	})
}

func (g *tabTarget) RestoreConfig(client string, in mcp.RestoreIn) (any, error) {
	id := in.Backup
	if id == "" {
		removed, err := g.t.Config.RemovedFiles()
		if err != nil {
			return nil, err
		}
		i := slices.IndexFunc(removed, func(r RemovedView) bool { return r.Path == in.Path })
		if i < 0 {
			return nil, fmt.Errorf("%s wasn't removed by Visor: give a backup id from list_backups", in.Path)
		}
		id = removed[i].BackupID
	}
	return g.act(client, g.t.RT.expectFile(in.Path), true, func() (any, error) {
		return g.t.Config.Restore(in.Path, id)
	})
}
