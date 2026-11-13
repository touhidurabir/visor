package remote

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"syscall"

	"github.com/touhidurabir/visor/internal/supervisor"
)

// How Visor reaches supervisord on a server.
const (
	ViaForward = "forward" // the SSH user opens the socket; nothing runs on the server
	ViaBridge  = "bridge"  // the bridge opens it as the SSH user (forwarding is off)
	ViaSudo    = "sudo"    // the bridge runs with sudo
)

// SocketDeniedError means the SSH user may not open supervisord's socket.
type SocketDeniedError struct {
	Addr  string
	Owner string
	Group string
	Mode  fs.FileMode
}

func (e *SocketDeniedError) Error() string {
	return fmt.Sprintf("%s can't be opened by this user (owner %s, group %s, mode %04o)", e.Addr, e.Owner, e.Group, e.Mode.Perm())
}

// ErrSocketMissing means there is no socket at the path (supervisord is not
// running, or the config names another path).
var ErrSocketMissing = errors.New("supervisord's socket isn't there")

// ErrNotListening means the socket exists but nothing answers on it.
var ErrNotListening = errors.New("supervisord isn't listening on its socket")

// Access is a way to reach supervisord on a server, and the bridge used to
// read its files (sudo when the server uses it).
type Access struct {
	Transport supervisor.Transport
	Via       string
	Files     *BridgeTransport
}

// Close stops the bridge, if one is running.
func (a *Access) Close() {
	if a.Files != nil {
		a.Files.Close()
	}
}

// Reach picks how to talk to supervisord at addr ("unix:<path>" or
// "tcp:<host>:<port>"). With sudo, everything goes through the sudo bridge.
// Without, the socket is forwarded when the SSH user may open it; when SSH
// forwarding is switched off on the server, the bridge relays it instead.
func Reach(ctx context.Context, c *Conn, addr string, o BridgeOptions) (*Access, error) {
	files := &BridgeTransport{Conn: c, Options: o, Addr: addr}
	if o.Sudo {
		conn, err := files.DialContext(ctx)
		if err != nil {
			files.Close()
			return nil, socketError(ctx, files, addr, err)
		}
		_ = conn.Close()
		return &Access{Transport: files, Via: ViaSudo, Files: files}, nil
	}
	fwd := &SocketTransport{Conn: c, Addr: addr}
	if conn, err := fwd.DialContext(ctx); err == nil {
		_ = conn.Close()
		return &Access{Transport: fwd, Via: ViaForward, Files: files}, nil
	} else if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	// Forwarding failed: ask the bridge why (and use it if it can connect).
	conn, err := files.DialContext(ctx)
	if err != nil {
		files.Close()
		return nil, socketError(ctx, files, addr, err)
	}
	_ = conn.Close()
	return &Access{Transport: files, Via: ViaBridge, Files: files}, nil
}

func socketError(ctx context.Context, files *BridgeTransport, addr string, err error) error {
	var re *RemoteError
	if !errors.As(err, &re) {
		return err
	}
	switch syscall.Errno(re.Errno) {
	case syscall.ENOENT:
		return fmt.Errorf("%w (%s)", ErrSocketMissing, addr)
	case syscall.ECONNREFUSED:
		return fmt.Errorf("%w (%s)", ErrNotListening, addr)
	case syscall.EACCES, syscall.EPERM:
		d := &SocketDeniedError{Addr: addr}
		if b, berr := files.Bridge(ctx); berr == nil {
			if path, ok := unixPath(addr); ok {
				if st, serr := b.Stat(ctx, path); serr == nil {
					d.Owner, d.Group, d.Mode = st.Owner, st.Group, st.Mode
				}
			}
		}
		return d
	}
	return err
}

func unixPath(addr string) (string, bool) {
	if len(addr) > 5 && addr[:5] == "unix:" {
		return addr[5:], true
	}
	return "", false
}
