package remote_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/touhidurabir/visor/internal/remote"
	"github.com/touhidurabir/visor/internal/remote/remotetest"
)

// A server offering several host keys, with only its Ed25519 key on file,
// must connect (Go would otherwise negotiate ECDSA and see a "changed" key).
func TestHostKeyNegotiatesTheKnownType(t *testing.T) {
	_, hk := isolate(t)
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ec, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	s := remotetest.Start(t, remotetest.Options{Password: "pw", ExtraHostKeys: []ssh.Signer{ec}})
	tg := target(s)
	if err := hk.Trust(tg.Host, tg.Port, s.HostKey); err != nil { // the ed25519 key only
		t.Fatal(err)
	}
	c, err := remote.Dial(context.Background(), tg, remote.Secrets{Password: "pw"}, hk)
	if err != nil {
		t.Fatalf("known ed25519 key, server also offers ECDSA: %v", err)
	}
	_ = c.Close()
}

// One unreadable line in ~/.ssh/known_hosts must not block every connection.
func TestKnownHostsBadLineIsSkipped(t *testing.T) {
	home, hk := isolate(t)
	s := remotetest.Start(t, remotetest.Options{Password: "pw"})
	tg := target(s)
	dir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	user := remote.HostKeys{VisorFile: filepath.Join(dir, "known_hosts")}
	if err := user.Trust(tg.Host, tg.Port, s.HostKey); err != nil {
		t.Fatal(err)
	}
	f, _ := os.OpenFile(user.VisorFile, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.WriteString("weird.example.com ssh-future AAAAnotakey\n")
	_ = f.Close()
	c, err := remote.Dial(context.Background(), tg, remote.Secrets{Password: "pw"}, hk)
	if err != nil {
		t.Fatalf("bad line blocked the connection: %v", err)
	}
	_ = c.Close()
}

// A wrong SSH password is sent once (password), not again through
// keyboard-interactive.
func TestWrongPasswordTriedOnce(t *testing.T) {
	_, hk := isolate(t)
	s := remotetest.Start(t, remotetest.Options{Password: "pw", KeyboardInteractive: true})
	tg := target(s)
	if err := hk.Trust(tg.Host, tg.Port, s.HostKey); err != nil {
		t.Fatal(err)
	}
	_, err := remote.Dial(context.Background(), tg, remote.Secrets{Password: "bad"}, hk)
	if !errors.Is(err, remote.ErrAuthFailed) {
		t.Fatalf("got %v", err)
	}
	if n := s.PasswordAttempts(); n != 1 {
		t.Fatalf("%d password attempts, want 1", n)
	}
}

func TestReachNotListening(t *testing.T) {
	needPython(t)
	dir, err := os.MkdirTemp("/tmp", "visor-stale-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s.sock")
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: sock, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	ln.SetUnlinkOnClose(false)
	_ = ln.Close() // a stale socket file: nothing listens
	c, _ := connectOpts(t, remotetest.Options{})
	_, err = remote.Reach(context.Background(), c, "unix:"+sock, remote.BridgeOptions{})
	if !errors.Is(err, remote.ErrNotListening) {
		t.Fatalf("got %v", err)
	}
}

// sudo takes the password and then says nothing: the result is kept, so the
// password isn't sent again on the next poll.
func TestSudoSilentAfterPasswordIsKept(t *testing.T) {
	needPython(t)
	old := remote.BridgeStartTimeout
	remote.BridgeStartTimeout = 500 * time.Millisecond
	t.Cleanup(func() { remote.BridgeStartTimeout = old })
	dir := t.TempDir()
	log := filepath.Join(dir, "sudo.log")
	script := "#!/bin/sh\necho run >> \"" + log + "\"\nprintf '[visor-sudo]' >&2\nread -r pw\nsleep 30\n"
	if err := os.WriteFile(filepath.Join(dir, "sudo"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	c, _ := connectOpts(t, remotetest.Options{Env: []string{"PATH=" + dir + ":" + os.Getenv("PATH")}})
	tr := &remote.BridgeTransport{Conn: c, Addr: "unix:/x", Options: remote.BridgeOptions{Sudo: true, SudoPassword: "pw"}}
	// The caller gives up quickly; the outcome is still waited for.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := tr.Bridge(ctx); !errors.Is(err, remote.ErrSudoUnconfirmed) {
		t.Fatalf("got %v", err)
	}
	if _, err := tr.Bridge(context.Background()); !errors.Is(err, remote.ErrSudoUnconfirmed) {
		t.Fatalf("second: %v", err)
	}
	if n := sudoRuns(t, log); n != 1 {
		t.Fatalf("sudo ran %d times", n)
	}
}
