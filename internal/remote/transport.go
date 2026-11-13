package remote

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
)

// BridgeTransport reaches supervisord through a bridge (a supervisor.Transport).
// When the bridge stops, the next call starts a new one, except after a sudo
// refusal: that is kept until Reset so a wrong password is never retried
// against the server's lockout rules.
type BridgeTransport struct {
	Conn    *Conn
	Options BridgeOptions
	// Addr is "unix:<path>" or "tcp:<host>:<port>".
	Addr string

	mu     sync.Mutex
	b      *Bridge
	failed error
}

// Bridge returns the running bridge, starting one when needed.
func (t *BridgeTransport) Bridge(ctx context.Context) (*Bridge, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.failed != nil {
		return nil, t.failed
	}
	if t.b != nil {
		select {
		case <-t.b.Done():
			t.b = nil
		default:
			return t.b, nil
		}
	}
	b, err := StartBridge(ctx, t.Conn, t.Options)
	if err != nil {
		if sticky(err) {
			t.failed = err
		}
		return nil, err
	}
	t.b = b
	return b, nil
}

// IsCredentialError reports a login or sudo password problem: trying again
// without a change can only count against the server's lockout.
func IsCredentialError(err error) bool {
	return sticky(err) || errors.Is(err, ErrAuthFailed) || errors.Is(err, ErrPassphraseWrong) || errors.Is(err, ErrPassphraseNeeded)
}

// sticky errors need the user to change something before a retry.
func sticky(err error) bool {
	return errors.Is(err, ErrSudoRefused) || errors.Is(err, ErrSudoPasswordNeeded) || errors.Is(err, ErrSudoUnconfirmed) ||
		errors.Is(err, ErrSudoNotAllowed) || errors.Is(err, ErrSudoNeedsTTY) || errors.Is(err, ErrNoPython)
}

// Reset forgets a kept failure (after the user changed the sudo password).
func (t *BridgeTransport) Reset() {
	t.mu.Lock()
	t.failed = nil
	t.mu.Unlock()
}

// DialContext opens a connection to supervisord through the bridge.
func (t *BridgeTransport) DialContext(ctx context.Context) (net.Conn, error) {
	b, err := t.Bridge(ctx)
	if err != nil {
		return nil, err
	}
	return b.Dial(ctx, t.Addr)
}

// URL is the XML-RPC endpoint; the host is a placeholder for a socket.
func (t *BridgeTransport) URL() string { return rpcURL(t.Addr) }

// Close stops the bridge.
func (t *BridgeTransport) Close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.b != nil {
		t.b.Close()
		t.b = nil
	}
}

// SocketTransport forwards supervisord's socket over SSH, with no process on
// the server (direct-streamlocal for a unix socket, direct-tcpip for TCP).
type SocketTransport struct {
	Conn *Conn
	Addr string // "unix:<path>" or "tcp:<host>:<port>"
}

// DialContext opens a forwarded connection.
func (t *SocketTransport) DialContext(ctx context.Context) (net.Conn, error) {
	type result struct {
		c   net.Conn
		err error
	}
	ch := make(chan result, 1)
	go func() {
		var r result
		if path, ok := strings.CutPrefix(t.Addr, "unix:"); ok {
			r.c, r.err = t.Conn.DialUnix(path)
		} else {
			r.c, r.err = t.Conn.DialTCP(strings.TrimPrefix(t.Addr, "tcp:"))
		}
		ch <- r
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

// URL is the XML-RPC endpoint.
func (t *SocketTransport) URL() string { return rpcURL(t.Addr) }

func rpcURL(addr string) string {
	if hp, ok := strings.CutPrefix(addr, "tcp:"); ok {
		return "http://" + hp + "/RPC2"
	}
	return "http://localhost/RPC2"
}
