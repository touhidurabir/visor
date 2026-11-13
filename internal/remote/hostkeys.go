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
func (h HostKeys) callback() (ssh.HostKeyCallback, error) {
	var files []string
	for _, f := range append(append([]string{}, h.UserFiles...), h.VisorFile) {
		if f == "" {
			continue
		}
		if _, err := os.Stat(f); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		files = append(files, f)
	}
	var known ssh.HostKeyCallback
	if len(files) > 0 {
		cb, err := knownhosts.New(files...)
		if err != nil {
			return nil, fmt.Errorf("read known hosts: %w", err)
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
	}, nil
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
