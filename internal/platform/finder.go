// Package platform holds the few macOS integrations Visor needs outside
// supervisord: showing files in Finder and opening them with their default
// app. It runs /usr/bin/open and nothing else.
package platform

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var (
	// ErrNotAbsolute: only absolute paths are opened, so a path can never
	// be read as an option of open(1).
	ErrNotAbsolute = errors.New("not an absolute path")
	// ErrMissing: the path does not exist.
	ErrMissing = errors.New("does not exist")
	// ErrExecutable: opening an executable file with its default app would
	// run it, so Open refuses; Reveal still works.
	ErrExecutable = errors.New("is a program, so Visor will not open it; use Reveal in Finder instead")
)

// RevealCmd shows path selected in a Finder window: open -R <path>.
func RevealCmd(path string) *exec.Cmd { return exec.Command("open", "-R", path) }

// OpenCmd opens path with its default app (a folder opens in Finder):
// open <path>.
func OpenCmd(path string) *exec.Cmd { return exec.Command("open", path) }

// Reveal shows path in Finder.
func Reveal(path string) error {
	if err := check(path, false); err != nil {
		return err
	}
	return run(RevealCmd(path))
}

// Open opens path with its default app. It refuses executable files.
func Open(path string) error {
	if err := check(path, true); err != nil {
		return err
	}
	return run(OpenCmd(path))
}

// check rejects paths that open(1) should not get: relative ones (which
// includes anything starting with "-"), missing ones, and, when opening,
// regular files with an execute bit.
func check(path string, open bool) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("cannot open %q: %w", path, ErrNotAbsolute)
	}
	fi, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("%s %w", path, ErrMissing)
	}
	if open && fi.Mode().IsRegular() && fi.Mode().Perm()&0o111 != 0 {
		return fmt.Errorf("%s %w", path, ErrExecutable)
	}
	return nil
}

func run(cmd *exec.Cmd) error {
	out, err := cmd.CombinedOutput()
	if err != nil {
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return fmt.Errorf("open %s: %s", cmd.Args[len(cmd.Args)-1], msg)
		}
		return fmt.Errorf("open %s: %w", cmd.Args[len(cmd.Args)-1], err)
	}
	return nil
}
