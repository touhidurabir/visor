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
