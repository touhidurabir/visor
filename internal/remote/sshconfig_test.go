package remote

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

const sampleG = `user abir
hostname 192.168.65.2
port 2222
identityfile ~/.ssh/id_rsa
identityfile ~/.ssh/id_ed25519
identityagent SSH_AUTH_SOCK
proxyjump bastion,ops@10.0.0.1:2200
proxycommand none
`

func TestParseSSHG(t *testing.T) {
	c := ParseSSHG([]byte(sampleG))
	if c.HostName != "192.168.65.2" || c.Port != 2222 || c.User != "abir" || len(c.IdentityFiles) != 2 ||
		c.IdentityAgent != "" || c.ProxyJump != "bastion,ops@10.0.0.1:2200" || c.ProxyCommand != "" {
		t.Fatalf("%+v", c)
	}
	// ssh prints "User" capitalised in some versions.
	if c := ParseSSHG([]byte("User deploy\n")); c.User != "deploy" {
		t.Fatalf("%+v", c)
	}
}

func TestParseHop(t *testing.T) {
	for in, want := range map[string]Target{
		"bastion":           {Host: "bastion"},
		"ops@10.0.0.1:2200": {Host: "10.0.0.1", Port: 2200, User: "ops"},
		"ssh://u@h":         {Host: "h", User: "u"},
		"[::1]:22":          {Host: "::1", Port: 22},
	} {
		got, err := parseHop(in)
		if err != nil || got.Host != want.Host || got.Port != want.Port || got.User != want.User {
			t.Errorf("%s: %+v %v", in, got, err)
		}
	}
	if _, err := parseHop("h:x"); err == nil {
		t.Error("bad port should fail")
	}
}

func TestResolve(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SSH_AUTH_SOCK", "/tmp/agent.sock")
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".ssh", "id_ed25519"), []byte("k"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := sshG
	t.Cleanup(func() { sshG = old })
	sshG = func(_ context.Context, host string) ([]byte, error) {
		switch host {
		case "vm":
			return []byte(sampleG), nil
		case "bastion":
			return []byte("hostname bastion.example.com\nuser jump\nport 22\n"), nil
		case "pc":
			return []byte("hostname x\nproxycommand nc %h %p\n"), nil
		}
		return []byte("hostname " + host + "\nport 22\n"), nil
	}
	got, err := Resolve(context.Background(), Target{Host: "vm", User: "deploy"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Host != "192.168.65.2" || got.Port != 2222 || got.User != "deploy" ||
		got.KeyPath != "~/.ssh/id_ed25519" || got.AgentSocket != "/tmp/agent.sock" {
		t.Fatalf("%+v", got)
	}
	if len(got.Jump) != 2 || got.Jump[0].Host != "bastion.example.com" || got.Jump[0].User != "jump" ||
		got.Jump[1].Host != "10.0.0.1" || got.Jump[1].Port != 2200 || got.Jump[1].User != "ops" {
		t.Fatalf("jumps: %+v", got.Jump)
	}
	if _, err := Resolve(context.Background(), Target{Host: "pc"}); !errors.Is(err, ErrProxyCommand) {
		t.Fatalf("proxycommand: %v", err)
	}

	sshG = func(context.Context, string) ([]byte, error) { return nil, errors.New("no ssh") }
	got, err = Resolve(context.Background(), Target{Host: "h", User: "u"})
	if err != nil || got.Host != "h" || got.AgentSocket != "/tmp/agent.sock" {
		t.Fatalf("without ssh: %+v %v", got, err)
	}
}
