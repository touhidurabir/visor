package apply

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fixture is a config folder and a state folder, both temporary.
type fixture struct {
	conf string // include folder
	bk   Backups
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	return fixture{conf: t.TempDir(), bk: Backups{Dir: filepath.Join(t.TempDir(), "backups")}}
}

func (f fixture) file(t *testing.T, name, content string, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(f.conf, name)
	if err := os.WriteFile(p, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil { // umask may have narrowed it
		t.Fatal(err)
	}
	return p
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// entries lists a folder's names.
func entries(t *testing.T, dir string) []string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out
}

// backupFiles lists every file under the backups folder, at any depth.
func backupFiles(t *testing.T, b Backups) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(b.Dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if !d.IsDir() {
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

const prog = "[program:mailer]\ncommand=php artisan queue:work\n"

func TestWriteNewFile(t *testing.T) {
	f := newFixture(t)
	p := filepath.Join(f.conf, "mailer.ini")
	bk, err := Write(Local, p, []byte(prog), "", f.bk)
	if err != nil {
		t.Fatal(err)
	}
	if bk != (Backup{}) {
		t.Errorf("backup = %+v, want none for a new file", bk)
	}
	if got := read(t, p); got != prog {
		t.Errorf("content = %q", got)
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o644 {
		t.Errorf("mode = %v, want 0644", fi.Mode().Perm())
	}
	if got := entries(t, f.conf); !slices.Equal(got, []string{"mailer.ini"}) {
		t.Errorf("config folder = %v, want only mailer.ini (no temp files)", got)
	}
	if got := backupFiles(t, f.bk); len(got) != 0 {
		t.Errorf("backups = %v, want none", got)
	}
}

func TestWriteNewFileExists(t *testing.T) {
	f := newFixture(t)
	p := f.file(t, "mailer.ini", "theirs\n", 0o644)
	_, err := Write(Local, p, []byte(prog), "", f.bk)
	if !errors.Is(err, ErrExists) {
		t.Fatalf("err = %v, want ErrExists", err)
	}
	if got := read(t, p); got != "theirs\n" {
		t.Errorf("content = %q, want it untouched", got)
	}
	if got := entries(t, f.conf); !slices.Equal(got, []string{"mailer.ini"}) {
		t.Errorf("config folder = %v", got)
	}
	if got := backupFiles(t, f.bk); len(got) != 0 {
		t.Errorf("backups = %v, want none", got)
	}
}

// Review Focus 5: a file edited outside Visor while the editor is open.
func TestSaveRejectsStaleHash(t *testing.T) {
	f := newFixture(t)
	p := f.file(t, "mailer.ini", prog, 0o644)
	opened := Hash([]byte(prog))
	if err := os.WriteFile(p, []byte(prog+"numprocs=2\n"), 0o644); err != nil { // someone else's edit
		t.Fatal(err)
	}
	_, err := Write(Local, p, []byte(prog+"autostart=false\n"), opened, f.bk)
	if !errors.Is(err, ErrChangedOnDisk) {
		t.Fatalf("err = %v, want ErrChangedOnDisk", err)
	}
	if got := read(t, p); got != prog+"numprocs=2\n" {
		t.Errorf("content = %q, want the other edit kept", got)
	}
	if got := backupFiles(t, f.bk); len(got) != 0 {
		t.Errorf("backups = %v, want none", got)
	}
	if got := entries(t, f.conf); !slices.Equal(got, []string{"mailer.ini"}) {
		t.Errorf("config folder = %v", got)
	}

	// Deleted on disk while open: also a change on disk, and nothing is
	// written back.
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if _, err := Write(Local, p, []byte(prog), opened, f.bk); !errors.Is(err, ErrChangedOnDisk) {
		t.Fatalf("deleted file: err = %v, want ErrChangedOnDisk", err)
	}
	if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("deleted file was written back: %v", err)
	}
}

func TestWriteBacksUpFirst(t *testing.T) {
	f := newFixture(t)
	p := f.file(t, "mailer.ini", prog, 0o644)
	next := prog + "numprocs=2\nprocess_name=%(program_name)s_%(process_num)02d\n"
	bk, err := Write(Local, p, []byte(next), Hash([]byte(prog)), f.bk)
	if err != nil {
		t.Fatal(err)
	}
	if bk.ID == "" || bk.Path != p || bk.Removed || bk.At.IsZero() {
		t.Errorf("backup = %+v", bk)
	}
	if got := read(t, p); got != next {
		t.Errorf("content = %q", got)
	}
	list, err := f.bk.List(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != bk.ID || !list[0].At.Equal(bk.At) || list[0].Path != p {
		t.Fatalf("List = %+v, want [%+v]", list, bk)
	}
	old, err := f.bk.Read(list[0])
	if err != nil || string(old) != prog {
		t.Errorf("Read = %q, %v; want the content before the write", old, err)
	}
	// The backup lives outside the include folder, with the original path
	// recorded next to it.
	if strings.HasPrefix(f.bk.Dir, f.conf) {
		t.Fatalf("backups inside the config folder")
	}
	if got := entries(t, f.conf); !slices.Equal(got, []string{"mailer.ini"}) {
		t.Errorf("config folder = %v", got)
	}
	var m struct{ Path string }
	raw, err := os.ReadFile(filepath.Join(f.bk.folder(p), "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &m); err != nil || m.Path != p {
		t.Errorf("meta.json = %s, %v", raw, err)
	}
	for _, bf := range backupFiles(t, f.bk) {
		fi, err := os.Stat(bf)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("%s mode = %v, want 0600 (configs may hold secrets)", bf, fi.Mode().Perm())
		}
	}
}

func TestWriteKeepsMode(t *testing.T) {
	f := newFixture(t)
	p := f.file(t, "mailer.ini", prog, 0o640)
	if _, err := Write(Local, p, []byte(prog+"autostart=false\n"), Hash([]byte(prog)), f.bk); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v, want 0640", fi.Mode().Perm())
	}
}

// A config that is a symlink (e.g. into a dotfiles repo) stays a symlink:
// the write goes to the file it points at.
func TestWriteThroughSymlink(t *testing.T) {
	f := newFixture(t)
	real := filepath.Join(t.TempDir(), "mailer.ini")
	if err := os.WriteFile(real, []byte(prog), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(f.conf, "mailer.ini")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Write(Local, link, []byte(prog+"autostart=false\n"), Hash([]byte(prog)), f.bk); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("link replaced: %v, %v", fi, err)
	}
	if got := read(t, real); got != prog+"autostart=false\n" {
		t.Errorf("target = %q", got)
	}
	if list, _ := f.bk.List(link); len(list) != 1 {
		t.Errorf("backups for the link path = %+v", list)
	}
}

// Review Focus 4: a root-owned include folder.
func TestSaveReadOnlyDirFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write a 0500 folder")
	}
	f := newFixture(t)
	p := f.file(t, "mailer.ini", prog, 0o644)
	if err := os.Chmod(f.conf, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(f.conf, 0o700) })

	_, err := Write(Local, p, []byte(prog+"autostart=false\n"), Hash([]byte(prog)), f.bk)
	if !errors.Is(err, ErrNotWritable) {
		t.Fatalf("existing file: err = %v, want ErrNotWritable", err)
	}
	_, err = Write(Local, filepath.Join(f.conf, "new.ini"), []byte(prog), "", f.bk)
	if !errors.Is(err, ErrNotWritable) {
		t.Fatalf("new file: err = %v, want ErrNotWritable", err)
	}
	if got := read(t, p); got != prog {
		t.Errorf("content = %q, want it untouched", got)
	}
	if got := entries(t, f.conf); !slices.Equal(got, []string{"mailer.ini"}) {
		t.Errorf("config folder = %v, want no temp or partial file", got)
	}
	if got := backupFiles(t, f.bk); len(got) != 0 {
		t.Errorf("orphan backups = %v", got)
	}
	if err := CheckRemovable(p); !errors.Is(err, ErrNotWritable) {
		t.Errorf("CheckRemovable: err = %v, want ErrNotWritable", err)
	}
	if _, err := Remove(Local, p, f.bk); !errors.Is(err, ErrNotWritable) {
		t.Fatalf("Remove: err = %v, want ErrNotWritable", err)
	}
	if got := read(t, p); got != prog {
		t.Errorf("after Remove, content = %q, want it kept", got)
	}
	if got := backupFiles(t, f.bk); len(got) != 0 {
		t.Errorf("orphan backups after Remove = %v", got)
	}
}

// A read-only file in a writable folder is not replaced either: someone
// made it read-only on purpose.
func TestWriteReadOnlyFileFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write a 0444 file")
	}
	f := newFixture(t)
	p := f.file(t, "mailer.ini", prog, 0o444)
	_, err := Write(Local, p, []byte(prog+"autostart=false\n"), Hash([]byte(prog)), f.bk)
	if !errors.Is(err, ErrNotWritable) {
		t.Fatalf("err = %v, want ErrNotWritable", err)
	}
	if got := read(t, p); got != prog {
		t.Errorf("content = %q", got)
	}
	if got := backupFiles(t, f.bk); len(got) != 0 {
		t.Errorf("orphan backups = %v", got)
	}
	if got := entries(t, f.conf); !slices.Equal(got, []string{"mailer.ini"}) {
		t.Errorf("config folder = %v", got)
	}
}

func TestRemoveMovesToBackups(t *testing.T) {
	f := newFixture(t)
	p := f.file(t, "mailer.ini", prog, 0o644)
	if err := CheckRemovable(p); err != nil {
		t.Fatalf("CheckRemovable: %v", err)
	}
	bk, err := Remove(Local, p, f.bk)
	if err != nil {
		t.Fatal(err)
	}
	if !bk.Removed || bk.Path != p || bk.ID == "" {
		t.Errorf("backup = %+v", bk)
	}
	if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("file still there: %v", err)
	}
	list, err := f.bk.List(p)
	if err != nil || len(list) != 1 || !list[0].Removed || list[0].ID != bk.ID {
		t.Fatalf("List = %+v, %v", list, err)
	}
	if got, err := f.bk.Read(list[0]); err != nil || string(got) != prog {
		t.Errorf("Read = %q, %v", got, err)
	}
	// Writing it back is a new file again.
	if _, err := Write(Local, p, []byte(prog), "", f.bk); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if err := CheckRemovable(filepath.Join(f.conf, "missing.ini")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("CheckRemovable of a missing file: err = %v, want ErrNotExist", err)
	}
	if _, err := Remove(Local, filepath.Join(f.conf, "missing.ini"), f.bk); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Remove of a missing file: err = %v, want ErrNotExist", err)
	}
}

func TestBackupsListNewestFirst(t *testing.T) {
	f := newFixture(t)
	p := f.file(t, "mailer.ini", "v1\n", 0o644)
	other := f.file(t, "other.ini", "x\n", 0o644)
	var made []Backup
	for i, next := range []string{"v2\n", "v3\n", "v4\n"} {
		old := []string{"v1\n", "v2\n", "v3\n"}[i]
		bk, err := Write(Local, p, []byte(next), Hash([]byte(old)), f.bk)
		if err != nil {
			t.Fatal(err)
		}
		made = append(made, bk)
	}
	if _, err := f.bk.Save(other, false); err != nil {
		t.Fatal(err)
	}
	list, err := f.bk.List(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("List = %+v, want 3 (not other.ini's)", list)
	}
	for i, bk := range list {
		want := made[len(made)-1-i]
		if bk.ID != want.ID {
			t.Errorf("List[%d] = %s, want %s", i, bk.ID, want.ID)
		}
		got, err := f.bk.Read(bk)
		if wantContent := []string{"v3\n", "v2\n", "v1\n"}[i]; err != nil || string(got) != wantContent {
			t.Errorf("Read(List[%d]) = %q, %v; want %q", i, got, err, wantContent)
		}
		if i > 0 && !list[i-1].At.After(bk.At) {
			t.Errorf("List[%d].At %v not after List[%d].At %v", i-1, list[i-1].At, i, bk.At)
		}
	}
	if list, err := f.bk.List(filepath.Join(f.conf, "never.ini")); err != nil || len(list) != 0 {
		t.Errorf("List of a file without backups = %+v, %v", list, err)
	}
}

func TestBackupIDsUniqueWithinOneTick(t *testing.T) {
	f := newFixture(t)
	p := f.file(t, "mailer.ini", prog, 0o644)
	seen := map[string]bool{}
	for range 50 {
		bk, err := f.bk.Save(p, false)
		if err != nil {
			t.Fatal(err)
		}
		if seen[bk.ID] {
			t.Fatalf("duplicate ID %s", bk.ID)
		}
		seen[bk.ID] = true
	}
	if list, _ := f.bk.List(p); len(list) != 50 {
		t.Errorf("List has %d, want 50", len(list))
	}
}

func TestReadRejectsForeignBackup(t *testing.T) {
	f := newFixture(t)
	p := f.file(t, "mailer.ini", prog, 0o644)
	if _, err := f.bk.Read(Backup{ID: "../../etc/passwd", Path: p}); err == nil {
		t.Error("Read accepted an ID that is not a backup ID")
	}
	if _, err := f.bk.Read(Backup{ID: "2026-11-01T00-00-00.000000000Z", Path: p}); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Read of an unknown backup: err = %v, want ErrNotExist", err)
	}
}

func TestWriteRejectsRelativePath(t *testing.T) {
	f := newFixture(t)
	if _, err := Write(Local, "mailer.ini", []byte(prog), "", f.bk); err == nil {
		t.Error("Write accepted a relative path")
	}
	if _, err := Remove(Local, "mailer.ini", f.bk); err == nil {
		t.Error("Remove accepted a relative path")
	}
}

func TestCheckWritable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write anything")
	}
	f := newFixture(t)
	p := f.file(t, "mailer.ini", prog, 0o644)
	if err := CheckWritable(p); err != nil {
		t.Fatalf("writable file: %v", err)
	}
	if err := os.Chmod(p, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := CheckWritable(p); !errors.Is(err, ErrNotWritable) {
		t.Errorf("read-only file: err = %v, want ErrNotWritable", err)
	}
	if err := os.Chmod(p, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(f.conf, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(f.conf, 0o700) })
	if err := CheckWritable(p); !errors.Is(err, ErrNotWritable) {
		t.Errorf("read-only folder: err = %v, want ErrNotWritable", err)
	}
}
