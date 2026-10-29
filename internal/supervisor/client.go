// Package supervisor is a typed XML-RPC client for supervisord.
package supervisor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/touhidurabir/visor/internal/supervisor/xmlrpc"
)

const defaultTimeout = 10 * time.Second

// maxResponseBytes caps a response body; large log tails are the only big ones.
var maxResponseBytes int64 = 32 << 20

// Client calls supervisord over a Transport. It is safe for concurrent use.
type Client struct {
	transport Transport
	http      *http.Client
	user      string
	pass      string
	auth      bool
	timeout   time.Duration
}

// Option configures a Client.
type Option func(*Client)

// WithBasicAuth sends HTTP basic auth credentials with every call.
func WithBasicAuth(user, pass string) Option {
	return func(c *Client) { c.user, c.pass, c.auth = user, pass, true }
}

// WithTimeout sets the deadline for calls that return promptly (default 10s).
// Calls that wait for a process to change state are bounded by the caller's
// context instead, since they can legitimately take longer (stopwaitsecs).
func WithTimeout(d time.Duration) Option {
	return func(c *Client) { c.timeout = d }
}

// New returns a Client that talks to supervisord over t.
func New(t Transport, opts ...Option) *Client {
	c := &Client{transport: t, timeout: defaultTimeout}
	for _, o := range opts {
		o(c)
	}
	c.http = &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return t.DialContext(ctx)
		},
		MaxIdleConns:    2,
		IdleConnTimeout: 30 * time.Second,
	}}
	return c
}

// call runs a quick method under the default timeout.
func (c *Client) call(ctx context.Context, method string, params ...any) (any, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	return c.do(ctx, method, params...)
}

// callWaiting runs a method that may block on a process state change; only
// the caller's context bounds it.
func (c *Client) callWaiting(ctx context.Context, method string, params ...any) (any, error) {
	return c.do(ctx, method, params...)
}

func (c *Client) do(ctx context.Context, method string, params ...any) (any, error) {
	var body bytes.Buffer
	if err := xmlrpc.EncodeRequest(&body, method, params...); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.transport.URL(), &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "text/xml")
	if c.auth {
		req.SetBasicAuth(c.user, c.pass)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("%s: %w", method, ctxErr)
		}
		var opErr *net.OpError
		if errors.As(err, &opErr) && opErr.Op == "dial" {
			return nil, fmt.Errorf("%w: %w", ErrUnreachable, err)
		}
		return nil, fmt.Errorf("%s: %w", method, err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, ErrUnauthorized
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("%s: HTTP %s", method, resp.Status)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("%s: %w", method, ctxErr)
		}
		return nil, fmt.Errorf("%s: %w", method, err)
	}
	if int64(len(raw)) > maxResponseBytes {
		return nil, fmt.Errorf("%s: response too large (over %d bytes)", method, maxResponseBytes)
	}
	v, err := xmlrpc.DecodeResponse(bytes.NewReader(raw))
	if err != nil {
		var f *Fault
		if errors.As(err, &f) {
			return nil, err
		}
		return nil, fmt.Errorf("%s: %w", method, err)
	}
	return v, nil
}
