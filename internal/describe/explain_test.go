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
		fmt.Fprintf(&b, "\n[%s] program=%q\n  summary: %s\n", s.Name, s.Program, s.Summary)
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

func TestExplainSectionSummaries(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"[program:a]\ncommand=php /srv/app/artisan ocpp:watch-ceilings --sleep=1\n", "Runs the Laravel command ocpp:watch-ceilings and starts with supervisord."},
		{"[program:a]\ncommand=php artisan reverb:start\nautostart=false\n", "Runs the Laravel command reverb:start and waits for you to start it."},
		{"[program:a]\ncommand=php artisan queue:work --tries=3\nnumprocs=2\nprocess_name=%(program_name)s_%(process_num)02d\n", "Runs two copies of a Laravel queue worker and starts with supervisord."},
		{"[program:a]\ncommand=php artisan horizon\n", "Runs Laravel Horizon and starts with supervisord."},
		{"[program:a]\ncommand=php artisan schedule:work\n", "Runs Laravel’s scheduler and starts with supervisord."},
		{"[program:a]\ncommand=php /srv/worker.php\n", "Runs a PHP script and starts with supervisord."},
		{"[program:a]\ncommand=/usr/bin/artisanal-bread\n", "Runs the program at /usr/bin/artisanal-bread and starts with supervisord."},
		// The log file is on its own line, not in the one-liner.
		{"[program:a]\ncommand=node server.js\nstdout_logfile=/var/log/a.log\n", "Runs a Node.js app and starts with supervisord."},
		{"[program:a]\nautostart=false\n", "Has no command yet."},
		{"[group:shop]\nprograms=a,b, c\n", "Starts and stops a, b and c together as shop."},
		{"[group:shop]\n", "A group: its programs can be started and stopped together as shop."},
	} {
		ex, err := Explain(c.in, Hints{})
		if err != nil {
			t.Fatal(err)
		}
		if got := ex.Sections[0].Summary; got != c.want {
			t.Errorf("%q: summary = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestExplainFileSummaryIsShort(t *testing.T) {
	ex, err := Explain("[program:a]\ncommand=sleep 1\n[program:b]\ncommand=sleep 1\n[group:g]\nprograms=a,b\n", Hints{})
	if err != nil {
		t.Fatal(err)
	}
	if ex.Summary != "This file sets up two programs." {
		t.Errorf("summary = %q", ex.Summary)
	}
	if ex, _ := Explain("[group:g]\nprograms=a\n", Hints{}); ex.Summary != "This file has no [program:x] section, so it starts nothing." {
		t.Errorf("summary = %q", ex.Summary)
	}
}
