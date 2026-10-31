package platform

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// A GUI app on macOS inherits a minimal PATH, not the one the user's shell
// sets up, so command binaries are resolved against the login shell's PATH.

const loginPATHTimeout = 2 * time.Second

var (
	loginOnce sync.Once
	loginVal  []string
)

// LoginPATH returns the directories on the user's login-shell PATH, plus
// /opt/homebrew/bin and /usr/local/bin, de-duplicated. It runs
// $SHELL -l -c 'printf %s "$PATH"' once (2 s timeout) and caches the result;
// if the shell fails or is too slow, only the two Homebrew directories are
// returned.
func LoginPATH() []string {
	loginOnce.Do(func() { loginVal = loginPATH(shellPATH, loginPATHTimeout) })
	return append([]string(nil), loginVal...)
}

func shellPATH(ctx context.Context) (string, error) {
	sh := os.Getenv("SHELL")
	if sh == "" {
		sh = "/bin/zsh"
	}
	out, err := exec.CommandContext(ctx, sh, "-l", "-c", `printf %s "$PATH"`).Output()
	return string(out), err
}

// loginPATH is LoginPATH with the shell and timeout injected. The runner may
// ignore its context; the timeout is enforced here regardless.
func loginPATH(run func(context.Context) (string, error), timeout time.Duration) []string {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	type result struct {
		path string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		p, err := run(ctx)
		ch <- result{p, err}
	}()
	var shell string
	select {
	case r := <-ch:
		if r.err == nil {
			shell = r.path
		}
	case <-ctx.Done():
	}
	seen := map[string]bool{}
	var out []string
	for _, d := range append(strings.Split(shell, ":"), "/opt/homebrew/bin", "/usr/local/bin") {
		if d = strings.TrimSpace(d); d != "" && !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	return out
}

// LookPath finds an executable file. A name with a slash is checked as is;
// otherwise each directory in path is searched in order.
func LookPath(bin string, path []string) (string, error) {
	if bin == "" {
		return "", fmt.Errorf("empty command name")
	}
	if strings.Contains(bin, "/") {
		if err := executable(bin); err != nil {
			return "", err
		}
		return bin, nil
	}
	for _, d := range path {
		p := filepath.Join(d, bin)
		if executable(p) == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("%s: %w", bin, exec.ErrNotFound)
}

func executable(p string) error {
	fi, err := os.Stat(p)
	if err != nil {
		return err
	}
	if fi.IsDir() || fi.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("%s: not an executable file", p)
	}
	return nil
}
