package remote_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"path/filepath"
	"testing"

	"github.com/touhidurabir/visor/internal/apply"
	"github.com/touhidurabir/visor/internal/config"
	"github.com/touhidurabir/visor/internal/remote"
	"github.com/touhidurabir/visor/internal/supervisor"
)

// TestLiveSSH connects to a real server named by VISOR_SSH_TEST (an alias
// from ~/.ssh/config) and only reads: it lists processes and the include
// files. VISOR_SSH_SUDO=1 also runs the bridge with sudo (password from
// VISOR_SSH_SUDO_PASSWORD, or none for NOPASSWD). Nothing is written on the
// server, and ~/.ssh/known_hosts is only read, unless VISOR_SSH_TEST_WRITE=1:
// then each mode writes, replaces and deletes a file in a temp folder it
// makes (and removes) on the server.
func TestLiveSSH(t *testing.T) {
	alias := os.Getenv("VISOR_SSH_TEST")
	if alias == "" {
		t.Skip("set VISOR_SSH_TEST=<ssh host alias> to run")
	}
	ctx := context.Background()
	tg, err := remote.Resolve(ctx, remote.Target{Host: alias})
	if err != nil {
		t.Fatal(err)
	}
	hk := remote.DefaultHostKeys(t.TempDir())
	c, err := remote.Dial(ctx, tg, remote.Secrets{}, hk)
	if err != nil {
		t.Fatalf("dial %s (%s@%s): %v", alias, tg.User, tg.Addr(), err)
	}
	defer func() { _ = c.Close() }()

	modes := []remote.BridgeOptions{{}}
	if os.Getenv("VISOR_SSH_SUDO") == "1" {
		modes = append(modes, remote.BridgeOptions{Sudo: true, SudoPassword: os.Getenv("VISOR_SSH_SUDO_PASSWORD")})
	}
	for _, o := range modes {
		name := "user"
		if o.Sudo {
			name = "sudo"
		}
		t.Run(name, func(t *testing.T) {
			files := &remote.BridgeTransport{Conn: c, Options: o}
			defer files.Close()
			d, err := config.DiscoverFS(remote.BridgeFS{T: files}, config.PathsFor("linux"))
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("config %s, include %v", d.ConfigPath, d.IncludeGlobs)
			addr, err := remote.AddrOf(d.Transport)
			if err != nil {
				t.Fatal(err)
			}
			a, err := remote.Reach(ctx, c, addr, o)
			if err != nil {
				t.Fatalf("reach %s: %v", addr, err)
			}
			defer a.Close()
			procs, err := supervisor.New(a.Transport).GetAllProcessInfo(ctx)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("via %s: %d processes", a.Via, len(procs))
			for _, p := range procs {
				t.Logf("  %s:%s %s", p.Group, p.Name, p.StateName)
			}
			inv, err := config.LoadInventory(d)
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range inv.Files {
				t.Logf("  file %s (%d programs) err=%v", filepath.Base(f.Path), len(f.Programs), f.Err)
			}
			if b, err := a.Files.Bridge(ctx); err == nil {
				t.Logf("bridge user %s, python %s", b.Hello().User, b.Hello().Python)
			}
			if os.Getenv("VISOR_SSH_TEST_WRITE") == "1" {
				liveWrite(t, c, remote.BridgeFS{T: files})
			}
		})
	}
}

// liveWrite replaces a file the SSH user owns through f (as root with sudo)
// and checks it keeps its owner and mode, then deletes it.
func liveWrite(t *testing.T, c *remote.Conn, f remote.BridgeFS) {
	ctx := context.Background()
	out, err := c.Run(ctx, "d=$(mktemp -d) && printf 'v1\\n' > $d/a.conf && chmod 640 $d/a.conf && echo $d", nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := strings.TrimSpace(string(out))
	t.Cleanup(func() { _, _ = c.Run(ctx, "rm -rf "+dir, nil) })
	p := dir + "/a.conf"
	b := apply.Backups{Dir: t.TempDir()}
	if _, err := apply.Write(f, p, []byte("v2\n"), apply.Hash([]byte("v1\n")), b); err != nil {
		t.Fatal(err)
	}
	if _, err := apply.Write(f, p, []byte("v3\n"), apply.Hash([]byte("v1\n")), b); !errors.Is(err, apply.ErrChangedOnDisk) {
		t.Fatalf("stale hash: %v", err)
	}
	st, err := c.Run(ctx, "stat -c '%U %a' "+p+" && whoami && cat "+p, nil)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Fields(string(st))
	if len(lines) != 4 || lines[0] != lines[2] || lines[1] != "640" || lines[3] != "v2" {
		t.Fatalf("after the replace (owner mode login content): %q", st)
	}
	if err := apply.RevertCreate(f, p, []byte("v2\n")); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote, replaced (owner %s, mode 640 kept) and deleted %s", lines[0], p)
}
