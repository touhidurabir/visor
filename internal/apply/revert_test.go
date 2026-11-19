package apply

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// I-2: a write supervisord refused is undone: the previous content is back,
// and the backup the write took is dropped (nothing changed in the end).
func TestRevertPutsPreviousBack(t *testing.T) {
	f := newFixture(t)
	p := f.file(t, "mailer.ini", prog, 0o640)
	bk, err := Write(Local, p, []byte("rejected\n"), Hash([]byte(prog)), f.bk)
	if err != nil {
		t.Fatal(err)
	}
	if err := Revert(Local, p, []byte(prog), []byte("rejected\n"), bk, f.bk); err != nil {
		t.Fatal(err)
	}
	if got := read(t, p); got != prog {
		t.Errorf("file = %q, want the previous content", got)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v, want 0640 kept", fi.Mode().Perm())
	}
	if list, _ := f.bk.List(p); len(list) != 0 {
		t.Errorf("backups = %+v, want the write's backup dropped", list)
	}
	if got := entries(t, f.conf); !slices.Equal(got, []string{"mailer.ini"}) {
		t.Errorf("folder = %v, want no temp files", got)
	}
}

// An edit made elsewhere since the refused write wins: nothing is reverted.
func TestRevertLeavesNewerEditAlone(t *testing.T) {
	f := newFixture(t)
	p := f.file(t, "mailer.ini", prog, 0o644)
	bk, err := Write(Local, p, []byte("rejected\n"), Hash([]byte(prog)), f.bk)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("theirs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Revert(Local, p, []byte(prog), []byte("rejected\n"), bk, f.bk); !errors.Is(err, ErrChangedOnDisk) {
		t.Fatalf("err = %v, want ErrChangedOnDisk", err)
	}
	if got := read(t, p); got != "theirs\n" {
		t.Errorf("file = %q", got)
	}
	if list, _ := f.bk.List(p); len(list) != 1 {
		t.Errorf("backups = %+v, want the backup kept (it is the only copy of the previous content)", list)
	}
}

func TestRevertCreateRemovesNewFile(t *testing.T) {
	f := newFixture(t)
	p := filepath.Join(f.conf, "new.ini")
	if _, err := Write(Local, p, []byte(prog), "", f.bk); err != nil {
		t.Fatal(err)
	}
	if err := RevertCreate(Local, p, []byte(prog)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(p); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("file still there: %v", err)
	}
	// Someone else's file at that path is never removed.
	other := f.file(t, "other.ini", "theirs\n", 0o644)
	if err := RevertCreate(Local, other, []byte(prog)); !errors.Is(err, ErrChangedOnDisk) {
		t.Errorf("err = %v, want ErrChangedOnDisk", err)
	}
	if got := read(t, other); got != "theirs\n" {
		t.Errorf("other file = %q", got)
	}
}

// I-7: the backup is taken before anything is stopped; a backups folder
// Visor can't write fails there, with the file untouched.
func TestBackUpForRemoveFailsFirst(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write a 0500 folder")
	}
	f := newFixture(t)
	p := f.file(t, "mailer.ini", prog, 0o644)
	if err := os.MkdirAll(f.bk.Dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(f.bk.Dir, 0o700) })
	if _, err := BackUpForRemove(Local, p, f.bk); err == nil {
		t.Fatal("backup into a read-only folder succeeded")
	}
	if got := read(t, p); got != prog {
		t.Errorf("file = %q", got)
	}
}

func TestRemoveBackedUp(t *testing.T) {
	f := newFixture(t)
	p := f.file(t, "mailer.ini", prog, 0o644)
	bk, err := BackUpForRemove(Local, p, f.bk)
	if err != nil {
		t.Fatal(err)
	}
	if !bk.Removed {
		t.Errorf("backup = %+v, want Removed", bk)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("the backup alone removed the file: %v", err)
	}
	// Edited between the backup and the removal: the backup follows.
	if err := os.WriteFile(p, []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := RemoveBackedUp(Local, p, bk, f.bk)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("file still there: %v", err)
	}
	list, _ := f.bk.List(p)
	if len(list) != 1 || list[0].ID != got.ID || !list[0].Removed {
		t.Fatalf("backups = %+v, want one removed copy", list)
	}
	if data, _ := f.bk.Read(list[0]); string(data) != "edited\n" {
		t.Errorf("backup = %q, want the content at removal", data)
	}
	// Discard drops a backup taken for a removal that did not happen.
	q := f.file(t, "other.ini", prog, 0o644)
	bk, err = BackUpForRemove(Local, q, f.bk)
	if err != nil {
		t.Fatal(err)
	}
	f.bk.Discard(bk)
	if list, _ := f.bk.List(q); len(list) != 0 {
		t.Errorf("backups = %+v after Discard", list)
	}
}

// I-5: Removed lists the files whose newest backup is a removal and that
// are not back on disk, newest first.
func TestBackupsRemoved(t *testing.T) {
	f := newFixture(t)
	a := f.file(t, "a.ini", "a\n", 0o644)
	b := f.file(t, "b.ini", "b\n", 0o644)
	c := f.file(t, "c.ini", "c\n", 0o644)
	if _, err := Remove(Local, a, f.bk); err != nil {
		t.Fatal(err)
	}
	bb, err := Remove(Local, b, f.bk)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Write(Local, c, []byte("c2\n"), Hash([]byte("c\n")), f.bk); err != nil {
		t.Fatal(err)
	}
	got, err := f.bk.Removed(Local)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Path != b || got[0].ID != bb.ID || got[1].Path != a {
		t.Fatalf("Removed = %+v, want b then a", got)
	}
	// Brought back (or a new file at that path): no longer removed.
	if err := os.WriteFile(a, []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, _ = f.bk.Removed(Local)
	if len(got) != 1 || got[0].Path != b {
		t.Errorf("Removed = %+v, want only b", got)
	}
	empty := Backups{Dir: filepath.Join(t.TempDir(), "none")}
	if got, err := empty.Removed(Local); err != nil || len(got) != 0 {
		t.Errorf("no backups folder: %+v, %v", got, err)
	}
}
