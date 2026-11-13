package remote

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"sync"
	"time"
)

// Dial opens a connection to addr through the bridge: "unix:<path>" or
// "tcp:<host>:<port>", opened by the bridge's user (root with sudo).
func (b *Bridge) Dial(ctx context.Context, addr string) (net.Conn, error) {
	ch := make(chan frame, 1)
	b.mu.Lock()
	select {
	case <-b.dead:
		b.mu.Unlock()
		return nil, ErrBridgeClosed
	default:
	}
	b.next++
	id := b.next
	s := newStream(b, id)
	b.pending[id] = ch
	b.streams[id] = s
	b.mu.Unlock()
	fail := func(err error) (net.Conn, error) {
		b.mu.Lock()
		delete(b.pending, id)
		delete(b.streams, id)
		b.mu.Unlock()
		return nil, err
	}
	if err := b.send('O', id, []byte(addr)); err != nil {
		return fail(err)
	}
	select {
	case f, ok := <-ch:
		if !ok {
			return fail(ErrBridgeClosed)
		}
		if f.kind == 'e' {
			return fail(bridgeError(f.payload))
		}
		return s, nil
	case <-ctx.Done():
		_ = b.send('X', id, nil)
		return fail(ctx.Err())
	}
}

// stream is one relayed connection. Data from the server is buffered without
// limit, so one slow reader never stalls the others sharing the bridge.
type stream struct {
	b  *Bridge
	id uint32

	mu       sync.Mutex
	cond     *sync.Cond
	buf      []byte
	rerr     error // set when the server side closed
	closed   bool
	deadline time.Time
	timer    *time.Timer
}

func newStream(b *Bridge, id uint32) *stream {
	s := &stream{b: b, id: id}
	s.cond = sync.NewCond(&s.mu)
	return s
}

func (s *stream) deliver(p []byte) {
	s.mu.Lock()
	s.buf = append(s.buf, p...)
	s.mu.Unlock()
	s.cond.Broadcast()
}

func (s *stream) closeRemote(err error) {
	s.mu.Lock()
	if s.rerr == nil {
		s.rerr = err
	}
	s.mu.Unlock()
	s.cond.Broadcast()
}

func (s *stream) Read(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for len(s.buf) == 0 {
		switch {
		case s.closed:
			return 0, net.ErrClosed
		case s.rerr != nil:
			if errors.Is(s.rerr, ErrBridgeClosed) {
				return 0, s.rerr
			}
			return 0, io.EOF
		case !s.deadline.IsZero() && !time.Now().Before(s.deadline):
			return 0, os.ErrDeadlineExceeded
		}
		s.cond.Wait()
	}
	n := copy(p, s.buf)
	s.buf = s.buf[n:]
	return n, nil
}

const maxChunk = 32 << 10

func (s *stream) Write(p []byte) (int, error) {
	s.mu.Lock()
	closed, rerr := s.closed, s.rerr
	s.mu.Unlock()
	if closed {
		return 0, net.ErrClosed
	}
	if errors.Is(rerr, ErrBridgeClosed) {
		return 0, rerr
	}
	written := 0
	for len(p) > 0 {
		n := min(len(p), maxChunk)
		if err := s.b.send('D', s.id, p[:n]); err != nil {
			return written, err
		}
		written += n
		p = p[n:]
	}
	return written, nil
}

func (s *stream) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	if s.timer != nil {
		s.timer.Stop()
	}
	s.mu.Unlock()
	s.cond.Broadcast()
	s.b.mu.Lock()
	_, open := s.b.streams[s.id]
	delete(s.b.streams, s.id)
	s.b.mu.Unlock()
	if open {
		_ = s.b.send('X', s.id, nil)
	}
	return nil
}

type bridgeAddr struct{}

func (bridgeAddr) Network() string { return "visor-bridge" }
func (bridgeAddr) String() string  { return "visor-bridge" }

func (s *stream) LocalAddr() net.Addr  { return bridgeAddr{} }
func (s *stream) RemoteAddr() net.Addr { return bridgeAddr{} }

func (s *stream) SetDeadline(t time.Time) error { return s.SetReadDeadline(t) }

// SetReadDeadline wakes a blocked Read when t passes. Writes go straight to
// the bridge and need no deadline.
func (s *stream) SetReadDeadline(t time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deadline = t
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	if !t.IsZero() {
		s.timer = time.AfterFunc(time.Until(t), func() {
			// Take the lock so the wake-up can't fall between Read's
			// deadline check and its Wait.
			s.mu.Lock()
			s.mu.Unlock() //nolint:staticcheck // empty critical section on purpose
			s.cond.Broadcast()
		})
	}
	s.cond.Broadcast()
	return nil
}

func (s *stream) SetWriteDeadline(time.Time) error { return nil }
