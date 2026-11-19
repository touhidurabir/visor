package remote_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/touhidurabir/visor/internal/remote"
	"github.com/touhidurabir/visor/internal/remote/remotetest"
)

// isolate points HOME at a temp dir so nothing reads or writes the real ~/.ssh.
func isolate(t *testing.T) (home string, hk remote.HostKeys) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	state := filepath.Join(home, "state")
	return home, remote.DefaultHostKeys(state)
}

// writeKey writes an ed25519 private key (encrypted when passphrase != "").
func writeKey(t *testing.T, dir, passphrase string) (string, ssh.PublicKey) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var block *pem.Block
	if passphrase == "" {
		block, err = ssh.MarshalPrivateKey(priv, "")
	} else {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(priv, "", []byte(passphrase))
	}
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "id_test")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return path, signer.PublicKey()
}

func target(s *remotetest.Server) remote.Target {
	return remote.Target{Host: s.Host, Port: s.Port, User: "visor"}
}

// trustFirst dials once, expects an unknown host, trusts it.
func trustFirst(t *testing.T, tg remote.Target, sec remote.Secrets, hk remote.HostKeys) {
	t.Helper()
	_, err := remote.Dial(context.Background(), tg, sec, hk)
	var unknown *remote.UnknownHostError
	if !errors.As(err, &unknown) {
		t.Fatalf("first dial: want UnknownHostError, got %v", err)
	}
	if !strings.HasPrefix(unknown.Fingerprint, "SHA256:") {
		t.Fatalf("fingerprint %q", unknown.Fingerprint)
	}
	if err := hk.Trust(tg.Host, tg.Port, unknown.Key); err != nil {
		t.Fatal(err)
	}
}

func TestDialPassword(t *testing.T) {
	home, hk := isolate(t)
	s := remotetest.Start(t, remotetest.Options{Password: "pw"})
	tg := target(s)
	sec := remote.Secrets{Password: "pw"}
	trustFirst(t, tg, sec, hk)

	c, err := remote.Dial(context.Background(), tg, sec, hk)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	out, err := c.Run(context.Background(), "echo hello", nil)
	if err != nil || strings.TrimSpace(string(out)) != "hello" {
		t.Fatalf("run: %q %v", out, err)
	}

	if _, err := remote.Dial(context.Background(), tg, remote.Secrets{Password: "nope"}, hk); !errors.Is(err, remote.ErrAuthFailed) {
		t.Fatalf("wrong password: want ErrAuthFailed, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".ssh", "known_hosts")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("~/.ssh/known_hosts was written: %v", err)
	}
}

func TestDialKeyboardInteractive(t *testing.T) {
	_, hk := isolate(t)
	s := remotetest.Start(t, remotetest.Options{Password: "pw", KeyboardInteractive: true})
	tg := target(s)
	trustFirst(t, tg, remote.Secrets{Password: "pw"}, hk)
	c, err := remote.Dial(context.Background(), tg, remote.Secrets{Password: "pw"}, hk)
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
}

func TestDialKey(t *testing.T) {
	home, hk := isolate(t)
	keyPath, pub := writeKey(t, home, "")
	s := remotetest.Start(t, remotetest.Options{AuthorizedKeys: []ssh.PublicKey{pub}})
	tg := target(s)
	tg.KeyPath = keyPath
	trustFirst(t, tg, remote.Secrets{}, hk)
	c, err := remote.Dial(context.Background(), tg, remote.Secrets{}, hk)
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()

	enc, err := remote.ParseKeyFile(keyPath)
	if err != nil || enc {
		t.Fatalf("ParseKeyFile: %v %v", enc, err)
	}
}

func TestDialKeyPassphrase(t *testing.T) {
	home, hk := isolate(t)
	keyPath, pub := writeKey(t, home, "secret")
	s := remotetest.Start(t, remotetest.Options{AuthorizedKeys: []ssh.PublicKey{pub}})
	tg := target(s)
	tg.KeyPath = keyPath

	if _, err := remote.Dial(context.Background(), tg, remote.Secrets{}, hk); !errors.Is(err, remote.ErrPassphraseNeeded) {
		t.Fatalf("no passphrase: got %v", err)
	}
	if _, err := remote.Dial(context.Background(), tg, remote.Secrets{KeyPassphrase: "bad"}, hk); !errors.Is(err, remote.ErrPassphraseWrong) {
		t.Fatalf("bad passphrase: got %v", err)
	}
	sec := remote.Secrets{KeyPassphrase: "secret"}
	trustFirst(t, tg, sec, hk)
	c, err := remote.Dial(context.Background(), tg, sec, hk)
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()

	if enc, err := remote.ParseKeyFile(keyPath); err != nil || !enc {
		t.Fatalf("ParseKeyFile: %v %v", enc, err)
	}
}

func TestDialNoAuth(t *testing.T) {
	_, hk := isolate(t)
	s := remotetest.Start(t, remotetest.Options{Password: "pw"})
	if _, err := remote.Dial(context.Background(), target(s), remote.Secrets{}, hk); !errors.Is(err, remote.ErrNoAuth) {
		t.Fatalf("got %v", err)
	}
}

func TestHostKeyChangedIsRefused(t *testing.T) {
	_, hk := isolate(t)
	s := remotetest.Start(t, remotetest.Options{Password: "pw"})
	tg := target(s)
	sec := remote.Secrets{Password: "pw"}
	trustFirst(t, tg, sec, hk)
	s.Close()

	// Same address, new key.
	s2 := remotetest.Start(t, remotetest.Options{Password: "pw"})
	tg2 := target(s2)
	if err := hk.Trust(tg2.Host, tg2.Port, s.HostKey); err != nil { // the old key on file for the new port
		t.Fatal(err)
	}
	_, err := remote.Dial(context.Background(), tg2, sec, hk)
	var changed *remote.HostKeyChangedError
	if !errors.As(err, &changed) {
		t.Fatalf("want HostKeyChangedError, got %v", err)
	}
	if changed.New != remote.Fingerprint(s2.HostKey) || len(changed.Old) != 1 || changed.Old[0] != remote.Fingerprint(s.HostKey) {
		t.Fatalf("fingerprints: %+v", changed)
	}
}

func TestUserKnownHostsIsReadOnly(t *testing.T) {
	home, hk := isolate(t)
	s := remotetest.Start(t, remotetest.Options{Password: "pw"})
	tg := target(s)
	// The user already trusts the server in ~/.ssh/known_hosts.
	ssh_ := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(ssh_, 0o700); err != nil {
		t.Fatal(err)
	}
	user := remote.HostKeys{VisorFile: filepath.Join(ssh_, "known_hosts")}
	if err := user.Trust(tg.Host, tg.Port, s.HostKey); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(user.VisorFile)

	c, err := remote.Dial(context.Background(), tg, remote.Secrets{Password: "pw"}, hk)
	if err != nil {
		t.Fatalf("a key in ~/.ssh/known_hosts should be trusted: %v", err)
	}
	_ = c.Close()
	after, _ := os.ReadFile(user.VisorFile)
	if string(before) != string(after) {
		t.Fatal("~/.ssh/known_hosts changed")
	}
	if _, err := os.Stat(hk.VisorFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("Visor's known_hosts should not be written for an already known host")
	}
}

func TestDialContextCancel(t *testing.T) {
	_, hk := isolate(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := remote.Dial(ctx, remote.Target{Host: "127.0.0.1", Port: 1, User: "x"}, remote.Secrets{Password: "pw"}, hk)
	if err == nil {
		t.Fatal("want an error")
	}
}
