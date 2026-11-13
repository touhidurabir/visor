package remote

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

//go:embed bridge.py
var bridgeSource []byte

// The bootstrap is short enough for a command line. It announces itself,
// then reads the bridge's length and source from stdin and runs it, so the
// bridge never touches the server's disk.
const bootstrap = `import os,sys
os.write(1,b"VISOR-BOOT\n")
def rl():
 b=b""
 while not b.endswith(b"\n"):
  c=os.read(0,1)
  if not c: sys.exit(3)
  b+=c
 return b
n=int(rl())
s=b""
while len(s)<n:
 c=os.read(0,n-len(s))
 if not c: sys.exit(3)
 s+=c
exec(compile(s,"visor-bridge","exec"))`

// sudoPrompt is what sudo -S prints when it wants the password; Visor
// answers it once.
const sudoPrompt = "[visor-sudo]"

var (
	// ErrNoPython means neither python3 nor python is on the server's PATH.
	ErrNoPython = errors.New("neither python3 nor python is installed on the server")
	// ErrSudoRefused means sudo did not accept the password.
	ErrSudoRefused = errors.New("sudo did not accept the password")
	// ErrSudoPasswordNeeded means sudo asked for a password and none is saved.
	ErrSudoPasswordNeeded = errors.New("sudo asked for a password and none is saved")
	// ErrSudoNotAllowed means the user may not run commands with sudo.
	ErrSudoNotAllowed = errors.New("this user may not use sudo")
	// ErrSudoNeedsTTY means sudo is set to require a terminal (requiretty).
	ErrSudoNeedsTTY = errors.New("sudo on the server requires a terminal (requiretty)")
	// ErrBridgeClosed means the bridge stopped; the next use starts a new one.
	ErrBridgeClosed = errors.New("the connection to the server's helper ended")
)

// BridgeOptions say how to start the bridge.
type BridgeOptions struct {
	Sudo         bool
	SudoPassword string
	// Python overrides the interpreter (default: python3, else python).
	Python string
}

// Hello is what the bridge reports about itself when it starts.
type Hello struct {
	UID    int      `json:"uid"`
	User   string   `json:"user"`
	Python string   `json:"python"`
	Groups []string `json:"groups"`
}

// BridgeStartTimeout bounds sudo and the bridge's start.
var BridgeStartTimeout = 20 * time.Second

// Bridge is a running bridge on the server.
type Bridge struct {
	proc  *Process
	hello Hello

	wmu     sync.Mutex
	mu      sync.Mutex
	next    uint32
	pending map[uint32]chan frame
	streams map[uint32]*stream
	dead    chan struct{}
	deadErr error
	stderr  *tailBuffer
}

type frame struct {
	kind    byte
	id      uint32
	payload []byte
}

// FindPython returns the server's python3 (or python) path.
func FindPython(ctx context.Context, c *Conn) (string, error) {
	out, err := c.Run(ctx, "command -v python3 || command -v python", nil)
	p := strings.TrimSpace(string(out))
	if p == "" || err != nil {
		return "", ErrNoPython
	}
	return strings.Fields(p)[0], nil
}

// StartBridge starts the bridge over c, with sudo when o.Sudo is set.
func StartBridge(ctx context.Context, c *Conn, o BridgeOptions) (*Bridge, error) {
	ctx, cancel := context.WithTimeout(ctx, BridgeStartTimeout)
	defer cancel()
	py := o.Python
	if py == "" {
		var err error
		if py, err = FindPython(ctx, c); err != nil {
			return nil, err
		}
	}
	cmd := shellQuote(py) + " -c " + shellQuote(bootstrap)
	if o.Sudo {
		cmd = "sudo -S -p " + shellQuote(sudoPrompt) + " " + cmd
	}
	proc, err := c.Session(cmd)
	if err != nil {
		return nil, err
	}
	b := &Bridge{
		proc:    proc,
		pending: map[uint32]chan frame{},
		streams: map[uint32]*stream{},
		dead:    make(chan struct{}),
		stderr:  &tailBuffer{max: 4096},
	}
	if err := b.boot(ctx, o); err != nil {
		proc.Kill()
		return nil, err
	}
	go b.readLoop()
	go func() {
		<-proc.Done()
		b.die(ErrBridgeClosed)
	}()
	return b, nil
}

// boot answers sudo's prompt (once), waits for the bootstrap, sends the
// bridge and reads its hello.
func (b *Bridge) boot(ctx context.Context, o BridgeOptions) error {
	prompts := make(chan struct{}, 4)
	go func() {
		buf := make([]byte, 1024)
		var seen strings.Builder
		for {
			n, err := b.proc.Stderr.Read(buf)
			if n > 0 {
				_, _ = b.stderr.Write(buf[:n])
				seen.Write(buf[:n])
				for strings.Contains(seen.String(), sudoPrompt) {
					s := strings.Replace(seen.String(), sudoPrompt, "", 1)
					seen.Reset()
					seen.WriteString(s)
					select {
					case prompts <- struct{}{}:
					default:
					}
				}
			}
			if err != nil {
				return
			}
		}
	}()
	booted := make(chan error, 1)
	go func() {
		line := make([]byte, 0, 16)
		one := make([]byte, 1)
		for {
			if _, err := io.ReadFull(b.proc.Stdout, one); err != nil {
				booted <- err
				return
			}
			line = append(line, one[0])
			if one[0] == '\n' {
				if string(line) == "VISOR-BOOT\n" {
					booted <- nil
					return
				}
				line = line[:0]
			}
		}
	}()
	answered := false
	for {
		select {
		case <-prompts:
			if answered {
				return ErrSudoRefused
			}
			if o.SudoPassword == "" {
				return ErrSudoPasswordNeeded
			}
			answered = true
			if _, err := io.WriteString(b.proc.Stdin, o.SudoPassword+"\n"); err != nil {
				return err
			}
		case err := <-booted:
			if err != nil {
				return b.startError()
			}
			src := strconv.Itoa(len(bridgeSource)) + "\n"
			if _, err := io.WriteString(b.proc.Stdin, src); err != nil {
				return err
			}
			if _, err := b.proc.Stdin.Write(bridgeSource); err != nil {
				return err
			}
			f, err := readFrame(b.proc.Stdout)
			if err != nil || f.kind != 'H' {
				return b.startError()
			}
			if err := json.Unmarshal(f.payload, &b.hello); err != nil {
				return fmt.Errorf("bridge hello: %w", err)
			}
			return nil
		case <-ctx.Done():
			return fmt.Errorf("starting the helper on the server: %w", ctx.Err())
		}
	}
}

// startError explains why the bridge exited before its hello.
func (b *Bridge) startError() error {
	select {
	case <-b.proc.Done():
	case <-time.After(2 * time.Second):
	}
	msg := b.stderr.String()
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "incorrect password"), strings.Contains(low, "sorry, try again"):
		return ErrSudoRefused
	case strings.Contains(low, "not in the sudoers"), strings.Contains(low, "not allowed to"), strings.Contains(low, "may not run sudo"):
		return ErrSudoNotAllowed
	case strings.Contains(low, "tty"), strings.Contains(low, "terminal is required"):
		return ErrSudoNeedsTTY
	case strings.Contains(low, "sudo: command not found"), strings.Contains(low, "sudo: not found"):
		return errors.New("sudo is not installed on the server")
	}
	msg = strings.TrimSpace(strings.ReplaceAll(msg, sudoPrompt, ""))
	if msg == "" {
		return ErrBridgeClosed
	}
	return fmt.Errorf("the helper on the server stopped: %s", msg)
}

// Hello is what the bridge reported at start (its user and groups).
func (b *Bridge) Hello() Hello { return b.hello }

// Done is closed when the bridge stops.
func (b *Bridge) Done() <-chan struct{} { return b.dead }

// Close stops the bridge.
func (b *Bridge) Close() {
	b.proc.Kill()
	b.die(ErrBridgeClosed)
}

func (b *Bridge) die(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	select {
	case <-b.dead:
		return
	default:
	}
	b.deadErr = err
	close(b.dead)
	for id, ch := range b.pending {
		close(ch)
		delete(b.pending, id)
	}
	for id, s := range b.streams {
		s.closeRemote(err)
		delete(b.streams, id)
	}
}

func readFrame(r io.Reader) (frame, error) {
	var h [9]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return frame{}, err
	}
	n := binary.BigEndian.Uint32(h[5:])
	if n > 64<<20 {
		return frame{}, errors.New("bridge frame too large")
	}
	f := frame{kind: h[0], id: binary.BigEndian.Uint32(h[1:5]), payload: make([]byte, n)}
	_, err := io.ReadFull(r, f.payload)
	return f, err
}

func (b *Bridge) send(kind byte, id uint32, payload []byte) error {
	select {
	case <-b.dead:
		return ErrBridgeClosed
	default:
	}
	var h [9]byte
	h[0] = kind
	binary.BigEndian.PutUint32(h[1:5], id)
	binary.BigEndian.PutUint32(h[5:], uint32(len(payload)))
	b.wmu.Lock()
	defer b.wmu.Unlock()
	if _, err := b.proc.Stdin.Write(append(h[:], payload...)); err != nil {
		b.die(ErrBridgeClosed)
		return ErrBridgeClosed
	}
	return nil
}

func (b *Bridge) readLoop() {
	for {
		f, err := readFrame(b.proc.Stdout)
		if err != nil {
			b.die(ErrBridgeClosed)
			return
		}
		b.mu.Lock()
		if s, ok := b.streams[f.id]; ok && (f.kind == 'D' || f.kind == 'X') {
			if f.kind == 'X' {
				delete(b.streams, f.id)
			}
			b.mu.Unlock()
			if f.kind == 'D' {
				s.deliver(f.payload)
			} else {
				s.closeRemote(io.EOF)
			}
			continue
		}
		ch, ok := b.pending[f.id]
		if ok {
			delete(b.pending, f.id)
		}
		b.mu.Unlock()
		if ok {
			ch <- f
		}
	}
}

// request sends one frame and waits for its reply.
func (b *Bridge) request(ctx context.Context, kind byte, payload []byte) (frame, error) {
	ch := make(chan frame, 1)
	b.mu.Lock()
	select {
	case <-b.dead:
		b.mu.Unlock()
		return frame{}, ErrBridgeClosed
	default:
	}
	b.next++
	id := b.next
	b.pending[id] = ch
	b.mu.Unlock()
	if err := b.send(kind, id, payload); err != nil {
		return frame{}, err
	}
	select {
	case f, ok := <-ch:
		if !ok {
			return frame{}, ErrBridgeClosed
		}
		if f.kind == 'e' {
			return frame{}, bridgeError(f.payload)
		}
		return f, nil
	case <-ctx.Done():
		b.mu.Lock()
		delete(b.pending, id)
		b.mu.Unlock()
		return frame{}, ctx.Err()
	}
}

// RemoteError is an error the bridge reported, with the server's errno.
type RemoteError struct {
	Errno int
	Msg   string
}

func (e *RemoteError) Error() string { return e.Msg }

// Is maps the server's errno to the fs errors callers check.
func (e *RemoteError) Is(target error) bool {
	switch syscall.Errno(e.Errno) {
	case syscall.ENOENT:
		return target == fs.ErrNotExist
	case syscall.EACCES, syscall.EPERM:
		return target == fs.ErrPermission
	}
	return false
}

func bridgeError(p []byte) error {
	var e RemoteError
	if err := json.Unmarshal(p, &e); err != nil {
		return errors.New(string(p))
	}
	return &e
}

// Ping checks the bridge answers.
func (b *Bridge) Ping(ctx context.Context) error {
	_, err := b.request(ctx, 'P', nil)
	return err
}

// tailBuffer keeps the last max bytes written to it.
type tailBuffer struct {
	mu  sync.Mutex
	max int
	buf []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.max {
		t.buf = t.buf[len(t.buf)-t.max:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(bytes.Clone(t.buf))
}

// shellQuote quotes s for a POSIX shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
