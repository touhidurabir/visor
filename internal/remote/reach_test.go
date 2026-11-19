package remote_test

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/touhidurabir/visor/internal/remote"
	"github.com/touhidurabir/visor/internal/remote/remotetest"
	"github.com/touhidurabir/visor/internal/supervisor"
	"github.com/touhidurabir/visor/internal/supervisor/supervisortest"
)

func connectOpts(t *testing.T, o remotetest.Options) (*remote.Conn, *remotetest.Server) {
	t.Helper()
	_, hk := isolate(t)
	o.Password = "pw"
	s := remotetest.Start(t, o)
	tg := target(s)
	if err := hk.Trust(tg.Host, tg.Port, s.HostKey); err != nil {
		t.Fatal(err)
	}
	c, err := remote.Dial(context.Background(), tg, remote.Secrets{Password: "pw"}, hk)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c, s
}

func TestReachForward(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Sleeper)
	c, srv := connectOpts(t, remotetest.Options{})
	a, err := remote.Reach(context.Background(), c, "unix:"+inst.Socket(), remote.BridgeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if a.Via != remote.ViaForward {
		t.Fatalf("via %s", a.Via)
	}
	if _, err := supervisor.New(a.Transport).GetAllProcessInfo(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(srv.Execs()) != 0 {
		t.Fatalf("forwarding should run nothing on the server: %v", srv.Execs())
	}
}

func TestReachBridgeWhenForwardingIsOff(t *testing.T) {
	needPython(t)
	inst := supervisortest.Start(t, supervisortest.Sleeper)
	c, _ := connectOpts(t, remotetest.Options{NoStreamLocal: true})
	a, err := remote.Reach(context.Background(), c, "unix:"+inst.Socket(), remote.BridgeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if a.Via != remote.ViaBridge {
		t.Fatalf("via %s", a.Via)
	}
	if _, err := supervisor.New(a.Transport).GetAllProcessInfo(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestReachSudo(t *testing.T) {
	needPython(t)
	inst := supervisortest.Start(t, supervisortest.Sleeper)
	env, _ := fakeSudo(t, "pw2", false)
	c, _ := connectOpts(t, remotetest.Options{Env: env})
	a, err := remote.Reach(context.Background(), c, "unix:"+inst.Socket(), remote.BridgeOptions{Sudo: true, SudoPassword: "pw2"})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if a.Via != remote.ViaSudo || a.Files != a.Transport {
		t.Fatalf("via %s", a.Via)
	}
}

func TestReachDenied(t *testing.T) {
	needPython(t)
	dir, err := os.MkdirTemp("/tmp", "visor-sock-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	if err := os.Chmod(sock, 0); err != nil {
		t.Fatal(err)
	}
	c, _ := connectOpts(t, remotetest.Options{})
	_, err = remote.Reach(context.Background(), c, "unix:"+sock, remote.BridgeOptions{})
	var denied *remote.SocketDeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("want SocketDeniedError, got %v", err)
	}
	if denied.Owner == "" || denied.Mode.Perm() != 0 {
		t.Fatalf("details: %+v", denied)
	}
}

func TestReachMissing(t *testing.T) {
	needPython(t)
	c, _ := connectOpts(t, remotetest.Options{})
	_, err := remote.Reach(context.Background(), c, "unix:/tmp/visor-no-such.sock", remote.BridgeOptions{})
	if !errors.Is(err, remote.ErrSocketMissing) {
		t.Fatalf("got %v", err)
	}
}

func TestSocketTransportTCP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		c, err := ln.Accept()
		if err == nil {
			_, _ = c.Write([]byte("hi"))
			_ = c.Close()
		}
	}()
	c, _ := connectOpts(t, remotetest.Options{})
	tr := &remote.SocketTransport{Conn: c, Addr: "tcp:" + ln.Addr().String()}
	if tr.URL() != "http://"+ln.Addr().String()+"/RPC2" {
		t.Fatal(tr.URL())
	}
	conn, err := tr.DialContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(conn)
	if string(b) != "hi" {
		t.Fatalf("%q", b)
	}
}
