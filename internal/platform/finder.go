// Package platform holds the few macOS integrations Visor needs outside
// supervisord: showing files in Finder and opening them with their default
// app (logs: the default text editor). It runs /usr/bin/open and nothing
// else.
package platform

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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
	// ErrNotFile: OpenText opens files only, not folders or devices.
	ErrNotFile = errors.New("is not a file, so it cannot open in a text editor")
)

// RevealCmd shows path selected in a Finder window: open -R <path>.
func RevealCmd(path string) *exec.Cmd { return exec.Command("open", "-R", path) }

// OpenCmd opens path with its default app (a folder opens in Finder):
// open <path>.
func OpenCmd(path string) *exec.Cmd { return exec.Command("open", path) }

// OpenTextCmd opens path in the default text editor: open -t <path>.
func OpenTextCmd(path string) *exec.Cmd { return exec.Command("open", "-t", path) }

// Reveal shows path in Finder.
func Reveal(path string) error {
	if err := check(path, false); err != nil {
		return err
	}
	return run(RevealCmd(path))
}

// Open opens path with its default app (a folder opens in Finder). It
// never launches anything: it refuses executable files, and reveals app
// bundles and file types macOS would run (.command, .jar, .pkg, …) in
// Finder instead.
func Open(path string) error {
	cmd, err := openCmdFor(path)
	if err != nil {
		return err
	}
	return run(cmd)
}

// OpenText opens a file (a log) in the default text editor, whatever its
// extension or mode, so no other handler can run it. It refuses folders
// and devices.
func OpenText(path string) error {
	cmd, err := openTextCmdFor(path)
	if err != nil {
		return err
	}
	return run(cmd)
}

func openTextCmdFor(path string) (*exec.Cmd, error) {
	if err := check(path, false); err != nil {
		return nil, err
	}
	if fi, err := os.Stat(path); err != nil || !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s %w", path, ErrNotFile)
	}
	return OpenTextCmd(path), nil
}

// openCmdFor is the command Open runs for path: open <path>, or
// open -R <path> for anything macOS would launch rather than show.
func openCmdFor(path string) (*exec.Cmd, error) {
	if err := check(path, true); err != nil {
		return nil, err
	}
	if launches(path) {
		return RevealCmd(path), nil
	}
	return OpenCmd(path), nil
}

// bundleExts are folders macOS treats as an app or a loadable bundle.
var bundleExts = []string{".app", ".bundle", ".framework", ".plugin", ".prefpane"}

// launchExts are files (or bundle folders) that open runs, installs or
// mounts even without an execute bit.
var launchExts = []string{
	".app", ".command", ".tool", ".terminal", ".jar", ".webloc", ".inetloc", ".fileloc",
	".workflow", ".scpt", ".pkg", ".mpkg", ".dmg", ".action", ".applescript",
	".shortcut", ".mobileconfig", ".saver", ".xpc", ".kext",
}

// launches reports whether opening path would launch something: a bundle
// folder (by extension, or one holding Contents/Info.plist), or a file
// type macOS runs. Execute bits are check's job.
func launches(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	fi, err := os.Stat(path)
	if err == nil && fi.IsDir() {
		if slices.Contains(bundleExts, ext) || slices.Contains(launchExts, ext) {
			return true
		}
		_, err := os.Stat(filepath.Join(path, "Contents", "Info.plist"))
		return err == nil
	}
	return slices.Contains(launchExts, ext)
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
