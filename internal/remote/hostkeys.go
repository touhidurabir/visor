package remote

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// HostKeys checks server host keys. Keys are trusted when they appear in any
// of UserFiles (normally ~/.ssh/known_hosts, read only: Visor never writes
// there) or in VisorFile, the only file Trust writes.
type HostKeys struct {
	VisorFile string
	UserFiles []string
}

// DefaultHostKeys uses Visor's file in stateDir and the user's
// ~/.ssh/known_hosts (read only).
func DefaultHostKeys(stateDir string) HostKeys {
	h := HostKeys{VisorFile: filepath.Join(stateDir, "known_hosts")}
	if home, err := os.UserHomeDir(); err == nil {
		h.UserFiles = []string{filepath.Join(home, ".ssh", "known_hosts")}
	}
	return h
}

// UnknownHostError means the server's key is in none of the files. The user
// can check Fingerprint and call Trust.
type UnknownHostError struct {
	Host        string // host:port as dialled
	Key         ssh.PublicKey
	Fingerprint string // SHA256:…
}

func (e *UnknownHostError) Error() string {
	return fmt.Sprintf("%s is not a known host (key %s)", e.Host, e.Fingerprint)
}

// HostKeyChangedError means a known host offered a different key. Visor
// refuses the connection; the user has to remove the old key themselves.
type HostKeyChangedError struct {
	Host string
	Old  []string // fingerprints on file
	New  string
	File string // where the old key is recorded
	Line int
}

func (e *HostKeyChangedError) Error() string {
	return fmt.Sprintf("the host key for %s changed (now %s, expected %s)", e.Host, e.New, strings.Join(e.Old, ", "))
}

// Fingerprint returns the SHA256 fingerprint ssh-keygen -l prints.
func Fingerprint(k ssh.PublicKey) string { return ssh.FingerprintSHA256(k) }

var trustMu sync.Mutex

// callback returns a HostKeyCallback over the existing files. Missing files
// are skipped; an unreadable or malformed one is an error.
func (h HostKeys) callback() (ssh.HostKeyCallback, func(addr string) []string, error) {
	var files []string
	for _, f := range append(append([]string{}, h.UserFiles...), h.VisorFile) {
		if f == "" {
			continue
		}
		if _, err := os.Stat(f); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, nil, err
		}
		files = append(files, f)
	}
	var known ssh.HostKeyCallback
	if len(files) > 0 {
		cb, err := knownhosts.New(files...)
		if err != nil {
			// Like OpenSSH, skip lines that can't be read (a key type Go
			// doesn't know) instead of refusing every connection.
			cb, err = tolerantKnownHosts(files)
			if err != nil {
				return nil, nil, fmt.Errorf("read known hosts: %w", err)
			}
		}
		known = cb
	}
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		if known == nil {
			return &UnknownHostError{Host: hostname, Key: key, Fingerprint: Fingerprint(key)}
		}
		err := known(hostname, remote, key)
		var ke *knownhosts.KeyError
		if errors.As(err, &ke) {
			if len(ke.Want) == 0 {
				return &UnknownHostError{Host: hostname, Key: key, Fingerprint: Fingerprint(key)}
			}
			ch := &HostKeyChangedError{Host: hostname, New: Fingerprint(key)}
			for _, w := range ke.Want {
				ch.Old = append(ch.Old, Fingerprint(w.Key))
				if ch.File == "" {
					ch.File, ch.Line = w.Filename, w.Line
				}
			}
			return ch
		}
		return err
	}, func(addr string) []string { return algorithms(known, addr) }, nil
}

// Trust records key for host:port in VisorFile (created 0600 with its folder).
func (h HostKeys) Trust(host string, port int, key ssh.PublicKey) error {
	if h.VisorFile == "" {
		return errors.New("no known_hosts file for Visor")
	}
	trustMu.Lock()
	defer trustMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(h.VisorFile), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(h.VisorFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	addr := knownhosts.Normalize(net.JoinHostPort(host, strconv.Itoa(port)))
	if _, err := f.WriteString(knownhosts.Line([]string{addr}, key) + "\n"); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// tolerantKnownHosts loads files keeping only the lines that parse.
func tolerantKnownHosts(files []string) (ssh.HostKeyCallback, error) {
	tmp, err := os.CreateTemp("", "visor-known-hosts-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			_ = tmp.Close()
			return nil, err
		}
		for _, line := range strings.Split(string(b), "\n") {
			if t := strings.TrimSpace(line); t == "" || strings.HasPrefix(t, "#") {
				continue
			}
			if _, _, _, _, _, err := ssh.ParseKnownHosts([]byte(line)); err != nil {
				continue
			}
			if _, err := tmp.WriteString(line + "\n"); err != nil {
				_ = tmp.Close()
				return nil, err
			}
		}
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	return knownhosts.New(tmp.Name())
}

// probeAddr is a net.Addr for asking the database about a host before dialling.
type probeAddr string

func (a probeAddr) Network() string { return "tcp" }
func (a probeAddr) String() string  { return string(a) }

var probeKey = func() ssh.PublicKey {
	k, _ := ssh.ParsePublicKey(ed25519Probe)
	return k
}()

// ed25519Probe is a fixed public key no host has (all zero bytes).
var ed25519Probe = ssh.Marshal(struct {
	Name string
	Key  []byte
}{ssh.KeyAlgoED25519, make([]byte, 32)})

// algorithms returns the host key algorithms to ask addr for: the types of
// the keys on file for it, so a server with several keys offers one Visor
// knows instead of one that looks changed. nil when none is on file.
func algorithms(known ssh.HostKeyCallback, addr string) []string {
	if known == nil {
		return nil
	}
	err := known(addr, probeAddr(addr), probeKey)
	var ke *knownhosts.KeyError
	if !errors.As(err, &ke) || len(ke.Want) == 0 {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	add := func(a ...string) {
		for _, x := range a {
			if !seen[x] {
				seen[x] = true
				out = append(out, x)
			}
		}
	}
	for _, w := range ke.Want {
		switch t := w.Key.Type(); t {
		case ssh.KeyAlgoRSA:
			add(ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA)
		default:
			add(t)
		}
	}
	return out
}
