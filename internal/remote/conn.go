package remote

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"

	"golang.org/x/crypto/ssh"
)

// Conn is one logged-in SSH connection. Everything for a server (socket
// forwarding, the bridge, file reads) goes over it.
type Conn struct {
	client  *ssh.Client
	hostKey ssh.PublicKey
	target  Target
	cleanup func()

	closeOnce sync.Once
	closed    chan struct{}
	closeErr  error
	mu        sync.Mutex
}

func newConn(c *ssh.Client, t Target, cleanup func()) *Conn {
	conn := &Conn{client: c, target: t, cleanup: cleanup, closed: make(chan struct{})}
	go func() {
		err := c.Wait()
		conn.markClosed(err)
	}()
	go conn.keepalive(KeepaliveInterval, KeepaliveMax)
	return conn
}

func (c *Conn) markClosed(err error) {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closeErr = err
		c.mu.Unlock()
		close(c.closed)
		if c.cleanup != nil {
			c.cleanup()
		}
	})
}

// Target is what this connection was opened with.
func (c *Conn) Target() Target { return c.target }

// Closed is closed when the connection ends, for whatever reason.
func (c *Conn) Closed() <-chan struct{} { return c.closed }

// Err is why the connection ended (nil while it is open or after Close).
func (c *Conn) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closeErr
}

// Close ends the connection and everything running over it.
func (c *Conn) Close() error {
	err := c.client.Close()
	c.markClosed(nil)
	return err
}

// DialUnix opens a unix socket on the server (direct-streamlocal).
func (c *Conn) DialUnix(path string) (net.Conn, error) { return c.client.Dial("unix", path) }

// DialTCP opens host:port as seen from the server (direct-tcpip).
func (c *Conn) DialTCP(addr string) (net.Conn, error) { return c.client.Dial("tcp", addr) }

// ExitError is a command that ran and exited non-zero.
type ExitError struct {
	Status int
	Stderr string
}

func (e *ExitError) Error() string {
	if e.Stderr != "" {
		return fmt.Sprintf("exit status %d: %s", e.Status, bytes.TrimSpace([]byte(e.Stderr)))
	}
	return fmt.Sprintf("exit status %d", e.Status)
}

// Run runs a short command and returns its stdout. A non-zero exit returns
// *ExitError with stderr.
func (c *Conn) Run(ctx context.Context, cmd string, stdin io.Reader) ([]byte, error) {
	s, err := c.client.NewSession()
	if err != nil {
		return nil, err
	}
	defer func() { _ = s.Close() }()
	var out, errb bytes.Buffer
	s.Stdout, s.Stderr, s.Stdin = &out, &errb, stdin
	done := make(chan error, 1)
	go func() { done <- s.Run(cmd) }()
	select {
	case err = <-done:
	case <-ctx.Done():
		_ = s.Close()
		return nil, ctx.Err()
	}
	var ee *ssh.ExitError
	if errors.As(err, &ee) {
		return out.Bytes(), &ExitError{Status: ee.ExitStatus(), Stderr: errb.String()}
	}
	return out.Bytes(), err
}

// Session starts a command with its stdin, stdout and stderr as pipes.
func (c *Conn) Session(cmd string) (*Process, error) {
	s, err := c.client.NewSession()
	if err != nil {
		return nil, err
	}
	in, err := s.StdinPipe()
	if err != nil {
		_ = s.Close()
		return nil, err
	}
	out, err := s.StdoutPipe()
	if err != nil {
		_ = s.Close()
		return nil, err
	}
	errp, err := s.StderrPipe()
	if err != nil {
		_ = s.Close()
		return nil, err
	}
	if err := s.Start(cmd); err != nil {
		_ = s.Close()
		return nil, err
	}
	p := &Process{Stdin: in, Stdout: out, Stderr: errp, s: s, done: make(chan struct{})}
	go func() { p.err = s.Wait(); close(p.done) }()
	return p, nil
}

// Process is a running remote command.
type Process struct {
	Stdin  io.WriteCloser
	Stdout io.Reader
	Stderr io.Reader
	s      *ssh.Session
	done   chan struct{}
	err    error
}

// Done is closed when the command exits.
func (p *Process) Done() <-chan struct{} { return p.done }

// Err is the command's exit error once Done is closed.
func (p *Process) Err() error { <-p.done; return p.err }

// Kill closes the session (the server sends the command SIGHUP).
func (p *Process) Kill() { _ = p.s.Close() }

// HostKey is the key the server proved itself with.
func (c *Conn) HostKey() ssh.PublicKey { return c.hostKey }
