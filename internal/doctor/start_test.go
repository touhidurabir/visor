package doctor

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/touhidurabir/visor/internal/config"
)

func TestPlanStartUsesHomebrewForItsOwnConfig(t *testing.T) {
	brewAt := func(prefix string) string { return prefix + "/bin/brew" } // every prefix has brew
	brewConf := config.Discovery{ConfigPath: "/opt/homebrew/etc/supervisord.conf"}
	p, ok := PlanStart(brewConf, "/opt/homebrew/Cellar/supervisor/4.2.5/bin/supervisord", brewAt)
	if !ok || p.Name != "/opt/homebrew/bin/brew" || !reflect.DeepEqual(p.Args, []string{"services", "start", "supervisor"}) || p.Command != "brew services start supervisor" {
		t.Errorf("Homebrew config: %+v", p)
	}
	for _, tc := range []struct{ name, conf, bin string }{
		// Intel Homebrew's config on Apple Silicon with both installed: its own brew, never /opt/homebrew's.
		{"other prefix", "/usr/local/etc/supervisord.conf", "/usr/local/Cellar/supervisor/4.2.5/bin/supervisord"},
	} {
		p, _ := PlanStart(config.Discovery{ConfigPath: tc.conf}, tc.bin, brewAt)
		if p.Name != "/usr/local/bin/brew" {
			t.Errorf("%s: %+v", tc.name, p)
		}
	}
	// supervisord not from that Homebrew (pip), or no brew there, or another config: supervisord -c.
	for _, tc := range []struct {
		name, conf, bin string
		brewAt          func(string) string
	}{
		{"pip install", "/usr/local/etc/supervisord.conf", "/Users/me/.venv/bin/supervisord", brewAt},
		{"no brew", "/opt/homebrew/etc/supervisord.conf", "/opt/homebrew/bin/supervisord", func(string) string { return "" }},
		{"own config", "/Users/me/sv/supervisord.conf", "/opt/homebrew/bin/supervisord", brewAt},
	} {
		p, ok := PlanStart(config.Discovery{ConfigPath: tc.conf}, tc.bin, tc.brewAt)
		if !ok || p.Name != tc.bin || !reflect.DeepEqual(p.Args, []string{"-c", tc.conf}) || p.Command != "supervisord -c "+tc.conf {
			t.Errorf("%s: %+v", tc.name, p)
		}
	}
}

func TestPlanStartQuotesTheShownCommand(t *testing.T) {
	p, _ := PlanStart(config.Discovery{ConfigPath: "/Users/me/My Configs/supervisord.conf"}, "/bin/supervisord", nil)
	if p.Command != "supervisord -c '/Users/me/My Configs/supervisord.conf'" {
		t.Errorf("command %q", p.Command)
	}
	if p.Args[1] != "/Users/me/My Configs/supervisord.conf" {
		t.Errorf("args %q must stay unquoted", p.Args)
	}
}

// nodaemon=true keeps supervisord in the foreground (launchd runs it so):
// Visor doesn't start it, it shows the command.
func TestPlanStartRefusesAForegroundConfig(t *testing.T) {
	p, ok := PlanStart(config.Discovery{ConfigPath: "/Users/me/sv/supervisord.conf", NoDaemon: true}, "/opt/homebrew/bin/supervisord", nil)
	if ok || p.Command != "supervisord -c /Users/me/sv/supervisord.conf" {
		t.Errorf("PlanStart = %+v, %v", p, ok)
	}
}

func TestRunStartReportsWhatTheCommandSaid(t *testing.T) {
	p := StartPlan{Name: "/bin/sh", Args: []string{"-c", "echo 'Error: bad line 3' >&2; exit 2"}}
	out, err := RunStart(context.Background(), p, nil)
	if err == nil || !strings.Contains(out, "Error: bad line 3") {
		t.Fatalf("RunStart = %q, %v", out, err)
	}
}

// supervisord daemonizes: its child may hold the output pipes open after
// the parent exits. RunStart must not wait for it.
func TestRunStartDoesNotWaitForADaemon(t *testing.T) {
	p := StartPlan{Name: "/bin/sh", Args: []string{"-c", "(sleep 30 &) ; echo started"}}
	begin := time.Now()
	out, err := RunStart(context.Background(), p, nil)
	if err != nil || !strings.Contains(out, "started") {
		t.Fatalf("RunStart = %q, %v", out, err)
	}
	if time.Since(begin) > 5*time.Second {
		t.Errorf("waited %s for the daemon", time.Since(begin))
	}
}

func TestLogTail(t *testing.T) {
	p := filepath.Join(t.TempDir(), "supervisord.log")
	var b strings.Builder
	for i := 1; i <= 50; i++ {
		b.WriteString("line ")
		b.WriteString(strings.Repeat("x", i%3))
		b.WriteString("\n")
	}
	b.WriteString("CRIT the last line\n")
	if err := os.WriteFile(p, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	got := LogTail(p, 3)
	if lines := strings.Split(got, "\n"); len(lines) != 3 || lines[2] != "CRIT the last line" {
		t.Errorf("LogTail = %q", got)
	}
	if LogTail(filepath.Join(t.TempDir(), "missing.log"), 3) != "" || LogTail("", 3) != "" {
		t.Error("a missing log should read as empty")
	}
}

// Only what was written after the start: an old run's lines don't explain
// this failure.
func TestLogSince(t *testing.T) {
	p := filepath.Join(t.TempDir(), "supervisord.log")
	if err := os.WriteFile(p, []byte("old 1\nold 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mark := LogSize(p)
	if got := LogSince(p, mark, 20); got != "" {
		t.Errorf("nothing new: %q", got)
	}
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.WriteString("new 1\nCRIT new 2\n")
	_ = f.Close()
	if got := LogSince(p, mark, 20); got != "new 1\nCRIT new 2" {
		t.Errorf("LogSince = %q", got)
	}
	if got := LogSince(p, 1<<20, 20); got != "new 1\nCRIT new 2" && got != "old 1\nold 2\nnew 1\nCRIT new 2" {
		t.Errorf("a log that shrank (rotated) = %q", got)
	}
}
