package settings

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/zalando/go-keyring"
)

// serversFile holds the saved servers; it never holds a secret.
const serversFile = "servers.json"

// Server tags. Production asks before actions that stop or change things.
const (
	TagLocal       = "local"
	TagDevelopment = "development"
	TagTesting     = "testing"
	TagStaging     = "staging"
	TagProduction  = "production"
)

// Tags in the order the form lists them.
var Tags = []string{TagLocal, TagDevelopment, TagTesting, TagStaging, TagProduction}

// Where a server's sudo password comes from.
const (
	SudoSameAsSSH = "same" // the SSH password
	SudoOwn       = "own"  // its own saved password
	SudoNone      = "none" // NOPASSWD: no password
)

// Limits on how many servers can be connected at once (the local tab doesn't count).
const (
	DefaultMaxServers = 3
	MinMaxServers     = 1
	MaxMaxServers     = 10
)

// Server is a saved server. Secrets are in the Keychain or keyring under
// ServerAccount(ID, …), never here.
type Server struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Tag   string `json:"tag"`
	Color string `json:"color"`
	Host  string `json:"host"`
	Port  int    `json:"port"`
	User  string `json:"user"`
	// KeyPath is a private key file; "" uses ~/.ssh/config and the agent.
	KeyPath string `json:"keyPath"`
	// Jump is ProxyJump hosts ("user@host:port,host2") when not set in ~/.ssh/config.
	Jump         string `json:"jump,omitempty"`
	UseSudo      bool   `json:"useSudo"`
	SudoPassword string `json:"sudoPassword"` // SudoSameAsSSH, SudoOwn or SudoNone
	// ConfigPath is supervisord.conf on the server; "" discovers it.
	ConfigPath string `json:"configPath,omitempty"`
	// ReadOnly hides and refuses every action: view only.
	ReadOnly bool `json:"readOnly"`
	// Mutes are the activity kinds whose notifications are off for this server.
	Mutes ActivityMutes `json:"mutes"`
}

// Production reports whether actions on this server ask first.
func (s Server) Production() bool { return s.Tag == TagProduction }

// Secret kinds kept per server.
const (
	SecretPassword      = "password"
	SecretKeyPassphrase = "key-passphrase"
	SecretSudo          = "sudo"
)

// ServerAccount is the Keychain account for one of a server's secrets.
func ServerAccount(id, kind string) string { return "server/" + id + "/" + kind }

// FieldError names the form field that is wrong.
type FieldError struct {
	Field string `json:"field"`
	Msg   string `json:"msg"`
}

func (e *FieldError) Error() string { return e.Msg }

var colorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// Normalize trims the fields and fills defaults (port 22, tag development,
// sudo password same as SSH).
func (s Server) Normalize() Server {
	s.Name = strings.TrimSpace(s.Name)
	s.Host = strings.TrimSpace(s.Host)
	s.User = strings.TrimSpace(s.User)
	s.KeyPath = strings.TrimSpace(s.KeyPath)
	s.Jump = strings.TrimSpace(s.Jump)
	s.ConfigPath = strings.TrimSpace(s.ConfigPath)
	if s.Port == 0 {
		s.Port = 22
	}
	if s.Tag == "" {
		s.Tag = TagDevelopment
	}
	switch s.SudoPassword {
	case SudoOwn, SudoNone:
	default:
		s.SudoPassword = SudoSameAsSSH
	}
	return s
}

// Validate checks s against the other saved servers (s itself excluded by ID).
func (s Server) Validate(saved []Server) error {
	switch {
	case s.Name == "":
		return &FieldError{"name", "Give the server a name."}
	case len(s.Name) > 60:
		return &FieldError{"name", "Keep the name under 60 characters."}
	case s.Host == "":
		return &FieldError{"host", "Enter the server's host name or address, or an alias from ~/.ssh/config."}
	case strings.ContainsAny(s.Host, " /@"):
		return &FieldError{"host", "The host is a name or address only, with no user or path."}
	case s.Port < 1 || s.Port > 65535:
		return &FieldError{"port", "The port is a number from 1 to 65535."}
	case s.Color != "" && !colorRe.MatchString(s.Color):
		return &FieldError{"color", "Pick one of the colours."}
	case s.ConfigPath != "" && !strings.HasPrefix(s.ConfigPath, "/"):
		return &FieldError{"configPath", "The config path on the server starts with /."}
	}
	valid := false
	for _, t := range Tags {
		valid = valid || s.Tag == t
	}
	if !valid {
		return &FieldError{"tag", "Pick a tag."}
	}
	for _, o := range saved {
		if o.ID != s.ID && strings.EqualFold(o.Name, s.Name) {
			return &FieldError{"name", fmt.Sprintf("Another server is already called %s.", o.Name)}
		}
	}
	if s.KeyPath != "" {
		p := s.KeyPath
		if p == "~" || strings.HasPrefix(p, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				p = home + p[1:]
			}
		}
		if fi, err := os.Stat(p); err != nil || fi.IsDir() {
			return &FieldError{"keyPath", "There's no key file at " + s.KeyPath + "."}
		}
	}
	return nil
}

// NewServerID returns a fresh id ("s" and 10 hex digits).
func NewServerID() string {
	var b [5]byte
	_, _ = rand.Read(b[:])
	return "s" + hex.EncodeToString(b[:])
}

var idRe = regexp.MustCompile(`^s[0-9a-f]{10}$`)

// ValidServerID reports whether id has the form NewServerID makes (it is
// used in paths and Keychain accounts).
func ValidServerID(id string) bool { return idRe.MatchString(id) }

type serversJSON struct {
	Version int      `json:"version"`
	Servers []Server `json:"servers"`
}

// LoadServers reads dir/servers.json; a missing file is no servers.
func LoadServers(dir string) ([]Server, error) {
	b, err := os.ReadFile(filepath.Join(dir, serversFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read the saved servers: %w", err)
	}
	var f serversJSON
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("cannot read the saved servers in %s: %w", filepath.Join(dir, serversFile), err)
	}
	out := f.Servers[:0]
	for _, s := range f.Servers {
		if ValidServerID(s.ID) {
			out = append(out, s.Normalize())
		}
	}
	return out, nil
}

// SaveServers writes dir/servers.json atomically, mode 0600.
func SaveServers(dir string, servers []Server) error {
	b, err := json.MarshalIndent(serversJSON{Version: 1, Servers: servers}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("cannot save the servers: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "servers-*.tmp")
	if err != nil {
		return fmt.Errorf("cannot save the servers: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("cannot save the servers: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("cannot save the servers: %w", err)
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, serversFile))
}

// ServerSecret returns a saved secret, or "" when there is none.
func ServerSecret(k Keychain, id, kind string) (string, error) {
	v, err := k.Get(KeychainService, ServerAccount(id, kind))
	switch {
	case errors.Is(err, keyring.ErrNotFound):
		return "", nil
	case err != nil:
		return "", fmt.Errorf("cannot read the server's %s from the %s: %w", kind, store(), err)
	}
	return v, nil
}

// SetServerSecret saves a secret; an empty value removes it.
func SetServerSecret(k Keychain, id, kind, value string) error {
	acct := ServerAccount(id, kind)
	if value == "" {
		if err := k.Delete(KeychainService, acct); err != nil && !errors.Is(err, keyring.ErrNotFound) {
			return fmt.Errorf("cannot remove the server's %s from the %s: %w", kind, store(), err)
		}
		return nil
	}
	if err := k.Set(KeychainService, acct, value); errors.Is(err, ErrNoKeyring) {
		return err
	} else if err != nil {
		return fmt.Errorf("cannot save the server's %s in the %s: %w", kind, store(), err)
	}
	return nil
}

// DeleteServerSecrets removes every secret saved for a server.
func DeleteServerSecrets(k Keychain, id string) error {
	for _, kind := range []string{SecretPassword, SecretKeyPassphrase, SecretSudo} {
		if err := SetServerSecret(k, id, kind, ""); err != nil {
			return err
		}
	}
	return nil
}

// ServerStateDir is where a server's issues, history and activity are kept.
func ServerStateDir(stateDir, id string) string {
	return filepath.Join(stateDir, "servers", id)
}

// RemoveServerState deletes a server's state folder (after the caller asked).
func RemoveServerState(stateDir, id string) error {
	if !ValidServerID(id) {
		return fmt.Errorf("bad server id %q", id)
	}
	return os.RemoveAll(ServerStateDir(stateDir, id))
}
