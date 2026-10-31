package supervisor

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// rpcCall is a request as the fake server saw it. Params holds each param's
// inner XML, e.g. "<string>web</string>".
type rpcCall struct {
	Method string
	Params []string
	Header http.Header
	Path   string
}

func parseCall(t *testing.T, r *http.Request) rpcCall {
	t.Helper()
	var mc struct {
		Method string `xml:"methodName"`
		Params []struct {
			Value struct {
				Inner string `xml:",innerxml"`
			} `xml:"value"`
		} `xml:"params>param"`
	}
	body, _ := io.ReadAll(r.Body)
	if err := xml.Unmarshal(body, &mc); err != nil {
		t.Errorf("bad request body %q: %v", body, err)
	}
	call := rpcCall{Method: mc.Method, Header: r.Header, Path: r.URL.Path}
	for _, p := range mc.Params {
		call.Params = append(call.Params, p.Value.Inner)
	}
	return call
}

// socketPath returns a unix socket path short enough for macOS's 104-byte
// sun_path limit; t.TempDir() paths under /var/folders often are not.
func socketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "visor-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, "s.sock")
}

// fakeServer serves handler on a unix socket and returns its path.
func fakeServer(t *testing.T, handler http.HandlerFunc) string {
	t.Helper()
	path := socketPath(t)
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: handler}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return path
}

func respond(w http.ResponseWriter, value string) {
	w.Header().Set("Content-Type", "text/xml")
	fmt.Fprintf(w, `<?xml version='1.0'?><methodResponse><params><param><value>%s</value></param></params></methodResponse>`, value)
}

func respondFault(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "text/xml")
	fmt.Fprintf(w, `<?xml version='1.0'?><methodResponse><fault><value><struct>`+
		`<member><name>faultCode</name><value><int>%d</int></value></member>`+
		`<member><name>faultString</name><value><string>%s</string></value></member>`+
		`</struct></value></fault></methodResponse>`, code, msg)
}

func TestRequestShape(t *testing.T) {
	var got rpcCall
	path := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		got = parseCall(t, r)
		respond(w, `<string>4.2.5</string>`)
	})
	v, err := New(UnixTransport{Path: path}).GetSupervisorVersion(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if v != "4.2.5" {
		t.Errorf("version = %q", v)
	}
	if got.Path != "/RPC2" {
		t.Errorf("path = %q, want /RPC2", got.Path)
	}
	if ct := got.Header.Get("Content-Type"); ct != "text/xml" {
		t.Errorf("Content-Type = %q, want text/xml", ct)
	}
	if a := got.Header.Get("Authorization"); a != "" {
		t.Errorf("unexpected Authorization header %q", a)
	}
	if got.Method != "supervisor.getSupervisorVersion" || len(got.Params) != 0 {
		t.Errorf("got call %s%v", got.Method, got.Params)
	}
}

func TestBasicAuth(t *testing.T) {
	path := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "admin" || pass != "s3cret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		respond(w, `<string>4.2.5</string>`)
	})
	ctx := context.Background()
	if _, err := New(UnixTransport{Path: path}, WithBasicAuth("admin", "s3cret")).GetSupervisorVersion(ctx); err != nil {
		t.Errorf("with auth: %v", err)
	}
	_, err := New(UnixTransport{Path: path}).GetSupervisorVersion(ctx)
	if !errors.Is(err, ErrUnauthorized) {
		t.Errorf("without auth: want ErrUnauthorized, got %v", err)
	}
}

func TestHTTPErrorStatus(t *testing.T) {
	path := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	_, err := New(UnixTransport{Path: path}).GetSupervisorVersion(context.Background())
	if err == nil || errors.Is(err, ErrUnauthorized) || !strings.Contains(err.Error(), "500") {
		t.Errorf("want HTTP 500 error, got %v", err)
	}
}

func TestUnreachableMissingSocket(t *testing.T) {
	path := filepath.Join(filepath.Dir(socketPath(t)), "missing.sock")
	_, err := New(UnixTransport{Path: path}).GetState(context.Background())
	if !errors.Is(err, ErrUnreachable) {
		t.Errorf("want ErrUnreachable, got %v", err)
	}
}

// A socket file left behind by a crashed supervisord refuses connections.
func TestUnreachableStaleSocket(t *testing.T) {
	path := socketPath(t)
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	ln.Close()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("socket file should still exist: %v", err)
	}
	_, err = New(UnixTransport{Path: path}).GetState(context.Background())
	if !errors.Is(err, ErrUnreachable) {
		t.Errorf("want ErrUnreachable, got %v", err)
	}
}

// A server that accepts but never answers (e.g. supervisord stuck mid-restart)
// must not hang a quick call past its timeout.
func TestDefaultTimeout(t *testing.T) {
	path := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	start := time.Now()
	_, err := New(UnixTransport{Path: path}, WithTimeout(100*time.Millisecond)).GetState(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("want DeadlineExceeded, got %v", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("took %v, want about 100ms", d)
	}
}

// Calls that wait for a process state change are exempt from the default
// timeout (stopwaitsecs can exceed it) but still honour the caller's ctx.
func TestWaitingCallsUseCallerContext(t *testing.T) {
	path := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(300 * time.Millisecond):
			respond(w, `<boolean>1</boolean>`)
		case <-r.Context().Done():
		}
	})
	c := New(UnixTransport{Path: path}, WithTimeout(100*time.Millisecond))
	if err := c.StopProcess(context.Background(), "web", true); err != nil {
		t.Errorf("stop with wait should outlive the default timeout: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := c.StopProcess(ctx, "web", true); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("want caller's DeadlineExceeded, got %v", err)
	}
}

func TestFaultMapping(t *testing.T) {
	cases := []struct {
		code int
		want error
	}{
		{1, ErrUnknownMethod},
		{2, ErrIncorrectParameters},
		{3, ErrBadArguments},
		{6, ErrShutdownState},
		{10, ErrBadName},
		{11, ErrBadSignal},
		{20, ErrNoFile},
		{21, ErrNotExecutable},
		{30, ErrFailed},
		{40, ErrAbnormalTermination},
		{50, ErrSpawnError},
		{60, ErrAlreadyStarted},
		{70, ErrNotRunning},
		{90, ErrAlreadyAdded},
		{91, ErrStillRunning},
		{92, ErrCantReread},
	}
	for _, c := range cases {
		t.Run(fmt.Sprint(c.code), func(t *testing.T) {
			path := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
				respondFault(w, c.code, "SOME_FAULT: web")
			})
			err := New(UnixTransport{Path: path}).StartProcess(context.Background(), "web", false)
			if !errors.Is(err, c.want) {
				t.Errorf("want %v, got %v", c.want, err)
			}
			var f *Fault
			if !errors.As(err, &f) || f.String != "SOME_FAULT: web" {
				t.Errorf("want *Fault with server message, got %#v", err)
			}
			if c.want != ErrBadName && errors.Is(err, ErrBadName) {
				t.Error("must not match a different fault")
			}
		})
	}
}

func TestTCPTransport(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/RPC2" {
			t.Errorf("path = %q", r.URL.Path)
		}
		respond(w, `<string>4.3.0</string>`)
	}))
	defer srv.Close()
	addr := strings.TrimPrefix(srv.URL, "http://")
	v, err := New(TCPTransport{Addr: addr}).GetSupervisorVersion(context.Background())
	if err != nil || v != "4.3.0" {
		t.Errorf("got %q, %v", v, err)
	}
}

func TestResponseTooLarge(t *testing.T) {
	defer func(n int64) { maxResponseBytes = n }(maxResponseBytes)
	maxResponseBytes = 1024
	path := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		respond(w, "<string>"+strings.Repeat("x", 2048)+"</string>")
	})
	_, err := New(UnixTransport{Path: path}).ReadLog(context.Background(), 0, 0)
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Errorf("want too-large error, got %v", err)
	}
}

func TestUnexpectedResultType(t *testing.T) {
	path := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		respond(w, `<int>3</int>`)
	})
	_, err := New(UnixTransport{Path: path}).GetSupervisorVersion(context.Background())
	if err == nil {
		t.Error("want error for int where string expected")
	}
}

func TestCloseDropsIdleConnections(t *testing.T) {
	path := socketPath(t)
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	closed := make(chan struct{}, 4)
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { respond(w, `<string>4.2.5</string>`) }),
		ConnState: func(_ net.Conn, s http.ConnState) {
			if s == http.StateClosed {
				closed <- struct{}{}
			}
		},
	}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })

	c := New(UnixTransport{Path: path})
	if _, err := c.GetSupervisorVersion(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-closed:
		t.Fatal("the connection closed before Close: it should be kept for reuse")
	case <-time.After(50 * time.Millisecond):
	}
	c.Close()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close left the idle connection open")
	}
	c.Close() // safe to call again
}
