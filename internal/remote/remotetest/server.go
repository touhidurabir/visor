// Package remotetest runs an in-process SSH server for tests. It logs in with
// a password or public key, runs exec requests as local `sh -c` commands, and
// forwards direct-streamlocal (unix socket) and direct-tcpip channels. It
// binds to 127.0.0.1 only and is closed when the test ends.
package remotetest

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"testing"

	"golang.org/x/crypto/ssh"
)

// Options configure a Server.
type Options struct {
	User           string          // login name (default "visor")
	Password       string          // accepted password ("" = password login off)
	AuthorizedKeys []ssh.PublicKey // accepted keys
	// KeyboardInteractive also accepts Password through keyboard-interactive.
	KeyboardInteractive bool
	// Env is added to every command's environment (e.g. a PATH with a fake sudo).
	Env []string
	// NoStreamLocal refuses direct-streamlocal, like AllowStreamLocalForwarding no.
	NoStreamLocal bool
	// HostKey replaces the generated host key (to simulate a changed key).
	HostKey ssh.Signer
	// ExtraHostKeys are offered too (a server with ECDSA, RSA and Ed25519 keys).
	ExtraHostKeys []ssh.Signer
}

// Server is a running test SSH server.
type Server struct {
	Addr    string
	Host    string
	Port    int
	HostKey ssh.PublicKey

	opts    Options
	ln      net.Listener
	cfg     *ssh.ServerConfig
	mu      sync.Mutex
	execs   []string
	unix    []string
	tcp     []string
	conns   map[net.Conn]struct{}
	frozen  bool
	wg      sync.WaitGroup
	pwTries int
}

// PasswordAttempts counts password and keyboard-interactive logins tried.
func (s *Server) PasswordAttempts() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pwTries
}

func (s *Server) countTry() {
	s.mu.Lock()
	s.pwTries++
	s.mu.Unlock()
}

// NewHostKey returns a fresh ed25519 host key.
func NewHostKey(t testing.TB) ssh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// Start runs a server until the test ends.
func Start(t testing.TB, o Options) *Server {
	t.Helper()
	if o.HostKey == nil {
		o.HostKey = NewHostKey(t)
	}
	s, err := Serve(o, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

// GenerateHostKey returns a fresh ed25519 host key.
func GenerateHostKey() (ssh.Signer, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return ssh.NewSignerFromKey(priv)
}

// Serve runs a server on addr (a 127.0.0.1 address) until Close. The e2e
// SSH server (cmd/visor-e2e-ssh) uses it outside tests.
func Serve(o Options, addr string) (*Server, error) {
	if o.User == "" {
		o.User = "visor"
	}
	hk := o.HostKey
	if hk == nil {
		var err error
		if hk, err = GenerateHostKey(); err != nil {
			return nil, err
		}
	}
	s := &Server{opts: o, HostKey: hk.PublicKey(), conns: map[net.Conn]struct{}{}}
	s.cfg = &ssh.ServerConfig{}
	if o.Password != "" {
		s.cfg.PasswordCallback = func(c ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
			s.countTry()
			if c.User() == o.User && string(pw) == o.Password {
				return nil, nil
			}
			return nil, errors.New("wrong password")
		}
		if o.KeyboardInteractive {
			s.cfg.KeyboardInteractiveCallback = func(c ssh.ConnMetadata, ch ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
				ans, err := ch("", "", []string{"Password: "}, []bool{false})
				if err == nil {
					s.countTry()
				}
				if err == nil && c.User() == o.User && len(ans) == 1 && ans[0] == o.Password {
					return nil, nil
				}
				return nil, errors.New("wrong password")
			}
		}
	}
	if len(o.AuthorizedKeys) > 0 {
		s.cfg.PublicKeyCallback = func(c ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
			if c.User() != o.User {
				return nil, errors.New("unknown user")
			}
			for _, a := range o.AuthorizedKeys {
				if string(a.Marshal()) == string(k.Marshal()) {
					return nil, nil
				}
			}
			return nil, errors.New("unknown key")
		}
	}
	for _, k := range o.ExtraHostKeys {
		s.cfg.AddHostKey(k)
	}
	s.cfg.AddHostKey(hk)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	s.ln = ln
	s.Addr = ln.Addr().String()
	host, port, _ := net.SplitHostPort(s.Addr)
	s.Host = host
	s.Port, _ = strconv.Atoi(port)
	s.wg.Add(1)
	go s.serve()
	return s, nil
}

// Close stops the server and drops every connection.
func (s *Server) Close() {
	_ = s.ln.Close()
	s.DropAll()
	s.wg.Wait()
}

// DropAll closes every open client connection (the listener stays up).
func (s *Server) DropAll() {
	s.mu.Lock()
	for c := range s.conns {
		_ = c.Close()
	}
	s.mu.Unlock()
}

// Freeze stops answering on open connections without closing them, like a
// network that went away (keepalive tests). New connections are refused.
func (s *Server) Freeze() {
	s.mu.Lock()
	s.frozen = true
	s.mu.Unlock()
}

// Execs returns the commands run so far.
func (s *Server) Execs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.execs...)
}

// UnixDials returns the socket paths forwarded so far.
func (s *Server) UnixDials() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.unix...)
}

func (s *Server) serve() {
	defer s.wg.Done()
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		frozen := s.frozen
		if !frozen {
			s.conns[c] = struct{}{}
		}
		s.mu.Unlock()
		if frozen {
			_ = c.Close()
			continue
		}
		go s.handle(c)
	}
}

// freezeConn wraps a connection so reads block forever once the server is frozen.
type freezeConn struct {
	net.Conn
	s *Server
}

func (f freezeConn) Read(p []byte) (int, error) {
	n, err := f.Conn.Read(p)
	f.s.mu.Lock()
	frozen := f.s.frozen
	f.s.mu.Unlock()
	if frozen {
		select {} // never answer again; the client's keepalive must notice
	}
	return n, err
}

func (s *Server) handle(c net.Conn) {
	defer func() {
		s.mu.Lock()
		delete(s.conns, c)
		s.mu.Unlock()
		_ = c.Close()
	}()
	sc, chans, reqs, err := ssh.NewServerConn(freezeConn{c, s}, s.cfg)
	if err != nil {
		return
	}
	defer func() { _ = sc.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ssh.DiscardRequests(reqs)
	for nc := range chans {
		switch nc.ChannelType() {
		case "session":
			go s.session(ctx, nc)
		case "direct-streamlocal@openssh.com":
			if s.opts.NoStreamLocal {
				_ = nc.Reject(ssh.Prohibited, "streamlocal forwarding disabled")
				continue
			}
			var p struct {
				Path      string
				Reserved0 string
				Reserved1 uint32
			}
			if err := ssh.Unmarshal(nc.ExtraData(), &p); err != nil {
				_ = nc.Reject(ssh.ConnectionFailed, err.Error())
				continue
			}
			s.mu.Lock()
			s.unix = append(s.unix, p.Path)
			s.mu.Unlock()
			go forward(nc, "unix", p.Path)
		case "direct-tcpip":
			var p struct {
				Host     string
				Port     uint32
				OrigHost string
				OrigPort uint32
			}
			if err := ssh.Unmarshal(nc.ExtraData(), &p); err != nil {
				_ = nc.Reject(ssh.ConnectionFailed, err.Error())
				continue
			}
			addr := net.JoinHostPort(p.Host, strconv.Itoa(int(p.Port)))
			s.mu.Lock()
			s.tcp = append(s.tcp, addr)
			s.mu.Unlock()
			go forward(nc, "tcp", addr)
		default:
			_ = nc.Reject(ssh.UnknownChannelType, "unsupported")
		}
	}
}

func forward(nc ssh.NewChannel, network, addr string) {
	target, err := net.Dial(network, addr)
	if err != nil {
		_ = nc.Reject(ssh.ConnectionFailed, err.Error())
		return
	}
	ch, reqs, err := nc.Accept()
	if err != nil {
		_ = target.Close()
		return
	}
	go ssh.DiscardRequests(reqs)
	go func() {
		_, _ = io.Copy(target, ch)
		if cw, ok := target.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		}
	}()
	_, _ = io.Copy(ch, target)
	_ = ch.Close()
	_ = target.Close()
}

func (s *Server) session(ctx context.Context, nc ssh.NewChannel) {
	ch, reqs, err := nc.Accept()
	if err != nil {
		return
	}
	defer func() { _ = ch.Close() }()
	for req := range reqs {
		switch req.Type {
		case "env", "pty-req":
			_ = req.Reply(true, nil)
		case "exec":
			var p struct{ Command string }
			if err := ssh.Unmarshal(req.Payload, &p); err != nil {
				_ = req.Reply(false, nil)
				return
			}
			_ = req.Reply(true, nil)
			s.mu.Lock()
			s.execs = append(s.execs, p.Command)
			s.mu.Unlock()
			status := s.run(ctx, ch, p.Command)
			b := make([]byte, 4)
			binary.BigEndian.PutUint32(b, uint32(status))
			_, _ = ch.SendRequest("exit-status", false, b)
			return
		default:
			_ = req.Reply(false, nil)
		}
	}
}

func (s *Server) run(ctx context.Context, ch ssh.Channel, command string) int {
	cmd := exec.Command("/bin/sh", "-c", command)
	cmd.Env = append(os.Environ(), s.opts.Env...)
	cmd.Stdout = ch
	cmd.Stderr = ch.Stderr()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	in, err := cmd.StdinPipe()
	if err != nil {
		return 255
	}
	if err := cmd.Start(); err != nil {
		return 127
	}
	go func() {
		_, _ = io.Copy(in, ch)
		_ = in.Close()
	}()
	// When the connection ends, stop the command and its children.
	done := make(chan struct{})
	go func() {
		select {
		case <-done:
		case <-ctx.Done():
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
	}()
	err = cmd.Wait()
	close(done)
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if st := ee.ExitCode(); st >= 0 {
			return st
		}
		return 255
	}
	if err != nil {
		return 255
	}
	return 0
}
