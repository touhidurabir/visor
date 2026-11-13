// Package remote connects to supervisord on another machine over SSH. It
// installs nothing on the server: it forwards the supervisord socket when the
// SSH user may open it, and otherwise runs a small Python bridge for the
// length of the connection (with sudo when the server is set to use it).
package remote

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// Target is where to connect and as whom.
type Target struct {
	Host    string
	Port    int // 22 when zero
	User    string
	KeyPath string // private key file; empty means no key file
	// AgentSocket is an ssh-agent socket to try keys from (empty: none).
	AgentSocket string
	// Jump hosts, outermost first (ProxyJump).
	Jump []Target
}

// Addr is host:port.
func (t Target) Addr() string {
	p := t.Port
	if p == 0 {
		p = 22
	}
	return net.JoinHostPort(t.Host, strconv.Itoa(p))
}

// Secrets are kept in the Keychain or keyring and passed in per dial.
type Secrets struct {
	Password      string
	KeyPassphrase string
}

var (
	// ErrAuthFailed means the server accepted none of the offered methods.
	ErrAuthFailed = errors.New("the server refused the login")
	// ErrPassphraseNeeded means the key file is encrypted and no passphrase was given.
	ErrPassphraseNeeded = errors.New("the key file needs its passphrase")
	// ErrPassphraseWrong means the passphrase did not open the key file.
	ErrPassphraseWrong = errors.New("the passphrase did not open the key file")
	// ErrNoAuth means there was nothing to log in with.
	ErrNoAuth = errors.New("no password, key file or agent to log in with")
)

// DialTimeout bounds the TCP connect and SSH handshake when ctx has no deadline.
var DialTimeout = 15 * time.Second

// Dial connects and logs in. Host keys are checked against hk; an unknown key
// returns *UnknownHostError and a changed one *HostKeyChangedError.
func Dial(ctx context.Context, t Target, s Secrets, hk HostKeys) (*Conn, error) {
	cb, algos, err := hk.callback()
	if err != nil {
		return nil, err
	}
	var closers []func()
	cleanup := func() {
		for i := len(closers) - 1; i >= 0; i-- {
			closers[i]()
		}
	}
	dial := func(ctx context.Context, addr string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "tcp", addr)
	}
	// Each jump host is reached through the one before it.
	for _, j := range t.Jump {
		c, err := handshake(ctx, dial, j, s, cb, algos)
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("jump host %s: %w", j.Addr(), err)
		}
		closers = append(closers, func() { _ = c.Close() })
		dial = func(ctx context.Context, addr string) (net.Conn, error) { return dialVia(ctx, c, addr) }
	}
	var seen ssh.PublicKey
	final := func(host string, addr net.Addr, key ssh.PublicKey) error {
		err := cb(host, addr, key)
		if err == nil {
			seen = key
		}
		return err
	}
	client, err := handshake(ctx, dial, t, s, final, algos)
	if err != nil {
		cleanup()
		return nil, err
	}
	conn := newConn(client, t, cleanup)
	conn.hostKey = seen
	return conn, nil
}

func handshake(ctx context.Context, dial func(context.Context, string) (net.Conn, error), t Target, s Secrets, cb ssh.HostKeyCallback, algos func(string) []string) (*ssh.Client, error) {
	auth, closeAuth, skippedKey, err := authMethods(t, s)
	if err != nil {
		return nil, err
	}
	defer closeAuth()
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DialTimeout)
		defer cancel()
	}
	raw, err := dial(ctx, t.Addr())
	if err != nil {
		return nil, err
	}
	deadline, _ := ctx.Deadline()
	_ = raw.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
	defer stop()
	cfg := &ssh.ClientConfig{
		User:              t.User,
		Auth:              auth,
		HostKeyCallback:   cb,
		HostKeyAlgorithms: algos(t.Addr()),
		Timeout:           time.Until(deadline),
	}
	c, chans, reqs, err := ssh.NewClientConn(raw, t.Addr(), cfg)
	if err != nil {
		_ = raw.Close()
		var unknown *UnknownHostError
		var changed *HostKeyChangedError
		switch {
		case errors.As(err, &unknown):
			return nil, unknown
		case errors.As(err, &changed):
			return nil, changed
		case errors.Is(err, ErrAuthFailed) && skippedKey:
			return nil, ErrPassphraseNeeded
		case errors.Is(err, ErrAuthFailed):
			return nil, fmt.Errorf("%w (%s@%s)", ErrAuthFailed, t.User, t.Addr())
		case strings.Contains(err.Error(), "unable to authenticate") && skippedKey:
			return nil, ErrPassphraseNeeded
		case strings.Contains(err.Error(), "unable to authenticate"):
			return nil, fmt.Errorf("%w (%s@%s)", ErrAuthFailed, t.User, t.Addr())
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, err
	}
	_ = raw.SetDeadline(time.Time{})
	return ssh.NewClient(c, chans, reqs), nil
}

// authMethods offers the key file, then the agent, then the password (also
// answering keyboard-interactive password prompts).
func authMethods(t Target, s Secrets) (methods []ssh.AuthMethod, closeAll func(), skipped bool, err error) {
	closeAll = func() {}
	if t.KeyPath != "" {
		signer, err := loadKey(t.KeyPath, s.KeyPassphrase)
		switch {
		case errors.Is(err, ErrPassphraseNeeded) && t.AgentSocket != "":
			// Like ssh: an encrypted key with no passphrase saved is left
			// to the agent, which usually holds it unlocked.
			skipped = true
		case err != nil:
			return nil, closeAll, false, err
		default:
			methods = append(methods, ssh.PublicKeys(signer))
		}
	}
	if t.AgentSocket != "" {
		if m, closeAgent, err := agentAuth(t.AgentSocket); err == nil {
			methods = append(methods, m)
			closeAll = closeAgent
		}
	}
	if s.Password != "" {
		pw := s.Password
		triedPassword := false
		methods = append(methods,
			ssh.PasswordCallback(func() (string, error) {
				triedPassword = true
				return pw, nil
			}),
			// Servers that only take keyboard-interactive get the password
			// for a password prompt; anything else (a one-time code, a new
			// password) is not answered. After a refused password it is not
			// sent again, so one connect counts as one failed login.
			ssh.KeyboardInteractive(func(_, _ string, questions []string, _ []bool) ([]string, error) {
				if triedPassword {
					return nil, ErrAuthFailed
				}
				answers := make([]string, len(questions))
				for i, q := range questions {
					if !strings.Contains(strings.ToLower(q), "password") {
						return nil, ErrAuthFailed
					}
					answers[i] = pw
				}
				return answers, nil
			}),
		)
	}
	if len(methods) == 0 {
		if skipped {
			return nil, closeAll, false, ErrPassphraseNeeded
		}
		return nil, closeAll, false, ErrNoAuth
	}
	return methods, closeAll, skipped, nil
}

func loadKey(path, passphrase string) (ssh.Signer, error) {
	pem, err := os.ReadFile(expandHome(path))
	if err != nil {
		return nil, fmt.Errorf("read key file: %w", err)
	}
	signer, err := ssh.ParsePrivateKey(pem)
	var missing *ssh.PassphraseMissingError
	if errors.As(err, &missing) {
		if passphrase == "" {
			return nil, ErrPassphraseNeeded
		}
		signer, err = ssh.ParsePrivateKeyWithPassphrase(pem, []byte(passphrase))
		if err != nil {
			return nil, ErrPassphraseWrong
		}
		return signer, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read key file: %w", err)
	}
	return signer, nil
}

// ParseKeyFile reports whether path holds a private key Visor can use, and
// whether it needs a passphrase. The form uses it to check the field.
func ParseKeyFile(path string) (encrypted bool, err error) {
	_, err = loadKey(path, "")
	if errors.Is(err, ErrPassphraseNeeded) {
		return true, nil
	}
	return false, err
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return home + p[1:]
		}
	}
	return p
}

// dialVia opens addr through a jump host, giving up when ctx ends.
func dialVia(ctx context.Context, c *ssh.Client, addr string) (net.Conn, error) {
	type result struct {
		c   net.Conn
		err error
	}
	ch := make(chan result, 1)
	go func() {
		conn, err := c.Dial("tcp", addr)
		ch <- result{conn, err}
	}()
	select {
	case r := <-ch:
		return r.c, r.err
	case <-ctx.Done():
		go func() {
			if r := <-ch; r.c != nil {
				_ = r.c.Close()
			}
		}()
		return nil, ctx.Err()
	}
}
