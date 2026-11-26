package remote_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"net"
	"os"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"github.com/touhidurabir/visor/internal/remote"
	"github.com/touhidurabir/visor/internal/remote/remotetest"
)

func TestDialThroughJumpHost(t *testing.T) {
	home, hk := isolate(t)
	keyPath, pub := writeKey(t, home, "")
	// The jump host also takes the server's password: it must never be offered.
	jump := remotetest.Start(t, remotetest.Options{Password: "pw", AuthorizedKeys: []ssh.PublicKey{pub}})
	dest := remotetest.Start(t, remotetest.Options{Password: "pw"})
	for _, s := range []*remotetest.Server{jump, dest} {
		if err := hk.Trust(s.Host, s.Port, s.HostKey); err != nil {
			t.Fatal(err)
		}
	}
	tg := target(dest)
	hop := target(jump)
	hop.KeyPath = keyPath
	tg.Jump = []remote.Target{hop}
	c, err := remote.Dial(context.Background(), tg, remote.Secrets{Password: "pw"}, hk)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	if out, err := c.Run(context.Background(), "echo ok", nil); err != nil || string(out) != "ok\n" {
		t.Fatalf("%q %v", out, err)
	}
	if len(dest.Execs()) != 1 || len(jump.Execs()) != 0 {
		t.Fatal("the command should run on the destination only")
	}
	if n := jump.PasswordAttempts(); n != 0 {
		t.Fatalf("the jump host was offered the server's password %d times", n)
	}

	// A jump host that only takes a password isn't given the server's.
	pwOnly := target(jump)
	tg.Jump = []remote.Target{pwOnly}
	if _, err := remote.Dial(context.Background(), tg, remote.Secrets{Password: "pw"}, hk); !errors.Is(err, remote.ErrNoAuth) {
		t.Fatalf("password-only jump host: %v, want ErrNoAuth", err)
	}
	if n := jump.PasswordAttempts(); n != 0 {
		t.Fatalf("the jump host was offered the server's password %d times", n)
	}
	tg.Jump = []remote.Target{hop}

	// An unknown jump host is reported as such.
	_, hk2 := isolate(t)
	if err := hk2.Trust(dest.Host, dest.Port, dest.HostKey); err != nil {
		t.Fatal(err)
	}
	_, err = remote.Dial(context.Background(), tg, remote.Secrets{Password: "pw"}, hk2)
	var unknown *remote.UnknownHostError
	if !errors.As(err, &unknown) || unknown.Host != jump.Addr {
		t.Fatalf("want unknown jump host, got %v", err)
	}
}

func TestDialWithAgent(t *testing.T) {
	_, hk := isolate(t)
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ring := agent.NewKeyring()
	if err := ring.Add(agent.AddedKey{PrivateKey: priv}); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp("/tmp", "visor-agent-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := dir + "/a.sock"
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { _ = agent.ServeAgent(ring, c) }()
		}
	}()
	signer, _ := ssh.NewSignerFromKey(priv)
	s := remotetest.Start(t, remotetest.Options{AuthorizedKeys: []ssh.PublicKey{signer.PublicKey()}})
	if err := hk.Trust(s.Host, s.Port, s.HostKey); err != nil {
		t.Fatal(err)
	}
	tg := target(s)
	tg.AgentSocket = sock
	c, err := remote.Dial(context.Background(), tg, remote.Secrets{}, hk)
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()

	// An encrypted key file with no passphrase saved is left to the agent.
	block, err := ssh.MarshalPrivateKeyWithPassphrase(priv, "", []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	tg.KeyPath = dir + "/id_enc"
	if err := os.WriteFile(tg.KeyPath, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err = remote.Dial(context.Background(), tg, remote.Secrets{}, hk)
	if err != nil {
		t.Fatalf("encrypted key + agent: %v", err)
	}
	_ = c.Close()
	// A wrong user: the agent offered the locked key's twin (its .pub says
	// so), so the server refused the login; the lock isn't to blame.
	if err := os.WriteFile(tg.KeyPath+".pub", ssh.MarshalAuthorizedKey(signer.PublicKey()), 0o644); err != nil {
		t.Fatal(err)
	}
	wrong := tg
	wrong.User = "nobody"
	if _, err := remote.Dial(context.Background(), wrong, remote.Secrets{}, hk); !errors.Is(err, remote.ErrAuthFailed) {
		t.Fatalf("wrong user, key in the agent: %v", err)
	}
	// The agent holds other keys only: the locked key is what's missing.
	other, _, _ := ed25519.GenerateKey(rand.Reader)
	otherPub, _ := ssh.NewPublicKey(other)
	if err := os.WriteFile(tg.KeyPath+".pub", ssh.MarshalAuthorizedKey(otherPub), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := remote.Dial(context.Background(), wrong, remote.Secrets{}, hk); !errors.Is(err, remote.ErrPassphraseNeeded) {
		t.Fatalf("wrong user, key not in the agent: %v", err)
	}
	tg.AgentSocket = ""
	if _, err := remote.Dial(context.Background(), tg, remote.Secrets{}, hk); !errors.Is(err, remote.ErrPassphraseNeeded) {
		t.Fatalf("no agent: %v", err)
	}
}

// A locked key left to an agent that can't use it, and a refused password:
// the password is what failed, not the missing passphrase.
func TestRefusedPasswordWinsOverALockedKey(t *testing.T) {
	_, hk := isolate(t)
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp("/tmp", "visor-agent-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	ln, err := net.Listen("unix", dir+"/a.sock")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { _ = agent.ServeAgent(agent.NewKeyring(), c) }()
		}
	}()
	block, err := ssh.MarshalPrivateKeyWithPassphrase(priv, "", []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	s := remotetest.Start(t, remotetest.Options{Password: "pw"})
	if err := hk.Trust(s.Host, s.Port, s.HostKey); err != nil {
		t.Fatal(err)
	}
	tg := target(s)
	tg.AgentSocket, tg.KeyPath = dir+"/a.sock", dir+"/id_enc"
	if err := os.WriteFile(tg.KeyPath, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := remote.Dial(context.Background(), tg, remote.Secrets{Password: "bad"}, hk); !errors.Is(err, remote.ErrAuthFailed) {
		t.Fatalf("wrong password: %v", err)
	}
	if _, err := remote.Dial(context.Background(), tg, remote.Secrets{}, hk); !errors.Is(err, remote.ErrPassphraseNeeded) {
		t.Fatalf("no password: %v", err)
	}
}

func TestKeepaliveClosesAFrozenConnection(t *testing.T) {
	oldI, oldM := remote.KeepaliveInterval, remote.KeepaliveMax
	remote.KeepaliveInterval, remote.KeepaliveMax = 50*time.Millisecond, 3
	t.Cleanup(func() { remote.KeepaliveInterval, remote.KeepaliveMax = oldI, oldM })

	c, srv := connectOpts(t, remotetest.Options{})
	select {
	case <-c.Closed():
		t.Fatal("closed while the server answers")
	case <-time.After(300 * time.Millisecond):
	}
	srv.Freeze()
	select {
	case <-c.Closed():
		if !errors.Is(c.Err(), remote.ErrKeepalive) {
			t.Fatalf("err %v", c.Err())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a frozen server should close the connection")
	}
}

func TestDropClosesConn(t *testing.T) {
	c, srv := connectOpts(t, remotetest.Options{})
	srv.DropAll()
	select {
	case <-c.Closed():
	case <-time.After(3 * time.Second):
		t.Fatal("dropped connection not noticed")
	}
}
