package platform

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestLookPathUsesGivenPath(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "mytool")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	plain := filepath.Join(dir, "notexec")
	if err := os.WriteFile(plain, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := LookPath("mytool", []string{"/nonexistent", dir})
	if err != nil || got != bin {
		t.Fatalf("LookPath(mytool) = %q, %v; want %q", got, err, bin)
	}
	if _, err := LookPath("mytool", []string{"/nonexistent"}); err == nil {
		t.Error("found mytool outside the given path")
	}
	if _, err := LookPath("notexec", []string{dir}); err == nil {
		t.Error("a non-executable file counted as found")
	}
	if got, err := LookPath(bin, nil); err != nil || got != bin {
		t.Errorf("absolute path = %q, %v; want %q", got, err, bin)
	}
	if _, err := LookPath(plain, nil); err == nil {
		t.Error("absolute non-executable file counted as found")
	}
	if _, err := LookPath("", []string{dir}); err == nil {
		t.Error("empty name counted as found")
	}
}

func TestLoginPATHFallsBackOnTimeout(t *testing.T) {
	slow := func(ctx context.Context) (string, error) {
		time.Sleep(3 * time.Second)
		return "/should/not/appear", nil
	}
	start := time.Now()
	got := loginPATH(slow, 50*time.Millisecond)
	if time.Since(start) > time.Second {
		t.Errorf("took %v; the timeout was not enforced", time.Since(start))
	}
	if want := []string{"/opt/homebrew/bin", "/usr/local/bin"}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestLoginPATHMergesAndDeduplicates(t *testing.T) {
	run := func(ctx context.Context) (string, error) {
		return "/usr/local/bin:/home/acme/bin::/usr/bin:/home/acme/bin", nil
	}
	got := loginPATH(run, time.Second)
	want := []string{"/usr/local/bin", "/home/acme/bin", "/usr/bin", "/opt/homebrew/bin"}
	if !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestLoginPATHRunnerError(t *testing.T) {
	run := func(ctx context.Context) (string, error) { return "/x", context.Canceled }
	got := loginPATH(run, time.Second)
	if want := []string{"/opt/homebrew/bin", "/usr/local/bin"}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestExtraPATHPerOS(t *testing.T) {
	none := func(string) bool { return false }
	if got := extraPATH("darwin", none); !slices.Equal(got, []string{"/opt/homebrew/bin", "/usr/local/bin"}) {
		t.Errorf("darwin = %q", got)
	}
	if got := extraPATH("linux", none); !slices.Equal(got, []string{"/usr/local/bin", "/usr/bin", "/bin"}) {
		t.Errorf("linux = %q", got)
	}
	brew := func(p string) bool { return p == "/home/linuxbrew/.linuxbrew/bin" }
	if got := extraPATH("linux", brew); !slices.Equal(got, []string{"/usr/local/bin", "/usr/bin", "/bin", "/home/linuxbrew/.linuxbrew/bin"}) {
		t.Errorf("linux with Homebrew = %q", got)
	}
}

func TestLoginPATHOnLinuxPutsTheShellFirst(t *testing.T) {
	run := func(ctx context.Context) (string, error) { return "/home/ana/.local/bin:/usr/bin", nil }
	got := loginPATHFor(run, time.Second, extraPATH("linux", func(string) bool { return false }))
	if want := []string{"/home/ana/.local/bin", "/usr/bin", "/usr/local/bin", "/bin"}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}
