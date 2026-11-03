package doctor

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/touhidurabir/visor/internal/config"
)

// StartPlan is how Visor starts a stopped supervisord: Name and Args to
// run, and Command as a person would type it.
type StartPlan struct {
	Name    string
	Args    []string
	Command string
}

// PlanStart picks the start command for d, with supervisord the binary
// Doctor found. Homebrew's own config (<prefix>/etc/supervisord.conf, with
// that prefix's brew and a supervisord from it) goes through `brew services
// start supervisor`, so it also starts after a reboot; anything else runs
// `supervisord -c <config>` once, as this user. Never sudo. brewAt says
// where a Homebrew prefix keeps brew ("" when it has none). ok is false for
// a config that keeps supervisord in the foreground (nodaemon=true, as
// launchd runs it): Visor shows that command but doesn't start it.
func PlanStart(d config.Discovery, supervisord string, brewAt func(prefix string) string) (p StartPlan, ok bool) {
	plain := StartPlan{Name: supervisord, Args: []string{"-c", d.ConfigPath}, Command: "supervisord -c " + shellQuote(d.ConfigPath)}
	if d.NoDaemon {
		return plain, false
	}
	if prefix := homebrewPrefix(d.ConfigPath); prefix != "" && brewAt != nil && under(supervisord, prefix) {
		if brew := brewAt(prefix); brew != "" {
			return StartPlan{Name: brew, Args: []string{"services", "start", "supervisor"}, Command: "brew services start supervisor"}, true
		}
	}
	return plain, true
}

// homebrewPrefix is the Homebrew prefix whose own supervisord.conf path is
// the config path (Apple Silicon, Intel), or "".
func homebrewPrefix(conf string) string {
	for _, prefix := range []string{"/opt/homebrew", "/usr/local"} {
		if conf == prefix+"/etc/supervisord.conf" {
			return prefix
		}
	}
	return ""
}

// under reports whether bin (symlinks resolved when it exists) lives in
// prefix, so that Homebrew's supervisord is the one being started.
func under(bin, prefix string) bool {
	if real, err := filepath.EvalSymlinks(bin); err == nil {
		bin = real
	}
	return strings.HasPrefix(bin, prefix+"/")
}

// shellQuote quotes s for a shell when it needs it, so a shown command can
// be pasted into Terminal.
func shellQuote(s string) string {
	safe := func(r rune) bool {
		return r == '/' || r == '.' || r == '-' || r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
	}
	for _, r := range s {
		if !safe(r) {
			return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
		}
	}
	return s
}

// startTimeout bounds a start (brew services can take a while).
const startTimeout = 60 * time.Second

// RunStart runs p with path as its PATH (the login shell's, so programs
// supervisord starts find Homebrew tools) and returns what it printed. A
// daemon that keeps the output pipes open is not waited for.
func RunStart(ctx context.Context, p StartPlan, path []string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, p.Name, p.Args...)
	cmd.Dir = os.TempDir()
	if len(path) > 0 {
		cmd.Env = append(os.Environ(), "PATH="+strings.Join(path, string(os.PathListSeparator)))
	}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	cmd.WaitDelay = 2 * time.Second // a daemonized child may still hold the pipes
	err := cmd.Run()
	if errors.Is(err, exec.ErrWaitDelay) {
		err = nil // the command exited; only the daemon's pipes were still open
	}
	return strings.TrimSpace(out.String()), err
}

// LogTail is the last n lines of the file at path ("" when it can't be
// read), for showing why supervisord didn't start.
func LogTail(path string, n int) string {
	if path == "" || n <= 0 {
		return ""
	}
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	const window = 64 << 10
	if fi, err := f.Stat(); err == nil && fi.Size() > window {
		if _, err := f.Seek(-window, io.SeekEnd); err != nil {
			return ""
		}
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// LogSize is the size of the file at path (0 when it can't be read), to
// mark where a start begins writing.
func LogSize(path string) int64 {
	if fi, err := os.Stat(path); err == nil {
		return fi.Size()
	}
	return 0
}

// LogSince is the last n lines written to path after offset (from
// LogSize). A file now shorter than offset was rotated: all of it is new.
func LogSince(path string, offset int64, n int) string {
	if path == "" || n <= 0 {
		return ""
	}
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	if fi, err := f.Stat(); err != nil || fi.Size() < offset {
		offset = 0
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return ""
	}
	data, err := io.ReadAll(io.LimitReader(f, 64<<10))
	if err != nil || len(data) == 0 {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
