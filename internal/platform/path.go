package platform

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// A GUI app inherits a minimal PATH, not the one the user's shell sets up
// (macOS gives it /usr/bin:/bin:/usr/sbin:/sbin; a Linux desktop's session
// may lack ~/.local/bin), so command binaries are resolved against the login
// shell's PATH.

const loginPATHTimeout = 2 * time.Second

var (
	loginOnce sync.Once
	loginVal  []string
)

// LoginPATH returns the directories on the user's login-shell PATH, then
// the OS's usual places (extraPATH), de-duplicated. It runs
// $SHELL -l -c 'printf %s "$PATH"' once (2 s timeout) and caches the result;
// if the shell fails or is too slow, only the usual places are returned.
func LoginPATH() []string {
	loginOnce.Do(func() { loginVal = loginPATHFor(shellPATH, loginPATHTimeout, extraPATH(runtime.GOOS, dirExists)) })
	return append([]string(nil), loginVal...)
}

// extraPATH is where supervisord and the commands it runs usually are
// beyond the login shell's PATH: Homebrew's two prefixes on macOS; on
// Linux the system dirs, and Homebrew on Linux when it is there.
func extraPATH(goos string, exists func(string) bool) []string {
	if goos == "darwin" {
		return []string{"/opt/homebrew/bin", "/usr/local/bin"}
	}
	dirs := []string{"/usr/local/bin", "/usr/bin", "/bin"}
	if brew := "/home/linuxbrew/.linuxbrew/bin"; exists(brew) {
		dirs = append(dirs, brew)
	}
	return dirs
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func shellPATH(ctx context.Context) (string, error) {
	sh := os.Getenv("SHELL")
	if sh == "" {
		sh = "/bin/zsh"
		if runtime.GOOS != "darwin" {
			sh = "/bin/sh"
		}
	}
	out, err := exec.CommandContext(ctx, sh, "-l", "-c", `printf %s "$PATH"`).Output()
	return string(out), err
}

// loginPATH is macOS's LoginPATH with the shell and timeout injected.
func loginPATH(run func(context.Context) (string, error), timeout time.Duration) []string {
	return loginPATHFor(run, timeout, extraPATH("darwin", dirExists))
}

// loginPATHFor is LoginPATH with the shell, timeout and usual places
// injected. The runner may ignore its context; the timeout is enforced here
// regardless.
func loginPATHFor(run func(context.Context) (string, error), timeout time.Duration, extra []string) []string {
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
	for _, d := range append(strings.Split(shell, ":"), extra...) {
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
