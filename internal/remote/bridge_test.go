package remote_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/touhidurabir/visor/internal/remote"
	"github.com/touhidurabir/visor/internal/remote/remotetest"
	"github.com/touhidurabir/visor/internal/supervisor"
	"github.com/touhidurabir/visor/internal/supervisor/supervisortest"
	"github.com/touhidurabir/visor/internal/usage"
)

// fakeSudo writes a sudo stand-in that checks the password from stdin (like
// sudo -S) and logs each run. It returns the PATH and env to give the server.
func fakeSudo(t *testing.T, password string, noPassword bool) (env []string, log string) {
	t.Helper()
	dir := t.TempDir()
	log = filepath.Join(dir, "sudo.log")
	script := `#!/bin/sh
echo run >> "$FAKE_SUDO_LOG"
prompt=""
while :; do
  case "$1" in
    -S) shift ;;
    -p) prompt="$2"; shift 2 ;;
    *) break ;;
  esac
done
if [ -z "$FAKE_SUDO_NOPASSWD" ]; then
  printf '%s' "$prompt" >&2
  IFS= read -r pw || exit 1
  if [ "$pw" != "$FAKE_SUDO_PASSWORD" ]; then
    echo "Sorry, try again." >&2
    printf '%s' "$prompt" >&2
    IFS= read -r pw
    echo "sudo: 2 incorrect password attempts" >&2
    exit 1
  fi
fi
exec "$@"
`
	if err := os.WriteFile(filepath.Join(dir, "sudo"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	env = []string{
		"PATH=" + dir + ":" + os.Getenv("PATH"),
		"FAKE_SUDO_LOG=" + log,
		"FAKE_SUDO_PASSWORD=" + password,
	}
	if noPassword {
		env = append(env, "FAKE_SUDO_NOPASSWD=1")
	}
	return env, log
}

func sudoRuns(t *testing.T, log string) int {
	t.Helper()
	b, err := os.ReadFile(log)
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(b), "run\n")
}

func needPython(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not installed")
	}
}

// connect starts a test SSH server with env and returns a trusted connection.
func connect(t *testing.T, env []string) (*remote.Conn, *remotetest.Server) {
	t.Helper()
	_, hk := isolate(t)
	s := remotetest.Start(t, remotetest.Options{Password: "pw", Env: env})
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

func TestBridgeClient(t *testing.T) {
	needPython(t)
	inst := supervisortest.Start(t, supervisortest.Sleeper, supervisortest.Spammer)
	c, _ := connect(t, nil)
	tr := &remote.BridgeTransport{Conn: c, Addr: "unix:" + inst.Socket()}
	defer tr.Close()
	cl := supervisor.New(tr)
	ctx := context.Background()

	procs, err := cl.GetAllProcessInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(procs) != 2 {
		t.Fatalf("got %d processes", len(procs))
	}
	inst.WaitFor(t, "spammer", supervisor.StateRunning)
	time.Sleep(200 * time.Millisecond)
	tail, err := cl.TailProcessStdoutLog(ctx, "spammer", 0, 4096)
	if err != nil || !strings.Contains(tail.Bytes, "line ") {
		t.Fatalf("tail: %q %v", tail.Bytes, err)
	}
	b, err := tr.Bridge(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if b.Hello().User == "" || b.Hello().Python == "" {
		t.Fatalf("hello: %+v", b.Hello())
	}

	// Concurrent calls share the one bridge.
	errs := make(chan error, 8)
	for range 8 {
		go func() {
			_, err := cl.GetAllProcessInfo(ctx)
			errs <- err
		}()
	}
	for range 8 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
}

func TestBridgeSudoOncePerConnection(t *testing.T) {
	needPython(t)
	inst := supervisortest.Start(t, supervisortest.Sleeper)
	env, log := fakeSudo(t, "sudopw", false)
	c, srv := connect(t, env)
	tr := &remote.BridgeTransport{
		Conn:    c,
		Addr:    "unix:" + inst.Socket(),
		Options: remote.BridgeOptions{Sudo: true, SudoPassword: "sudopw"},
	}
	defer tr.Close()
	cl := supervisor.New(tr)
	for i := range 100 {
		if _, err := cl.GetAllProcessInfo(context.Background()); err != nil {
			t.Fatalf("poll %d: %v", i, err)
		}
	}
	if n := sudoRuns(t, log); n != 1 {
		t.Fatalf("sudo ran %d times for 100 polls, want 1", n)
	}
	sudoExecs := 0
	for _, e := range srv.Execs() {
		if strings.HasPrefix(e, "sudo ") {
			sudoExecs++
		}
	}
	if sudoExecs != 1 {
		t.Fatalf("%d sudo execs", sudoExecs)
	}
}

func TestBridgeSudoNoPasswordNeeded(t *testing.T) {
	needPython(t)
	inst := supervisortest.Start(t, supervisortest.Sleeper)
	env, _ := fakeSudo(t, "", true)
	c, _ := connect(t, env)
	tr := &remote.BridgeTransport{Conn: c, Addr: "unix:" + inst.Socket(), Options: remote.BridgeOptions{Sudo: true}}
	defer tr.Close()
	if _, err := supervisor.New(tr).GetAllProcessInfo(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestBridgeSudoRefused(t *testing.T) {
	needPython(t)
	env, log := fakeSudo(t, "right", false)
	c, _ := connect(t, env)
	tr := &remote.BridgeTransport{Conn: c, Addr: "unix:/nonexistent", Options: remote.BridgeOptions{Sudo: true, SudoPassword: "wrong"}}
	ctx := context.Background()
	if _, err := tr.Bridge(ctx); !errors.Is(err, remote.ErrSudoRefused) {
		t.Fatalf("want ErrSudoRefused, got %v", err)
	}
	// Kept: no second try against the server.
	if _, err := tr.Bridge(ctx); !errors.Is(err, remote.ErrSudoRefused) {
		t.Fatalf("second call: %v", err)
	}
	if n := sudoRuns(t, log); n != 1 {
		t.Fatalf("sudo ran %d times, want 1", n)
	}

	tr2 := &remote.BridgeTransport{Conn: c, Addr: "unix:/nonexistent", Options: remote.BridgeOptions{Sudo: true}}
	if _, err := tr2.Bridge(ctx); !errors.Is(err, remote.ErrSudoPasswordNeeded) {
		t.Fatalf("no password: want ErrSudoPasswordNeeded, got %v", err)
	}
}

func TestBridgeRestartsAfterDying(t *testing.T) {
	needPython(t)
	inst := supervisortest.Start(t, supervisortest.Sleeper)
	c, _ := connect(t, nil)
	tr := &remote.BridgeTransport{Conn: c, Addr: "unix:" + inst.Socket()}
	defer tr.Close()
	cl := supervisor.New(tr)
	ctx := context.Background()
	if _, err := cl.GetAllProcessInfo(ctx); err != nil {
		t.Fatal(err)
	}
	b, _ := tr.Bridge(ctx)
	b.Close()
	<-b.Done()
	cl.Close() // drop the kept-alive connection that went through the old bridge
	if _, err := cl.GetAllProcessInfo(ctx); err != nil {
		t.Fatalf("after restart: %v", err)
	}
	b2, _ := tr.Bridge(ctx)
	if b2 == b {
		t.Fatal("bridge was not restarted")
	}
}

func TestBridgeDiesMidRequest(t *testing.T) {
	needPython(t)
	inst := supervisortest.Start(t, supervisortest.Sleeper)
	c, _ := connect(t, nil)
	tr := &remote.BridgeTransport{Conn: c, Addr: "unix:" + inst.Socket()}
	defer tr.Close()
	ctx := context.Background()
	b, err := tr.Bridge(ctx)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := b.Dial(ctx, "unix:"+inst.Socket())
	if err != nil {
		t.Fatal(err)
	}
	b.Close()
	buf := make([]byte, 10)
	if _, err := conn.Read(buf); !errors.Is(err, remote.ErrBridgeClosed) {
		t.Fatalf("read after the bridge died: %v", err)
	}
	if _, err := supervisor.New(tr).GetAllProcessInfo(ctx); err != nil {
		t.Fatalf("next call should recover: %v", err)
	}
}

func TestBridgeNoPython(t *testing.T) {
	c, _ := connect(t, []string{"PATH=/nonexistent"})
	_, err := remote.StartBridge(context.Background(), c, remote.BridgeOptions{})
	if !errors.Is(err, remote.ErrNoPython) {
		t.Fatalf("got %v", err)
	}
}

// procStat writes a fake /proc/<pid>/stat: parent ppid, ticks of CPU split
// over utime and cstime, rss in pages.
func procStat(t *testing.T, root string, pid, ppid, ticks, rss int) {
	t.Helper()
	f := []string{strconv.Itoa(pid), "(php artisan (w))", "S", strconv.Itoa(ppid)}
	for i := 5; i <= 52; i++ {
		v := 0
		switch i {
		case 14:
			v = ticks - ticks/2
		case 17:
			v = ticks / 2
		case 24:
			v = rss
		}
		f = append(f, strconv.Itoa(v))
	}
	d := filepath.Join(root, strconv.Itoa(pid))
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "stat"), []byte(strings.Join(f, " ")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBridgeUsage(t *testing.T) {
	needPython(t)
	root := t.TempDir()
	procStat(t, root, 10, 1, 300, 10)
	procStat(t, root, 11, 10, 50, 20)
	procStat(t, root, 12, 11, 50, 30)
	procStat(t, root, 20, 1, 9999, 9999)
	c, _ := connect(t, []string{"VISOR_BRIDGE_PROC=" + root})
	tr := &remote.BridgeTransport{Conn: c}
	defer tr.Close()
	ctx := context.Background()
	b, err := tr.Bridge(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got, err := b.Usage(ctx, []int{10, 77})
	if err != nil {
		t.Fatal(err)
	}
	page := uint64(os.Getpagesize())
	want := usage.Totals{CPU: 4 * time.Second, RSS: 60 * page, Procs: 3}
	if len(got) != 1 || got[10] != want {
		t.Fatalf("usage %+v, want 10: %+v", got, want)
	}

	// No /proc (macOS, or hidden): nothing measured, no error.
	c2, _ := connect(t, []string{"VISOR_BRIDGE_PROC=" + filepath.Join(root, "none")})
	tr2 := &remote.BridgeTransport{Conn: c2}
	defer tr2.Close()
	b2, err := tr2.Bridge(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := b2.Usage(ctx, []int{10}); err != nil || len(got) != 0 {
		t.Fatalf("no /proc: %+v %v", got, err)
	}
}

// The machine's size: cores from Python, total memory from /proc/meminfo
// (0 without it, as on macOS).
func TestBridgeMachine(t *testing.T) {
	needPython(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "meminfo"), []byte("MemTotal:        4028100 kB\nMemFree: 1 kB\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, tc := range []struct {
		root string
		mem  uint64
	}{{root, 4028100 * 1024}, {filepath.Join(root, "none"), 0}} {
		c, _ := connect(t, []string{"VISOR_BRIDGE_PROC=" + tc.root})
		tr := &remote.BridgeTransport{Conn: c}
		b, err := tr.Bridge(ctx)
		if err != nil {
			t.Fatal(err)
		}
		m, err := b.Machine(ctx)
		tr.Close()
		if err != nil || m.Cores < 1 || m.MemBytes != tc.mem {
			t.Fatalf("%s: machine %+v %v, want cores and %d bytes", tc.root, m, err, tc.mem)
		}
	}
}
