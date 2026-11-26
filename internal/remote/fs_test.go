package remote_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/touhidurabir/visor/internal/config"
	"github.com/touhidurabir/visor/internal/remote"
	"github.com/touhidurabir/visor/internal/remote/remotetest"
	"github.com/touhidurabir/visor/internal/supervisor/supervisortest"
)

func TestBridgeFSMatchesLocal(t *testing.T) {
	needPython(t)
	inst := supervisortest.Start(t, supervisortest.Sleeper, supervisortest.Failer)
	c, _ := connectOpts(t, remotetest.Options{})
	tr := &remote.BridgeTransport{Conn: c}
	defer tr.Close()
	rfs := remote.BridgeFS{T: tr}

	local, err := config.Discover([]string{inst.ConfigPath()})
	if err != nil {
		t.Fatal(err)
	}
	rem, err := config.DiscoverFS(rfs, []string{"/nonexistent/supervisord.conf", inst.ConfigPath()})
	if err != nil {
		t.Fatal(err)
	}
	if rem.FS == nil {
		t.Fatal("remote discovery should keep its FS")
	}
	if rem.ConfigPath != local.ConfigPath || rem.PidFile != local.PidFile || rem.LogFile != local.LogFile ||
		len(rem.IncludeGlobs) != 1 || rem.IncludeGlobs[0] != local.IncludeGlobs[0] {
		t.Fatalf("discovery differs:\nlocal %+v\nremote %+v", local, rem)
	}

	li, err := config.LoadInventory(local)
	if err != nil {
		t.Fatal(err)
	}
	ri, err := config.LoadInventory(rem)
	if err != nil {
		t.Fatal(err)
	}
	if len(li.Files) != 2 || len(ri.Files) != len(li.Files) {
		t.Fatalf("files: local %d remote %d", len(li.Files), len(ri.Files))
	}
	for i := range li.Files {
		l, r := li.Files[i], ri.Files[i]
		if l.Path != r.Path || l.Hash != r.Hash || len(l.Programs) != len(r.Programs) || r.Err != nil {
			t.Fatalf("file %d differs: %+v vs %+v", i, l, r)
		}
		if d := l.ModTime.Sub(r.ModTime); d > 1e6 || d < -1e6 {
			t.Fatalf("mtime differs by %v", d)
		}
	}

	// The watcher reads through the same FS.
	w := config.NewWatcher(rem, t.TempDir())
	if _, err := w.Check(); err != nil {
		t.Fatal(err)
	}
}

func TestBridgeFSErrors(t *testing.T) {
	needPython(t)
	c, _ := connectOpts(t, remotetest.Options{})
	tr := &remote.BridgeTransport{Conn: c}
	defer tr.Close()
	rfs := remote.BridgeFS{T: tr}

	if _, err := rfs.ReadFile("/nonexistent/x"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing: %v", err)
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "secret.conf")
	if err := os.WriteFile(p, []byte("x"), 0); err != nil {
		t.Fatal(err)
	}
	if _, err := rfs.ReadFile(p); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("unreadable: %v", err)
	}
	st, err := rfs.Stat(p)
	if err != nil || st.Mode().Perm() != 0 || st.IsDir() || st.Name() != "secret.conf" {
		t.Fatalf("stat: %v %v", st, err)
	}
	if st, err := rfs.Stat(dir); err != nil || !st.IsDir() {
		t.Fatalf("stat dir: %v", err)
	}
	tail, err := rfs.Tail("/etc/hosts", 5)
	if err != nil || len(tail) > 5 {
		t.Fatalf("tail: %q %v", tail, err)
	}
	if _, err := rfs.Glob("[bad"); err == nil {
		t.Fatal("bad pattern should fail")
	}
}

// A sample on a link that stopped answering ends with its ctx (a tab
// closing), not after BridgeFS's own 15 s.
func TestBridgeFSUsageStopsWithCtx(t *testing.T) {
	needPython(t)
	c, srv := connectOpts(t, remotetest.Options{})
	tr := &remote.BridgeTransport{Conn: c}
	defer tr.Close()
	rfs := remote.BridgeFS{T: tr}
	if _, err := rfs.Usage(context.Background(), []int{1}); err != nil {
		t.Fatal(err)
	}
	srv.Freeze()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := rfs.Usage(ctx, []int{1}); err == nil {
		t.Fatal("a frozen link answered")
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("took %s after its ctx ended", d)
	}
}
