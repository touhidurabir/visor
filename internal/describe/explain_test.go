package describe

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func renderExplain(ex Explanation) string {
	var b strings.Builder
	b.WriteString("summary: " + ex.Summary + "\n")
	for _, s := range ex.Sections {
		fmt.Fprintf(&b, "\n[%s] program=%q\n", s.Name, s.Program)
		for _, l := range s.Lines {
			fmt.Fprintf(&b, "%3d %-7s %s\n    => %s\n", l.Line, l.Kind, l.Text, l.Meaning)
			if l.Suggest != "" {
				b.WriteString("    suggest: " + l.Suggest + "\n")
			}
			for _, a := range l.Add {
				b.WriteString("    add: " + a + "\n")
			}
		}
	}
	return b.String()
}

func TestExplainGolden(t *testing.T) {
	inputs, err := filepath.Glob("testdata/explain/*.ini")
	if err != nil || len(inputs) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}
	hints := Hints{LogDir: map[string]string{"web": "/usr/local/var/log/supervisor"}}
	for _, in := range inputs {
		name := strings.TrimSuffix(filepath.Base(in), ".ini")
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(in)
			if err != nil {
				t.Fatal(err)
			}
			ex, err := Explain(string(src), hints)
			if err != nil {
				t.Fatal(err)
			}
			got := renderExplain(ex)
			golden := strings.TrimSuffix(in, ".ini") + ".golden"
			if *update {
				if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("%v (run with -update)", err)
			}
			if got != string(want) {
				t.Errorf("mismatch for %s\n--- got\n%s--- want\n%s", name, got, want)
			}
		})
	}
}

func TestExplainParseError(t *testing.T) {
	if _, err := Explain("[program:a\n", Hints{}); err == nil {
		t.Error("want a parse error")
	}
}

func TestExplainEverySettingHasAMeaning(t *testing.T) {
	for _, k := range []string{"command", "process_name", "numprocs", "numprocs_start", "priority",
		"autostart", "startsecs", "startretries", "autorestart", "exitcodes",
		"stopsignal", "stopwaitsecs", "stopasgroup", "killasgroup", "user",
		"redirect_stderr", "stdout_logfile", "stdout_logfile_maxbytes", "stdout_logfile_backups",
		"stdout_capture_maxbytes", "stdout_events_enabled", "stdout_syslog",
		"stderr_logfile", "stderr_logfile_maxbytes", "stderr_logfile_backups",
		"stderr_capture_maxbytes", "stderr_events_enabled", "stderr_syslog",
		"environment", "directory", "umask", "serverurl"} {
		ex, err := Explain("[program:a]\n"+k+"=1\n", Hints{})
		if err != nil {
			t.Fatal(err)
		}
		if m := ex.Sections[0].Lines[1].Meaning; strings.Contains(m, "doesn’t know") || m == "" {
			t.Errorf("%s: meaning %q", k, m)
		}
	}
}
