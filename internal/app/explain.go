package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/touhidurabir/visor/internal/config"
	"github.com/touhidurabir/visor/internal/describe"
	"github.com/touhidurabir/visor/internal/logs"
	"github.com/touhidurabir/visor/internal/supervisor"
	"github.com/touhidurabir/visor/internal/validate"
)

// ExplainView explains a config line by line: a
// plain-English summary, then every line with what it means, the problems
// on it with how to fix them, and the files and folders it points to,
// checked on disk. Errors and Suggestions count lines; Settings counts
// explained settings and Paths the paths checked. SaveErrors counts the
// problems that block saving: errors in the text, not what supervisord
// says about the running program. ParseError is set (and
// Sections empty) when the file cannot be read as a config. FileProblems
// are problems not about one line.
type ExplainView struct {
	Summary      string
	Errors       int
	SaveErrors   int
	Suggestions  int
	Settings     int
	Paths        int
	ParseError   *ProblemView
	FileProblems []ProblemView
	Sections     []ExplainSectionView
}

// ExplainSectionView is one [name] block; Program is "" unless it is a
// [program:x].
type ExplainSectionView struct {
	Name    string
	Program string
	Lines   []ExplainLineView
}

// ExplainLineView is one line. Kind is "section", "key" or "unset" (the
// settings a program leaves out, with the defaults that apply; Line is
// then the line after which Add would go). Level is the worst of Problems
// ("error" or "warning"), "suggestion" for an unset row with a
// suggestion, or "".
type ExplainLineView struct {
	Line     int
	Text     string
	Key      string
	Kind     string
	Meaning  string
	Level    string
	Problems []ProblemView
	Suggest  string
	Add      []string
	Paths    []LinePathView
}

// ProblemView is one problem: what is wrong, how to fix it, and the exact
// change when there is one. Line is where it is (0: the whole file). Live
// marks what supervisord says about the running program, rather than a
// problem in the text.
type ProblemView struct {
	Level   string
	Live    bool
	Line    int
	Message string
	Fix     string
	Edit    *EditView
}

// EditView is one change to a config: Text replaces line Line, or with
// Insert goes in as a new line after it.
type EditView struct {
	Line   int
	Insert bool
	Text   string
	Rename string // only the key on Line becomes Rename; Text shows the result
}

// LinePathView is a file or folder a line points to. Kind is "bin", "dir"
// or "log"; Auto marks a log file supervisord named itself. Size is the
// file's size in bytes (-1 when unknown). A log has the Process and
// Stream ("stdout" or "stderr") the Logs tab shows it under.
type LinePathView struct {
	Label   string
	Path    string
	Kind    string
	Exists  bool
	Auto    bool
	Size    int64
	Process string
	Stream  string
}

// explain explains content as the file fileName. selfPath is the file on
// disk it is ("" for a new one): its processes add the log files
// supervisord named and what is failing right now.
func (r *Runtime) explain(fileName, content, selfPath string) ExplainView {
	checks := validate.File(fileName, content, r.validateEnv(selfPath))

	r.mu.Lock()
	inv, snap := r.inv, r.last.Snapshot.Processes
	r.mu.Unlock()
	byProg := map[string][]supervisor.ProcessInfo{}
	if selfPath != "" {
		for _, p := range snap {
			if path, prog, ok := inv.Lookup(p.FullName()); ok && path == selfPath {
				byProg[prog.Name] = append(byProg[prog.Name], p)
			}
		}
	}
	hints := describe.Hints{LogDir: map[string]string{}}
	for name, ps := range byProg {
		for _, p := range ps {
			if p.StdoutLogfile != "" {
				hints.LogDir[name] = filepath.Dir(p.StdoutLogfile)
				break
			}
		}
	}

	var v ExplainView
	ex, err := describe.Explain(content, hints)
	if err != nil {
		for _, c := range checks {
			if c.Level == validate.Error {
				p := problemOf(c)
				v.ParseError = &p
				break
			}
		}
		if v.ParseError == nil {
			v.ParseError = &ProblemView{Level: "error", Message: err.Error()}
		}
		v.Errors, v.SaveErrors = 1, 1
		return v
	}
	parsed, _ := config.ParseINI(strings.NewReader(content), fileName)
	here := filepath.Dir(selfPath)
	if selfPath == "" {
		here = r.includeDir()
	}

	// Validation problems by line.
	byLine := map[int][]ProblemView{}
	for _, c := range checks {
		if c.Level == validate.OK {
			continue
		}
		byLine[c.Line] = append(byLine[c.Line], problemOf(c))
	}
	used := map[int]bool{}

	v.Summary = ex.Summary
	var failures []string
	for _, s := range ex.Sections {
		sv := ExplainSectionView{Name: s.Name, Program: s.Program}
		var sec config.Section
		if k := slices.IndexFunc(parsed.Sections, func(x config.Section) bool { return x.Name == s.Name }); k >= 0 {
			sec = parsed.Sections[k]
		}
		prog := config.Program{Name: s.Program, Group: s.Program, Section: sec, NumProcs: max(intKey(sec, "numprocs", 1), 1)}
		procs := byProg[s.Program]
		for _, l := range s.Lines {
			lv := ExplainLineView{Line: l.Line, Text: l.Text, Key: l.Key, Kind: l.Kind, Meaning: l.Meaning, Suggest: l.Suggest, Add: l.Add}
			if l.Kind != describe.KindUnset {
				lv.Problems = byLine[l.Line]
				used[l.Line] = true
			}
			if s.Program != "" {
				lv.Paths = r.linePaths(l, prog, here, procs)
			}
			sv.Lines = append(sv.Lines, lv)
		}
		if s.Program != "" {
			if msg := liveFailure(&sv, prog, procs, inv); msg != "" {
				failures = append(failures, s.Program+" can’t run: "+msg)
			}
		}
		v.Sections = append(v.Sections, sv)
	}
	for line, ps := range byLine {
		if !used[line] {
			v.FileProblems = append(v.FileProblems, ps...)
		}
	}
	slices.SortStableFunc(v.FileProblems, func(a, b ProblemView) int { return a.Line - b.Line })

	for i := range v.Sections {
		for j := range v.Sections[i].Lines {
			l := &v.Sections[i].Lines[j]
			l.Level = worst(l.Problems)
			if l.Level == "" && l.Suggest != "" {
				l.Level = "suggestion"
			}
			switch l.Level {
			case "error":
				v.Errors++
			case "warning", "suggestion":
				v.Suggestions++
			}
			for _, p := range l.Problems {
				if p.Level == "error" && !p.Live {
					v.SaveErrors++
				}
			}
			if l.Kind == describe.KindKey {
				v.Settings++
			}
			v.Paths += len(l.Paths)
		}
	}
	for _, p := range v.FileProblems {
		if p.Level == "error" {
			v.Errors++
			v.SaveErrors++
		} else {
			v.Suggestions++
		}
	}
	if len(failures) > 0 {
		v.Summary += " " + strings.Join(failures, " ")
	}
	return v
}

// includeDir is where a new config would go, for %(here)s in a new file.
func (r *Runtime) includeDir() string {
	dir, _ := r.newFileHome()
	return dir
}

func problemOf(c validate.Check) ProblemView {
	p := ProblemView{Level: string(c.Level), Line: c.Line, Message: upperFirst(c.Message), Fix: c.Fix}
	if c.Edit != nil {
		p.Edit = &EditView{Line: c.Edit.Line, Insert: c.Edit.Insert, Text: c.Edit.Text, Rename: c.Edit.Rename}
	}
	return p
}

func worst(ps []ProblemView) string {
	level := ""
	for _, p := range ps {
		switch p.Level {
		case "error":
			return "error"
		case "warning":
			level = "warning"
		}
	}
	return level
}

// linePaths are the files and folders line l of prog points to.
func (r *Runtime) linePaths(l describe.Line, prog config.Program, here string, procs []supervisor.ProcessInfo) []LinePathView {
	vars := progVars(prog, here)
	sec := prog.Section
	first := ""
	if len(procs) > 0 {
		first = procs[0].FullName()
	}
	logOf := func(s logs.Stream, label string, auto bool) []LinePathView {
		var out []LinePathView
		for _, pv := range progLogs(prog, s, procs, vars) {
			out = append(out, LinePathView{Label: label, Path: pv.Path, Kind: "log", Auto: auto, Process: logProcess(pv.Path, s, procs, first), Stream: string(s)})
		}
		return out
	}
	var out []LinePathView
	switch {
	case l.Kind == describe.KindKey && l.Key == "command":
		cmd, _ := sec.Get("command")
		bin := commandBinary(cmd, vars)
		if bin == "" || strings.Contains(bin, "%(") {
			return nil
		}
		if !strings.Contains(bin, "/") {
			found, err := lookPath(bin)
			if err != nil {
				return []LinePathView{{Label: "Program", Path: bin, Kind: "bin", Size: -1}}
			}
			bin = found
		} else if !filepath.IsAbs(bin) {
			if dir, ok := sec.Get("directory"); ok && filepath.IsAbs(strings.TrimSpace(dir)) {
				bin = filepath.Join(config.Expand(strings.TrimSpace(dir), vars), bin)
			} else {
				return nil
			}
		}
		out = []LinePathView{{Label: "Program", Path: bin, Kind: "bin"}}
	case l.Kind == describe.KindKey && l.Key == "directory":
		dir, _ := sec.Get("directory")
		dir = strings.TrimSpace(config.Expand(strings.TrimSpace(dir), vars))
		if !filepath.IsAbs(dir) || strings.Contains(dir, "%(") {
			return nil
		}
		out = []LinePathView{{Label: "Working directory", Path: filepath.Clean(dir), Kind: "dir"}}
	case l.Kind == describe.KindKey && l.Key == "stdout_logfile":
		label := "Output log"
		if truthy(sec, "redirect_stderr") {
			label = "Output and error log"
		}
		out = logOf(logs.Stdout, label, isAutoValue(sec, "stdout_logfile"))
	case l.Kind == describe.KindKey && l.Key == "stderr_logfile" && !truthy(sec, "redirect_stderr"):
		out = logOf(logs.Stderr, "Error log", isAutoValue(sec, "stderr_logfile"))
	case l.Kind == describe.KindUnset:
		if slices.Contains(l.Unset, "stdout_logfile") {
			label := "Output log"
			if truthy(sec, "redirect_stderr") {
				label = "Output and error log"
			}
			out = append(out, logOf(logs.Stdout, label, true)...)
		}
		if slices.Contains(l.Unset, "stderr_logfile") {
			out = append(out, logOf(logs.Stderr, "Error log", true)...)
		}
	}
	for i := range out {
		p := &out[i]
		if p.Kind == "log" && strings.Contains(p.Path, "%(") {
			p.Size = -1
			continue
		}
		fi, err := os.Stat(p.Path)
		p.Exists, p.Size = err == nil, -1
		if err == nil && !fi.IsDir() {
			p.Size = fi.Size()
		}
	}
	return out
}

// logProcess is the process whose stream s writes path, else first.
func logProcess(path string, s logs.Stream, procs []supervisor.ProcessInfo, first string) string {
	for _, p := range procs {
		if (s == logs.Stdout && p.StdoutLogfile == path) || (s == logs.Stderr && p.StderrLogfile == path) {
			return p.FullName()
		}
	}
	return first
}

func isAutoValue(s config.Section, key string) bool {
	v, _ := s.Get(key)
	v = strings.TrimSpace(v)
	return v == "" || strings.EqualFold(v, "auto")
}

// liveFailure puts what supervisord says about prog's failing processes
// on the line that causes it (command, directory or user, else the
// section header), taking over the warning already there with its fix.
// It returns the message, or "" when nothing is failing.
func liveFailure(sv *ExplainSectionView, prog config.Program, procs []supervisor.ProcessInfo, inv config.Inventory) string {
	for _, p := range procs {
		expected := slices.Contains(inv.ExpectedExit(p.FullName()), p.ExitStatus)
		if classify(p.State, expected) != classFailing {
			continue
		}
		msg := lastErrorMessage(p, intKey(prog.Section, "startretries", defaultStartRetries), inv.ExpectedExit(p.FullName()))
		k := failureLine(sv, p.SpawnErr)
		l := &sv.Lines[k]
		live := ProblemView{Level: "error", Live: true, Line: l.Line, Message: msg}
		if j := slices.IndexFunc(l.Problems, func(x ProblemView) bool { return x.Level == "warning" }); j >= 0 {
			live.Fix, live.Edit = l.Problems[j].Fix, l.Problems[j].Edit
			l.Problems = slices.Delete(l.Problems, j, j+1)
		}
		l.Problems = append([]ProblemView{live}, l.Problems...)
		first, _, _ := strings.Cut(msg, ". ")
		return lowerFirst(strings.TrimSuffix(first, ".")) + "."
	}
	return ""
}

// failureLine is the index of the line a spawn error is about.
func failureLine(sv *ExplainSectionView, spawnErr string) int {
	e := strings.ToLower(spawnErr)
	key := ""
	switch {
	case strings.Contains(e, "command"), strings.Contains(e, "executable"), strings.Contains(e, "no such file"):
		key = "command"
	case strings.Contains(e, "director"), strings.Contains(e, "chdir"):
		key = "directory"
	case strings.Contains(e, "user"), strings.Contains(e, "setuid"), strings.Contains(e, "uid"):
		key = "user"
	}
	if key != "" {
		if k := slices.IndexFunc(sv.Lines, func(l ExplainLineView) bool { return l.Kind == describe.KindKey && l.Key == key }); k >= 0 {
			return k
		}
	}
	return 0
}

// explainLine is the line where program's failure is shown in ex, or 0.
func explainLine(ex ExplainView, program string) int {
	for _, s := range ex.Sections {
		if s.Program != program {
			continue
		}
		for _, l := range s.Lines {
			if l.Kind != describe.KindUnset && l.Level == "error" {
				return l.Line
			}
		}
	}
	return 0
}
