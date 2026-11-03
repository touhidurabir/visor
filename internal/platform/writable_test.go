//go:build unix

package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDirWritable(t *testing.T) {
	dir := t.TempDir()
	if !DirWritable(dir) {
		t.Error("a temp dir reads as not writable")
	}
	locked := filepath.Join(dir, "locked")
	if err := os.Mkdir(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
	if os.Geteuid() != 0 && DirWritable(locked) {
		t.Error("a read-only dir reads as writable")
	}
	if DirWritable(filepath.Join(dir, "missing")) || DirWritable("") {
		t.Error("a missing dir reads as writable")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("checking left files behind: %v", entries)
	}
}
