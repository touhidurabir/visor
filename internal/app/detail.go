package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/touhidurabir/visor/internal/config"
	"github.com/touhidurabir/visor/internal/logs"
	"github.com/touhidurabir/visor/internal/supervisor"
)

const (
	// stderrTailBytes is how much of a failing process's stderr the last
	// error card reads, in one tail call.
	stderrTailBytes = 8 << 10
	// stderrTailLines is how many of its last lines the card shows.
	stderrTailLines = 20
	// defaultStartRetries is supervisord's startretries default.
	defaultStartRetries = 3
)

// lookPath resolves a command name.
// TODO(Task 20): use platform.LookPath, which also searches the login
// shell's PATH, /opt/homebrew/bin and /usr/local/bin.
var lookPath = exec.LookPath

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// configDetail builds the config detail page for one include file.
func (r *Runtime) configDetail(path string) (ConfigDetailView, error) {
	// Without a connection the inventory is empty: say why, rather than
	// that the file is unknown.
	if _, err := r.conn(); err != nil {
		return ConfigDetailView{}, err
	}
	ov := r.Overview()
	r.mu.Lock()
	inv, snap := r.inv, r.last.Snapshot.Processes
	r.mu.Unlock()

	i := slices.IndexFunc(inv.Files, func(f config.ConfigFile) bool { return f.Path == path })
	j := slices.IndexFunc(ov.Configs, func(c ConfigView) bool { return c.Path == path && path != "" })
	if i < 0 || j < 0 {
		return ConfigDetailView{}, fmt.Errorf("%s is not one of supervisord's config files: check the [include] files= setting", path)
	}
	d := ConfigDetailView{Config: ov.Configs[j], Programs: len(inv.Files[i].Programs)}

	mine := map[string]bool{}
	for _, p := range d.Config.Processes {
		mine[p.FullName] = true
	}
	var procs []supervisor.ProcessInfo
	for _, p := range snap {
		if mine[p.FullName()] {
			procs = append(procs, p)
		}
	}
	look, has := lookPath, exists
	if r.Remote() {
		// The paths are on the server: stat them there; a bare command name
		// can't be looked up on the server's PATH, so it stays as written.
		fsys := r.files()
		look = func(string) (string, error) { return "", errors.New("not looked up on servers") }
		has = func(p string) bool { _, err := fsys.Stat(p); return err == nil }
	}
	d.Paths, d.Logs = buildPaths(inv.Files[i], inv, procs, look, has)
	d.LastError = r.lastError(d, inv, procs)
	if raw, err := r.files().ReadFile(path); err != nil {
		d.ExplainError = err.Error()
	} else {
		d.Explain = r.explain(filepath.Base(path), string(raw), path)
	}
	if d.LastError != nil {
		if _, prog, ok := inv.Lookup(d.LastError.Process); ok {
			d.LastError.Line = explainLine(d.Explain, prog.Name)
		}
	}
	return d, nil
}

// lastError describes the most recently failed process of d, or nil.
func (r *Runtime) lastError(d ConfigDetailView, inv config.Inventory, procs []supervisor.ProcessInfo) *LastErrorView {
	var latest *ProcessView
	for k, p := range d.Config.Processes {
		if p.Status == "failing" && (latest == nil || p.StopMs > latest.StopMs) {
			latest = &d.Config.Processes[k]
		}
	}
	if latest == nil {
		return nil
	}
	k := slices.IndexFunc(procs, func(p supervisor.ProcessInfo) bool { return p.FullName() == latest.FullName })
	if k < 0 {
		return nil
	}
	info := procs[k]
	_, prog, _ := inv.Lookup(latest.FullName)
	le := &LastErrorView{
		Process:    latest.FullName,
		State:      latest.State,
		Since:      strings.TrimPrefix(latest.Since, "failing "),
		Message:    lastErrorMessage(info, intKey(prog.Section, "startretries", defaultStartRetries), inv.ExpectedExit(latest.FullName)),
		ExitStatus: info.ExitStatus,
	}
	for _, l := range d.Logs {
		if l.Process == latest.FullName {
			le.StderrLog = l.Stderr
		}
	}
	if le.StderrLog == "" {
		return le
	}
	stream := logs.Stderr
	if truthy(prog.Section, "redirect_stderr") {
		stream = logs.Stdout
	}
	if ac, err := r.conn(); err == nil {
		ctx, cancel := context.WithTimeout(ac.ctx, rpcTimeout)
		defer cancel()
		le.StderrTail = tailLines(ctx, ac.client, latest.FullName, stream, stderrTailBytes, stderrTailLines)
	}
	return le
}

// tailLines returns the last n lines of a process's log, read with one
// tail call of at most length bytes, or "" if it cannot be read.
func tailLines(ctx context.Context, c logs.TailClient, process string, s logs.Stream, length, n int) string {
	text, err := logs.Tail(ctx, c, process, s, length)
	if err != nil || text == "" {
		return ""
	}
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	return strings.Join(lines[max(0, len(lines)-n):], "\n")
}

// lastErrorMessage explains why p is failing, in a sentence or two. A
// process that exits too quickly never gets its exit status recorded
// (supervisord only keeps it for exits after startsecs), so FATAL and
// BACKOFF messages leave it out.
func lastErrorMessage(p supervisor.ProcessInfo, startRetries int, expected []int) string {
	reason := p.SpawnErr
	if reason == "" {
		reason = "Exited too quickly"
	}
	reason = upperFirst(strings.TrimSuffix(reason, "."))
	switch p.State {
	case supervisor.StateFatal:
		return fmt.Sprintf("%s. supervisord gave up after %d start attempts.", reason, startRetries+1)
	case supervisor.StateBackoff:
		return reason + ". supervisord is retrying."
	case supervisor.StateExited:
		codes := make([]string, len(expected))
		for i, c := range expected {
			codes[i] = strconv.Itoa(c)
		}
		return fmt.Sprintf("Exited with status %d, which this program does not expect (expected %s).", p.ExitStatus, strings.Join(codes, ", "))
	}
	return ""
}

// noLog says why a program's stream has no log file, or nil if it has
// one. supervisord answers a tail of such a stream with empty data, not an
// error (R23), so Visor decides from the config.
func noLog(prog config.Program, s logs.Stream) error {
	if s == logs.Stderr && truthy(prog.Section, "redirect_stderr") {
		return fmt.Errorf("stderr is sent to the stdout log: %w", logs.ErrNoLog)
	}
	v, _ := prog.Section.Get(string(s) + "_logfile")
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "none", "syslog":
		return logs.ErrNoLog
	}
	return nil
}

// buildPaths lists the working directories, command binaries and log files
// of file's programs, each once, and where each of procs (the file's
// processes supervisord runs) writes its output. Log paths come from
// supervisord, which knows the AUTO names; a program supervisord does not
// run shows the log path its config sets, if any.
func buildPaths(file config.ConfigFile, inv config.Inventory, procs []supervisor.ProcessInfo, look func(string) (string, error), exists func(string) bool) ([]PathView, []ProcessLogView) {
	here := filepath.Dir(file.Path)
	byProg := map[string][]supervisor.ProcessInfo{}
	for _, p := range procs {
		if path, prog, ok := inv.Lookup(p.FullName()); ok && path == file.Path {
			byProg[prog.Name] = append(byProg[prog.Name], p)
		}
	}

	var dirs, bins, logPaths []PathView
	for _, prog := range file.Programs {
		vars := progVars(prog, here)
		if v, ok := prog.Section.Get("directory"); ok && strings.TrimSpace(v) != "" {
			dirs = append(dirs, PathView{Label: prog.Name, Path: filepath.Clean(config.Expand(strings.TrimSpace(v), vars)), Kind: "dir"})
		}
		cmd, _ := prog.Section.Get("command")
		if bin := commandBinary(cmd, vars); bin != "" {
			pv := PathView{Label: "Command (" + filepath.Base(bin) + ")", Path: bin, Kind: "bin"}
			if !strings.Contains(bin, "/") {
				if found, err := look(bin); err == nil {
					pv.Path = found
				}
			}
			bins = append(bins, pv)
		}
		for _, s := range []logs.Stream{logs.Stdout, logs.Stderr} {
			logPaths = append(logPaths, progLogs(prog, s, byProg[prog.Name], vars)...)
		}
	}
	if len(dedupe(dirs)) == 1 {
		dirs[0].Label = "Working directory"
	} else {
		for k := range dirs {
			dirs[k].Label = "Working directory (" + dirs[k].Label + ")"
		}
	}

	var out []PathView
	for _, p := range dedupe(append(append(dirs, bins...), logPaths...)) {
		if strings.Contains(p.Path, "%(") {
			continue // %(ENV_x)s: only supervisord knows the path
		}
		switch {
		case p.Kind == "bin" && !filepath.IsAbs(p.Path):
			p.Exists = false // not found on PATH
		default:
			p.Exists = exists(p.Path)
		}
		out = append(out, p)
	}

	var plogs []ProcessLogView
	for _, prog := range file.Programs {
		for _, p := range byProg[prog.Name] {
			l := ProcessLogView{Process: p.FullName(), Stdout: p.StdoutLogfile, Stderr: p.StderrLogfile}
			if noLog(prog, logs.Stdout) != nil {
				l.Stdout = ""
			}
			switch err := noLog(prog, logs.Stderr); {
			case truthy(prog.Section, "redirect_stderr"):
				l.Stderr = l.Stdout
			case err != nil:
				l.Stderr = ""
			}
			plogs = append(plogs, l)
		}
	}
	slices.SortFunc(plogs, func(a, b ProcessLogView) int { return strings.Compare(a.Process, b.Process) })
	return out, plogs
}

// progLogs lists one stream's log files for a program: one entry when its
// processes share a file, else one per process.
func progLogs(prog config.Program, s logs.Stream, procs []supervisor.ProcessInfo, vars map[string]string) []PathView {
	if noLog(prog, s) != nil {
		return nil
	}
	v, _ := prog.Section.Get(string(s) + "_logfile")
	v = strings.TrimSpace(v)
	auto := v == "" || strings.EqualFold(v, "auto")
	what := " log"
	if s == logs.Stderr {
		what = " errors"
	}
	if auto {
		what += " (auto)"
	}
	type entry struct{ name, path string }
	var es []entry
	for _, p := range procs {
		path := p.StdoutLogfile
		if s == logs.Stderr {
			path = p.StderrLogfile
		}
		if path != "" {
			es = append(es, entry{p.Name, path})
		}
	}
	if len(procs) == 0 && !auto {
		es = append(es, entry{prog.Name, config.Expand(v, vars)})
	}
	shared := len(es) > 0 && !slices.ContainsFunc(es, func(e entry) bool { return e.path != es[0].path })
	if shared {
		return []PathView{{Label: prog.Name + what, Path: es[0].path, Kind: "log"}}
	}
	out := make([]PathView, 0, len(es))
	for _, e := range es {
		out = append(out, PathView{Label: e.name + what, Path: e.path, Kind: "log"})
	}
	return out
}

// dedupe drops repeated (Kind, Path) pairs, keeping the first.
func dedupe(ps []PathView) []PathView {
	seen := map[string]bool{}
	var out []PathView
	for _, p := range ps {
		if k := p.Kind + "\x00" + p.Path; !seen[k] {
			seen[k] = true
			out = append(out, p)
		}
	}
	return out
}

// progVars are the %(name)s expansions supervisord offers in program
// values, with the first process number. %(ENV_x)s stays unexpanded: it comes
// from supervisord's environment, which Visor can't see, and Visor's own must
// never reach an AI tool or a server.
func progVars(prog config.Program, here string) map[string]string {
	return map[string]string{
		"program_name": prog.Name,
		"group_name":   prog.Group,
		"here":         here,
		"process_num":  strconv.Itoa(intKey(prog.Section, "numprocs_start", 0)),
		"numprocs":     strconv.Itoa(prog.NumProcs),
	}
}

// commandBinary is the program a command= line runs: its first word, with
// quotes removed and %(name)s expanded.
func commandBinary(cmd string, vars map[string]string) string {
	cmd = strings.TrimSpace(config.Expand(cmd, vars))
	if cmd == "" {
		return ""
	}
	if q := cmd[0]; q == '"' || q == '\'' {
		if end := strings.IndexByte(cmd[1:], q); end >= 0 {
			return cmd[1 : end+1]
		}
		return cmd[1:]
	}
	if i := strings.IndexFunc(cmd, unicode.IsSpace); i >= 0 {
		return cmd[:i]
	}
	return cmd
}

// truthy reads a supervisord boolean (true/yes/on/1).
func truthy(s config.Section, key string) bool {
	v, _ := s.Get(key)
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "yes", "on", "1":
		return true
	}
	return false
}

func intKey(s config.Section, key string, def int) int {
	v, ok := s.Get(key)
	if !ok {
		return def
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return def
	}
	return n
}

func upperFirst(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return s
	}
	return string(unicode.ToUpper(r)) + s[n:]
}
