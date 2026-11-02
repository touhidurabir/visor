package platform

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestRevealCmdArgs(t *testing.T) {
	cmd := RevealCmd("/tmp/some dir/app.log")
	if want := []string{"open", "-R", "/tmp/some dir/app.log"}; !slices.Equal(cmd.Args, want) {
		t.Errorf("args = %q, want %q", cmd.Args, want)
	}
}

func TestOpenCmdArgs(t *testing.T) {
	cmd := OpenCmd("/tmp/app.log")
	if want := []string{"open", "/tmp/app.log"}; !slices.Equal(cmd.Args, want) {
		t.Errorf("args = %q, want %q", cmd.Args, want)
	}
}

// check runs before anything is exec'd, so these never open Finder.
func TestCheckRejectsBadPaths(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "run.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, path string
		open       bool
		want       error
	}{
		{"relative", "logs/app.log", false, ErrNotAbsolute},
		{"option-like", "-a", true, ErrNotAbsolute},
		{"empty", "", false, ErrNotAbsolute},
		{"missing", filepath.Join(dir, "gone.log"), false, ErrMissing},
		{"open runs an executable", script, true, ErrExecutable},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := check(c.path, c.open); !errors.Is(err, c.want) {
				t.Errorf("check(%q) = %v, want %v", c.path, err, c.want)
			}
		})
	}
	// Revealing an executable is fine, and so is opening a folder (its
	// execute bits mean "searchable").
	if err := check(script, false); err != nil {
		t.Errorf("reveal executable: %v", err)
	}
	if err := check(dir, true); err != nil {
		t.Errorf("open folder: %v", err)
	}
}

// openCmdFor never runs anything that launches: bundles and files macOS
// runs without an execute bit are revealed in Finder instead.
func TestOpenCmdForRevealsLaunchables(t *testing.T) {
	dir := t.TempDir()
	mk := func(name string, isDir bool) string {
		p := filepath.Join(dir, name)
		var err error
		if isDir {
			err = os.MkdirAll(p, 0o755)
		} else {
			err = os.WriteFile(p, []byte("x"), 0o644)
		}
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	hidden := mk("Plain Folder", true) // a bundle without an extension
	if err := os.MkdirAll(filepath.Join(hidden, "Contents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hidden, "Contents", "Info.plist"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		path   string
		reveal bool
	}{
		{"log file", mk("app.log", false), false},
		{"config file", mk("mailer.ini", false), false},
		{"plain folder", mk("logs", true), false},
		{"app bundle", mk("Foo.app", true), true},
		{"upper-case bundle", mk("Bar.APP", true), true},
		{"bundle", mk("x.bundle", true), true},
		{"framework", mk("x.framework", true), true},
		{"plugin", mk("x.plugin", true), true},
		{"prefPane", mk("x.prefPane", true), true},
		{"dir with Contents/Info.plist", hidden, true},
		{"workflow dir", mk("x.workflow", true), true},
		{"command", mk("run.command", false), true},
		{"tool", mk("run.tool", false), true},
		{"terminal", mk("x.terminal", false), true},
		{"jar", mk("x.jar", false), true},
		{"webloc", mk("x.webloc", false), true},
		{"inetloc", mk("x.inetloc", false), true},
		{"fileloc", mk("x.fileloc", false), true},
		{"workflow file", mk("y.workflow", false), true},
		{"script", mk("x.scpt", false), true},
		{"pkg", mk("x.pkg", false), true},
		{"mpkg", mk("x.mpkg", false), true},
		{"dmg", mk("x.dmg", false), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cmd, err := openCmdFor(c.path)
			if err != nil {
				t.Fatal(err)
			}
			want := []string{"open", c.path}
			if c.reveal {
				want = []string{"open", "-R", c.path}
			}
			if !slices.Equal(cmd.Args, want) {
				t.Errorf("args = %q, want %q", cmd.Args, want)
			}
		})
	}
	// The existing checks still come first.
	if _, err := openCmdFor("relative.log"); !errors.Is(err, ErrNotAbsolute) {
		t.Errorf("relative: %v", err)
	}
	exe := filepath.Join(dir, "run.sh")
	if err := os.WriteFile(exe, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := openCmdFor(exe); !errors.Is(err, ErrExecutable) {
		t.Errorf("executable: %v", err)
	}
}

func TestOpenTextCmdArgs(t *testing.T) {
	cmd := OpenTextCmd("/tmp/some dir/app.log")
	if want := []string{"open", "-t", "/tmp/some dir/app.log"}; !slices.Equal(cmd.Args, want) {
		t.Errorf("args = %q, want %q", cmd.Args, want)
	}
}

// M8: a log file always opens in the default text editor, whatever its
// extension or mode, so no handler (Python Launcher for .py, Terminal for
// .command) ever gets to run it.
func TestOpenTextCmdForAlwaysUsesTheTextEditor(t *testing.T) {
	dir := t.TempDir()
	for _, c := range []struct {
		name string
		mode os.FileMode
	}{
		{"app.log", 0o644},
		{"worker.py", 0o644},
		{"run.command", 0o644},
		{"x.jar", 0o644},
		{"exec.log", 0o755},
	} {
		p := filepath.Join(dir, c.name)
		if err := os.WriteFile(p, []byte("x"), c.mode); err != nil {
			t.Fatal(err)
		}
		cmd, err := openTextCmdFor(p)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if want := []string{"open", "-t", p}; !slices.Equal(cmd.Args, want) {
			t.Errorf("%s: args = %q, want %q", c.name, cmd.Args, want)
		}
	}
	for _, c := range []struct {
		name, path string
		want       error
	}{
		{"relative", "logs/app.log", ErrNotAbsolute},
		{"option-like", "-a", ErrNotAbsolute},
		{"missing", filepath.Join(dir, "gone.log"), ErrMissing},
		{"folder", dir, ErrNotFile},
		{"device", "/dev/null", ErrNotFile},
	} {
		if _, err := openTextCmdFor(c.path); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
	}
}

func TestNotificationSettingsCmdArgs(t *testing.T) {
	got := NotificationSettingsCmd().Args
	want := []string{"open", "x-apple.systempreferences:com.apple.Notifications-Settings.extension"}
	if !slices.Equal(got, want) {
		t.Fatalf("args = %q, want %q", got, want)
	}
}
