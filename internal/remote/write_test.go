package remote_test

import (
	"errors"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"

	"github.com/touhidurabir/visor/internal/apply"
	"github.com/touhidurabir/visor/internal/remote"
	"github.com/touhidurabir/visor/internal/remote/remotetest"
)

// The server's disk (through the bridge) behaves like this one for every
// write apply makes.
func TestDisksWriteAlike(t *testing.T) {
	needPython(t)
	c, _ := connectOpts(t, remotetest.Options{})
	tr := &remote.BridgeTransport{Conn: c}
	defer tr.Close()
	for name, d := range map[string]apply.Disk{"local": apply.Local, "bridge": remote.BridgeFS{T: tr}} {
		t.Run(name, func(t *testing.T) { diskContract(t, d) })
	}
}

func diskContract(t *testing.T, d apply.Disk) {
	dir := t.TempDir()
	b := apply.Backups{Dir: t.TempDir()}
	p := filepath.Join(dir, "web.ini")
	v1 := []byte("[program:web]\ncommand=sleep 1\n")
	v2 := []byte("[program:web]\ncommand=sleep 2\n")

	if ok, err := d.Exists(p); ok || err != nil {
		t.Fatalf("Exists before create = %v, %v", ok, err)
	}
	if _, err := apply.Write(d, p, v1, "", b); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(p); fi == nil || fi.Mode().Perm() != 0o644 {
		t.Fatalf("new file mode: %v", fi)
	}
	if _, err := apply.Write(d, p, v1, "", b); !errors.Is(err, apply.ErrExists) {
		t.Fatalf("create over a file = %v, want ErrExists", err)
	}

	// Replace keeps the mode and backs up the old content.
	if err := os.Chmod(p, 0o640); err != nil {
		t.Fatal(err)
	}
	bk, err := apply.Write(d, p, v2, apply.Hash(v1), b)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(p); string(got) != string(v2) {
		t.Fatalf("content = %q", got)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %v, want 0640", fi.Mode().Perm())
	}
	if old, err := b.Read(bk); err != nil || string(old) != string(v1) {
		t.Fatalf("backup = %q, %v", old, err)
	}

	// A stale hash changes nothing and leaves no backup.
	if _, err := apply.Write(d, p, v1, apply.Hash(v1), b); !errors.Is(err, apply.ErrChangedOnDisk) {
		t.Fatalf("stale hash = %v, want ErrChangedOnDisk", err)
	}
	if list, _ := b.List(p); len(list) != 1 {
		t.Fatalf("backups after a refused write: %d", len(list))
	}
	if err := d.Replace(p, v1, apply.Hash(v1)); !errors.Is(err, apply.ErrChangedOnDisk) {
		t.Fatalf("Replace with a stale hash = %v", err)
	}

	// A symlink stays one; the file it points at is written.
	link := filepath.Join(dir, "link.ini")
	if err := os.Symlink(p, link); err != nil {
		t.Fatal(err)
	}
	if _, err := apply.Write(d, link, v1, apply.Hash(v2), b); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Lstat(link); fi.Mode()&fs.ModeSymlink == 0 {
		t.Fatal("the symlink was replaced by a file")
	}
	if got, _ := os.ReadFile(p); string(got) != string(v1) {
		t.Fatalf("target = %q", got)
	}

	// Delete refuses a changed file; removes the link itself.
	if err := d.Delete(link, apply.Hash(v2)); !errors.Is(err, apply.ErrChangedOnDisk) {
		t.Fatalf("Delete stale = %v", err)
	}
	if err := d.Delete(link, apply.Hash(v1)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(link); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("link still there")
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatal("deleting the link removed its target")
	}
	if err := apply.RevertCreate(d, p, v1); err != nil {
		t.Fatal(err)
	}
	if ok, _ := d.Exists(p); ok {
		t.Fatal("RevertCreate left the file")
	}

	// A folder this user can't write.
	if os.Geteuid() != 0 {
		ro := filepath.Join(dir, "ro")
		if err := os.Mkdir(ro, 0o755); err != nil {
			t.Fatal(err)
		}
		q := filepath.Join(ro, "a.ini")
		if err := os.WriteFile(q, v1, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(ro, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(ro, 0o755) })
		if err := d.CheckWritable(q); !errors.Is(err, apply.ErrNotWritable) {
			t.Fatalf("CheckWritable = %v", err)
		}
		if err := d.CheckRemovable(q); !errors.Is(err, apply.ErrNotWritable) {
			t.Fatalf("CheckRemovable = %v", err)
		}
		if _, err := apply.Write(d, filepath.Join(ro, "b.ini"), v1, "", b); !errors.Is(err, apply.ErrNotWritable) {
			t.Fatalf("create in a read-only folder = %v", err)
		}
		if _, err := apply.Write(d, q, v2, apply.Hash(v1), b); !errors.Is(err, apply.ErrNotWritable) {
			t.Fatalf("replace in a read-only folder = %v", err)
		}
		if list, _ := b.List(q); len(list) != 0 {
			t.Fatal("a refused write left a backup")
		}
	}
	if err := d.CheckRemovable(filepath.Join(dir, "missing.ini")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("CheckRemovable missing = %v", err)
	}

	// No temp files left behind.
	es, _ := os.ReadDir(dir)
	for _, e := range es {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("temp file left: %s", e.Name())
		}
	}
}

func TestBridgeUsersAndFolders(t *testing.T) {
	needPython(t)
	c, _ := connectOpts(t, remotetest.Options{})
	tr := &remote.BridgeTransport{Conn: c}
	defer tr.Close()
	f := remote.BridgeFS{T: tr}
	me, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	if !f.UserExists(me.Username) || !f.UserExists(me.Uid) {
		t.Fatalf("%s (uid %s) should exist", me.Username, me.Uid)
	}
	if f.UserExists("no-such-user-visor") {
		t.Fatal("a made-up user exists")
	}
	dir := t.TempDir()
	if !f.FolderWritable(dir) {
		t.Fatal("a temp folder isn't writable")
	}
	if f.FolderWritable(filepath.Join(dir, "missing")) {
		t.Fatal("a missing folder is writable")
	}
}
