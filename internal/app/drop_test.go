package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadDropped(t *testing.T) {
	dir := t.TempDir()
	ok := filepath.Join(dir, "worker.conf")
	if err := os.WriteFile(ok, []byte("[program:w]\ncommand=sleep 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	big := filepath.Join(dir, "big.ini")
	if err := os.WriteFile(big, make([]byte, maxImportBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	got := ReadDropped([]string{ok, dir, big, filepath.Join(dir, "gone.ini")})
	if len(got) != 4 {
		t.Fatalf("%d results: %+v", len(got), got)
	}
	if got[0] != (DroppedFileView{Name: "worker.conf", Content: "[program:w]\ncommand=sleep 1\n"}) {
		t.Errorf("file: %+v", got[0])
	}
	for i, want := range map[int]string{1: "folder", 2: "Larger than 1 MB", 3: "Couldn’t read it"} {
		if got[i].Content != "" || !strings.Contains(got[i].Error, want) {
			t.Errorf("%s: %+v, want an error with %q", got[i].Name, got[i], want)
		}
	}

	many := make([]string, maxImportFiles+5)
	for i := range many {
		many[i] = ok
	}
	if n := len(ReadDropped(many)); n != maxImportFiles {
		t.Errorf("read %d files, want at most %d", n, maxImportFiles)
	}
}
