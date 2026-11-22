package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"sync"
	"time"

	"github.com/touhidurabir/visor/internal/issues"
	"github.com/touhidurabir/visor/internal/remote"
)

// Connection states of a server tab (HostView.Conn).
const (
	ConnConnected    = "connected"
	ConnReconnecting = "reconnecting"
	ConnFailed       = "failed" // a password or host key needs the user
)

// Reconnect backoff after a server's SSH connection drops: 2s, 4s, … 60s.
var (
	reconnectFirst = 2 * time.Second
	reconnectMax   = 60 * time.Second
)

// remoteLink is a server tab's way to supervisord and its files that
// outlives one SSH connection: the client, monitor and watcher keep it, and
// a reconnect swaps the session underneath. While the connection is down,
// every call fails with issues.ErrHostDown, so polls open "can't reach the
// server over SSH", not "supervisord isn't responding".
type remoteLink struct {
	mu    sync.Mutex
	sess  *RemoteSession
	down  error // why, while not connected
	state string
	kick  chan struct{} // Retry: try now instead of waiting out the backoff
}

func newLink(s *RemoteSession) *remoteLink {
	return &remoteLink{sess: s, state: ConnConnected, kick: make(chan struct{}, 1)}
}

// retry asks a reconnecting link to try again now (a no-op otherwise).
func (l *remoteLink) retry() {
	select {
	case l.kick <- struct{}{}:
	default:
	}
}

func (l *remoteLink) current() (*RemoteSession, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.down != nil {
		return nil, fmt.Errorf("%w (%v)", issues.ErrHostDown, l.down)
	}
	return l.sess, nil
}

// DialContext reaches supervisord through the current session.
func (l *remoteLink) DialContext(ctx context.Context) (net.Conn, error) {
	s, err := l.current()
	if err != nil {
		return nil, err
	}
	return s.Discovery.Transport.DialContext(ctx)
}

// URL is the current session's endpoint.
func (l *remoteLink) URL() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.sess.Discovery.Transport.URL()
}

// ReadFile, Stat and Glob read the server's files (config.FS).
func (l *remoteLink) ReadFile(name string) ([]byte, error) {
	s, err := l.current()
	if err != nil {
		return nil, &fs.PathError{Op: "read", Path: name, Err: err}
	}
	return s.Discovery.Files().ReadFile(name)
}

func (l *remoteLink) Stat(name string) (fs.FileInfo, error) {
	s, err := l.current()
	if err != nil {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: err}
	}
	return s.Discovery.Files().Stat(name)
}

func (l *remoteLink) Glob(pattern string) ([]string, error) {
	s, err := l.current()
	if err != nil {
		return nil, err
	}
	return s.Discovery.Files().Glob(pattern)
}

// Tail reads the end of a file on the server through the current session.
func (l *remoteLink) Tail(name string, most int) ([]byte, error) {
	s, err := l.current()
	if err != nil {
		return nil, err
	}
	if s.Tail == nil {
		return nil, errors.New("no file access on this server")
	}
	return s.Tail(name, most)
}

// lost is closed when the current session's SSH connection ends.
func (l *remoteLink) lost() <-chan struct{} {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.sess.Lost
}

func (l *remoteLink) setDown(err error, state string) {
	l.mu.Lock()
	l.down, l.state = err, state
	l.mu.Unlock()
}

// swap puts a new session in place and returns the old one (to close).
func (l *remoteLink) swap(s *RemoteSession) *RemoteSession {
	l.mu.Lock()
	defer l.mu.Unlock()
	old := l.sess
	l.sess, l.down, l.state = s, nil, ConnConnected
	return old
}

// close ends the current session.
func (l *remoteLink) close() {
	l.mu.Lock()
	s := l.sess
	l.mu.Unlock()
	if s != nil && s.Close != nil {
		s.Close()
	}
}

func (l *remoteLink) status() (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.state, l.down
}

// RetryNow makes a server tab that is reconnecting try at once.
func (r *Runtime) RetryNow() {
	r.mu.Lock()
	l := r.link
	r.mu.Unlock()
	if l != nil {
		l.retry()
	}
}

// errConnectionLost is why a session ended when the connection says nothing.
var errConnectionLost = errors.New("the SSH connection was lost")

// watchLink reconnects a server tab whose SSH connection drops, with
// backoff, until ctx ends. A refused password or a changed host key stops
// it: trying again would count against the server's lockout, or trust a
// server Visor shouldn't.
func (r *Runtime) watchLink(ctx context.Context, l *remoteLink, src *RemoteSource) {
	defer r.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case <-l.lost():
		}
		if ctx.Err() != nil {
			return
		}
		l.setDown(errConnectionLost, ConnReconnecting)
		r.emitOverview()
		select { // a Retry made while connected is stale
		case <-l.kick:
		default:
		}
		wait := reconnectFirst
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			case <-l.kick:
				wait = reconnectFirst
			}
			// As saved now, and with the config this tab found first.
			cur, _ := r.remoteServer()
			r.mu.Lock()
			if r.d.ConfigPath != "" {
				cur.ConfigPath = r.d.ConfigPath
			}
			r.mu.Unlock()
			s, err := src.Connect(ctx, cur)
			if err == nil {
				if ctx.Err() != nil {
					s.Close()
					return
				}
				old := l.swap(s)
				if old != nil && old.Close != nil {
					old.Close()
				}
				r.mu.Lock()
				r.session = s
				client, mon := r.client, r.mon
				r.mu.Unlock()
				if client != nil {
					client.Close() // drop connections kept over the old session
				}
				if mon != nil {
					mon.Poke()
				}
				r.emitOverview()
				break
			}
			if ctx.Err() != nil {
				return
			}
			var unknown *remote.UnknownHostError
			var changed *remote.HostKeyChangedError
			if remote.IsCredentialError(err) || errors.As(err, &unknown) || errors.As(err, &changed) {
				l.setDown(err, ConnFailed)
				r.emitOverview()
				return
			}
			l.setDown(err, ConnReconnecting)
			wait = min(wait*2, reconnectMax)
		}
	}
}
