package supervisor

import (
	"context"
	"net"
)

// Transport opens connections to supervisord's HTTP server. Unix sockets and
// TCP are implemented here; an SSH tunnel can implement the same two methods
// (DialContext via ssh.Client.Dial("unix", remotePath)).
type Transport interface {
	DialContext(ctx context.Context) (net.Conn, error)
	// URL is the XML-RPC endpoint the HTTP request is addressed to.
	URL() string
}

// UnixTransport connects to a [unix_http_server] socket.
type UnixTransport struct {
	Path string
}

func (t UnixTransport) DialContext(ctx context.Context) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "unix", t.Path)
}

// URL uses a placeholder host; the connection itself goes to the socket.
func (t UnixTransport) URL() string { return "http://localhost/RPC2" }

// TCPTransport connects to an [inet_http_server] at Addr (host:port).
type TCPTransport struct {
	Addr string
}

func (t TCPTransport) DialContext(ctx context.Context) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "tcp", t.Addr)
}

func (t TCPTransport) URL() string { return "http://" + t.Addr + "/RPC2" }
