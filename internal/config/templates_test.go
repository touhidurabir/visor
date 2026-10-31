package config_test

import (
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/touhidurabir/visor/internal/config"
	"github.com/touhidurabir/visor/internal/validate"
)

// everythingExists is a machine where every path, command and user exists.
func everythingExists() validate.Env {
	return validate.Env{
		IncludeGlobs: []string{"/conf/*.ini"},
		LookPath:     func(b string) (string, error) { return "/bin/" + b, nil },
		UserExists:   func(string) bool { return true },
		Stat: func(string) (fs.FileInfo, error) {
			return fstest.MapFS{"x": {Mode: fs.ModeDir | 0o755}}.Stat("x")
		},
		Writable: func(string) bool { return true },
	}
}

func TestTemplatesRenderValid(t *testing.T) {
	ts := config.Templates()
	if len(ts) != 5 {
		t.Fatalf("templates = %d, want 5", len(ts))
	}
	for _, tpl := range ts {
		name := "unique-" + tpl.ID
		content := config.RenderProgram(name, tpl.Values)
		for _, c := range validate.File(name+".ini", content, everythingExists()) {
			if c.Level == validate.Error {
				t.Errorf("%s: %s (line %d)\n%s", tpl.ID, c.Message, c.Line, content)
			}
		}
	}
}

func TestRenderProgramOrderAndSkip(t *testing.T) {
	got := config.RenderProgram("web", map[string]string{
		"priority": "10", "command": "node x.js", "user": "", "directory": "/srv", "bogus": "1", "autostart": " false ",
	})
	want := "[program:web]\ncommand=node x.js\ndirectory=/srv\nautostart=false\npriority=10\n"
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestProgramValuesRoundTrip(t *testing.T) {
	for _, tpl := range config.Templates() {
		content := config.RenderProgram("rt", tpl.Values)
		name, vals, err := config.ProgramValues(content)
		if err != nil || name != "rt" {
			t.Fatalf("%s: %q, %v", tpl.ID, name, err)
		}
		if config.RenderProgram(name, vals) != content {
			t.Errorf("%s: round trip changed the content", tpl.ID)
		}
	}
	// Only the first program, and only the form's keys.
	name, vals, err := config.ProgramValues("[program:a]\ncommand=x\numask=022\n[program:b]\ncommand=y\n")
	if err != nil || name != "a" || len(vals) != 1 || vals["command"] != "x" {
		t.Errorf("got %q %v %v", name, vals, err)
	}
	if _, _, err := config.ProgramValues("[eventlistener:z]\ncommand=x\n"); err == nil {
		t.Error("no program section: want an error")
	}
	if _, _, err := config.ProgramValues("junk"); err == nil {
		t.Error("unparsable: want an error")
	}
}

// M-6: a multi-line value (environment= over several lines) survives a
// round trip through the form: its continuation lines stay indented.
func TestRenderProgramKeepsMultiLineValues(t *testing.T) {
	src := "[program:web]\ncommand=node x.js\nenvironment=A=\"1\",\n  B=\"2\"\n"
	name, values, err := config.ProgramValues(src)
	if err != nil {
		t.Fatal(err)
	}
	out := config.RenderProgram(name, values)
	if out != src {
		t.Errorf("rendered %q, want %q", out, src)
	}
	_, again, err := config.ProgramValues(out)
	if err != nil || again["environment"] != values["environment"] {
		t.Errorf("environment = %q (%v), want %q", again["environment"], err, values["environment"])
	}
	// Blank and space-only lines a user typed don't end the value early.
	out = config.RenderProgram("web", map[string]string{"command": "x", "environment": "A=\"1\",\n\n   \nB=\"2\""})
	if want := "[program:web]\ncommand=x\nenvironment=A=\"1\",\n  B=\"2\"\n"; out != want {
		t.Errorf("rendered %q, want %q", out, want)
	}
}
