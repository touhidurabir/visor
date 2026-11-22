package app

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/touhidurabir/visor/internal/supervisor"
	"github.com/touhidurabir/visor/internal/supervisor/supervisortest"
)

func findLine(t *testing.T, v ExplainView, line int, kind string) ExplainLineView {
	t.Helper()
	for _, s := range v.Sections {
		for _, l := range s.Lines {
			if l.Line == line && l.Kind == kind {
				return l
			}
		}
	}
	t.Fatalf("no %s row on line %d in %+v", kind, line, v)
	return ExplainLineView{}
}

// A new file: problems land on their lines with fixes, paths are checked.
func TestExplainNewFile(t *testing.T) {
	rt := NewRuntime(RuntimeOptions{StateDir: t.TempDir()})
	dir := t.TempDir()
	content := "[program:acme]\ncommand=/bin/sh -c 'sleep 1'\ndirectory=" + dir + "/nope\nnumprocs=3\nprocess_name=%(program_name)s\nstdout_logfile=" + dir + "/out.log\n"
	if err := os.WriteFile(dir+"/out.log", []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	v := rt.explain("acme.conf", content, "")
	if v.ParseError != nil {
		t.Fatalf("parse error %+v", v.ParseError)
	}
	cmd := findLine(t, v, 2, "key")
	if len(cmd.Paths) != 1 || cmd.Paths[0].Path != "/bin/sh" || !cmd.Paths[0].Exists || cmd.Paths[0].Kind != "bin" {
		t.Errorf("command paths = %+v", cmd.Paths)
	}
	d := findLine(t, v, 3, "key")
	if d.Level != "warning" || len(d.Paths) != 1 || d.Paths[0].Exists || d.Problems[0].Fix == "" {
		t.Errorf("directory = %+v", d)
	}
	pn := findLine(t, v, 5, "key")
	if pn.Level != "error" || pn.Problems[0].Edit == nil || pn.Problems[0].Edit.Text != "process_name=%(program_name)s_%(process_num)02d" {
		t.Errorf("process_name = %+v", pn)
	}
	out := findLine(t, v, 6, "key")
	if len(out.Paths) != 1 || !out.Paths[0].Exists || out.Paths[0].Size != 5 || out.Paths[0].Auto || out.Paths[0].Stream != "stdout" {
		t.Errorf("stdout paths = %+v", out.Paths)
	}
	unset := findLine(t, v, 6, "unset")
	if unset.Level != "suggestion" || len(unset.Add) != 1 || unset.Add[0] != "stderr_logfile="+dir+"/acme.err.log" {
		t.Errorf("unset = %+v", unset)
	}
	if v.Errors != 1 || v.SaveErrors != 1 || v.Suggestions != 2 || v.Settings != 5 || v.Paths != 3 {
		t.Errorf("counts = %d errors, %d suggestions, %d settings, %d paths", v.Errors, v.Suggestions, v.Settings, v.Paths)
	}
	if v.Summary != "This file sets up one program." {
		t.Errorf("summary = %q", v.Summary)
	}
	if s := v.Sections[0]; s.Summary != "Runs three copies of a shell command and starts with supervisord." || s.Failure != "" {
		t.Errorf("section summary = %q, failure = %q", s.Summary, s.Failure)
	}
}

func TestExplainParseError(t *testing.T) {
	rt := NewRuntime(RuntimeOptions{StateDir: t.TempDir()})
	v := rt.explain("a.conf", "[program:a\ncommand=x\n", "")
	if v.ParseError == nil || v.ParseError.Line != 1 || v.Errors != 1 || len(v.Sections) != 0 {
		t.Fatalf("explain = %+v", v)
	}
}

// What supervisord says about a failing program lands on the line that
// causes it, with the fix, and the last error card points at that line.
func TestConfigDetailExplainsLiveFailure(t *testing.T) {
	broken := supervisortest.Program{Name: "broken", Command: "/nonexistent/sync",
		Extra: map[string]string{"startsecs": "0", "startretries": "0", "autorestart": "false"}}
	inst := supervisortest.Start(t, broken, supervisortest.Sleeper)
	inst.WaitFor(t, "broken:broken", supervisor.StateFatal)
	rt, _ := startRuntime(t, confOf(inst), t.TempDir(), 100*time.Millisecond)
	svc := NewServerService(rt)
	waitOverview(t, svc, 5*time.Second, "broken fatal", procState("broken:broken", "FATAL"))

	path := filepath.Join(inst.Dir(), "conf.d", "broken.ini")
	d, err := svc.ConfigDetail(path)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	cmdLine := 0
	for i, l := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(l, "command=") {
			cmdLine = i + 1
		}
	}
	l := findLine(t, d.Explain, cmdLine, "key")
	if l.Level != "error" || !l.Problems[0].Live || !strings.Contains(l.Problems[0].Message, "supervisord gave up") || l.Problems[0].Fix == "" {
		t.Errorf("command line = %+v", l)
	}
	if len(l.Paths) != 1 || l.Paths[0].Exists {
		t.Errorf("command paths = %+v", l.Paths)
	}
	if d.LastError == nil || d.LastError.Line != cmdLine {
		t.Errorf("last error = %+v, want line %d", d.LastError, cmdLine)
	}
	if d.Explain.SaveErrors != 0 {
		t.Errorf("a live failure blocks saving: %d", d.Explain.SaveErrors)
	}
	// The failure is on its program's section, not in the file's summary.
	if strings.Contains(d.Explain.Summary, "can’t run") {
		t.Errorf("summary = %q", d.Explain.Summary)
	}
	for _, s := range d.Explain.Sections {
		switch s.Program {
		case "broken":
			if !strings.HasPrefix(s.Failure, "Can’t run: ") {
				t.Errorf("broken failure = %q", s.Failure)
			}
		default:
			if s.Failure != "" {
				t.Errorf("%s failure = %q", s.Program, s.Failure)
			}
		}
	}
	for _, s := range d.Explain.Sections {
		for _, ln := range s.Lines {
			for _, p := range ln.Paths {
				if p.Kind == "log" && p.Process != "broken:broken" {
					t.Errorf("log path without its process: %+v", p)
				}
			}
		}
	}
}

// statFS answers Stat for the files it holds, as a server's would.
type statFS map[string]bool

func (f statFS) ReadFile(string) ([]byte, error) { return nil, fs.ErrNotExist }
func (f statFS) Glob(string) ([]string, error)   { return nil, nil }
func (f statFS) Stat(name string) (fs.FileInfo, error) {
	if f[name] {
		return fstest.MapFS{"f": {Mode: 0o755}}.Stat("f")
	}
	return nil, fs.ErrNotExist
}

func TestLookOnServerSearchesTheServersPath(t *testing.T) {
	r := &Runtime{opts: RuntimeOptions{Remote: &RemoteSource{}}}
	r.d.FS = statFS{"/usr/bin/php": true}
	if p, err := r.lookOnServer("php"); err != nil || p != "/usr/bin/php" {
		t.Fatalf("php: %q %v", p, err)
	}
	if _, err := r.lookOnServer("node"); err == nil {
		t.Fatal("node found on a server that hasn't it")
	}
	if p, err := r.lookOnServer("/usr/bin/php"); err != nil || p != "/usr/bin/php" {
		t.Fatalf("a full path: %q %v", p, err)
	}
}
