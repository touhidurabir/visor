package settings

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServersRoundTripHoldsNoSecret(t *testing.T) {
	dir := t.TempDir()
	k := &MemoryKeychain{}
	s := Server{ID: NewServerID(), Name: " cpcsl dev ", Host: "dev.example.com", User: "deploy", UseSudo: true, Tag: TagProduction, Color: "#2f6fd6"}.Normalize()
	if s.Port != 22 || s.SudoPassword != SudoSameAsSSH || s.Name != "cpcsl dev" {
		t.Fatalf("normalize: %+v", s)
	}
	if err := s.Validate(nil); err != nil {
		t.Fatal(err)
	}
	if err := SaveServers(dir, []Server{s}); err != nil {
		t.Fatal(err)
	}
	for kind, v := range map[string]string{SecretPassword: "ssh-secret-1", SecretKeyPassphrase: "pass-secret-2", SecretSudo: "sudo-secret-3"} {
		if err := SetServerSecret(k, s.ID, kind, v); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(filepath.Join(dir, "servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"ssh-secret-1", "pass-secret-2", "sudo-secret-3"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("servers.json contains a secret: %s", raw)
		}
	}
	if fi, _ := os.Stat(filepath.Join(dir, "servers.json")); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode())
	}
	got, err := LoadServers(dir)
	if err != nil || len(got) != 1 || got[0].Name != "cpcsl dev" || !got[0].Production() {
		t.Fatalf("load: %+v %v", got, err)
	}
	if pw, _ := ServerSecret(k, s.ID, SecretSudo); pw != "sudo-secret-3" {
		t.Fatalf("secret %q", pw)
	}
	if err := DeleteServerSecrets(k, s.ID); err != nil {
		t.Fatal(err)
	}
	if pw, err := ServerSecret(k, s.ID, SecretPassword); pw != "" || err != nil {
		t.Fatalf("after delete: %q %v", pw, err)
	}
}

func TestLoadServersMissingFile(t *testing.T) {
	got, err := LoadServers(t.TempDir())
	if err != nil || got != nil {
		t.Fatalf("%v %v", got, err)
	}
}

func TestServerValidate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(home, "id"), []byte("k"), 0o600); err != nil {
		t.Fatal(err)
	}
	other := Server{ID: "s0000000001", Name: "Prod"}
	base := Server{ID: "s0000000002", Name: "dev", Host: "h"}.Normalize()
	cases := map[string]func(*Server){
		"name":       func(s *Server) { s.Name = "" },
		"host":       func(s *Server) { s.Host = "" },
		"port":       func(s *Server) { s.Port = 70000 },
		"tag":        func(s *Server) { s.Tag = "nope" },
		"color":      func(s *Server) { s.Color = "red" },
		"configPath": func(s *Server) { s.ConfigPath = "etc/x.conf" },
		"keyPath":    func(s *Server) { s.KeyPath = "~/missing" },
	}
	for field, mod := range cases {
		s := base
		mod(&s)
		var fe *FieldError
		if err := s.Validate([]Server{other}); !errors.As(err, &fe) || fe.Field != field {
			t.Errorf("%s: got %v", field, err)
		}
	}
	dup := base
	dup.Name = "prod"
	var fe *FieldError
	if err := dup.Validate([]Server{other}); !errors.As(err, &fe) || fe.Field != "name" {
		t.Errorf("duplicate name: %v", err)
	}
	ok := base
	ok.KeyPath = "~/id"
	if err := ok.Validate([]Server{other, base}); err != nil {
		t.Errorf("valid server: %v", err)
	}
}

func TestServerStateAndIDs(t *testing.T) {
	id := NewServerID()
	if !ValidServerID(id) || ValidServerID("../x") || ValidServerID("local") {
		t.Fatal("ids")
	}
	dir := t.TempDir()
	sd := ServerStateDir(dir, id)
	if err := os.MkdirAll(sd, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := RemoveServerState(dir, id); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sd); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("state left behind")
	}
	if err := RemoveServerState(dir, ".."); err == nil {
		t.Fatal("bad id accepted")
	}
}

func TestMaxServersClamp(t *testing.T) {
	if Defaults().MaxServers != 3 {
		t.Fatal("default")
	}
	s := Defaults()
	s.MaxServers = 0
	if s.Clamp().MaxServers != MinMaxServers {
		t.Fatal("clamp")
	}
}
