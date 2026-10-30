// Package supervisortest runs a real supervisord in a temp dir for
// integration tests. Each instance has its own config, unix socket, include
// dir and logs under /tmp, and never touches the system supervisord.
package supervisortest

import (
	"context"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/touhidurabir/visor/internal/supervisor"
)

// maxSockPath is macOS's sun_path limit (104 bytes including the NUL).
const maxSockPath = 104

// Program is one [program:x] section. Extra holds further ini keys and
// overrides the defaults (autostart=true, per-program log files).
type Program struct {
	Name    string
	Command string
	Extra   map[string]string
}

var (
	// Sleeper runs quietly until stopped.
	Sleeper = Program{Name: "sleeper", Command: "sleep 3600"}
	// Failer exits with status 3 straight away and is not restarted, so it
	// settles in EXITED.
	Failer = Program{
		Name:    "failer",
		Command: "sh -c 'exit 3'",
		Extra:   map[string]string{"startsecs": "0", "startretries": "0", "autorestart": "false"},
	}
	// Spammer writes about 50 numbered lines a second to stdout.
	Spammer = Program{
		Name:    "spammer",
		Command: `sh -c 'i=0; while :; do i=$((i+1)); echo "line $i"; sleep 0.02; done'`,
	}
)

// Instance is a running supervisord. It is shut down when the test ends.
type Instance struct {
	dir         string
	sock        string
	cmd         *exec.Cmd
	exited      chan struct{}
	client      *supervisor.Client
	stopTimeout time.Duration
}

// Start launches supervisord with progs and waits until it accepts calls.
// It skips the test under -short or when supervisord is not installed.
func Start(t testing.TB, progs ...Program) *Instance {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test: skipped with -short")
	}
	bin, err := exec.LookPath("supervisord")
	if err != nil {
		t.Skip("supervisord not installed")
	}
	// t.TempDir() paths under /var/folders are too long for a unix socket.
	dir, err := os.MkdirTemp("/tmp", "visor-")
	if err != nil {
		t.Fatal(err)
	}
	i := &Instance{
		dir:         dir,
		sock:        filepath.Join(dir, "s.sock"),
		exited:      make(chan struct{}),
		stopTimeout: 5 * time.Second,
	}
	if len(i.sock) >= maxSockPath {
		t.Fatalf("socket path %q is too long for macOS", i.sock)
	}
	for _, sub := range []string{"conf.d", "logs"} {
		if err := os.Mkdir(filepath.Join(dir, sub), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	conf := filepath.Join(dir, "supervisord.conf")
	if err := os.WriteFile(conf, []byte(i.mainConfig()), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, p := range progs {
		i.WriteProgram(t, p)
	}

	out, err := os.Create(filepath.Join(dir, "supervisord.out"))
	if err != nil {
		t.Fatal(err)
	}
	// -n keeps supervisord in the foreground, so cmd's pid is the daemon.
	i.cmd = exec.Command(bin, "-n", "-c", conf)
	i.cmd.Stdout, i.cmd.Stderr = out, out
	if err := i.cmd.Start(); err != nil {
		_ = out.Close()
		t.Fatal(err)
	}
	go func() {
		_ = i.cmd.Wait()
		_ = out.Close()
		close(i.exited)
	}()
	i.client = supervisor.New(supervisor.UnixTransport{Path: i.sock}, supervisor.WithTimeout(5*time.Second))
	t.Cleanup(func() {
		i.stop(t)
		if t.Failed() {
			t.Logf("supervisortest: keeping %s for inspection", dir)
			return
		}
		_ = os.RemoveAll(dir)
	})
	i.waitReady(t)
	return i
}

// Client returns a client connected to this instance.
func (i *Instance) Client() *supervisor.Client { return i.client }

// Dir is the instance's root: supervisord.conf, conf.d/ (the include dir),
// logs/ and supervisord.out (the daemon's own output).
func (i *Instance) Dir() string { return i.dir }

// WriteProgram writes conf.d/<name>.ini. supervisord sees it after
// ReloadConfig, or at startup when written before Start returns.
func (i *Instance) WriteProgram(t testing.TB, p Program) {
	t.Helper()
	keys := map[string]string{
		"autostart":      "true",
		"stdout_logfile": filepath.Join(i.dir, "logs", p.Name+".out.log"),
		"stderr_logfile": filepath.Join(i.dir, "logs", p.Name+".err.log"),
	}
	maps.Copy(keys, p.Extra)
	var b strings.Builder
	fmt.Fprintf(&b, "[program:%s]\ncommand=%s\n", p.Name, p.Command)
	for _, k := range slices.Sorted(maps.Keys(keys)) {
		fmt.Fprintf(&b, "%s=%s\n", k, keys[k])
	}
	path := filepath.Join(i.dir, "conf.d", p.Name+".ini")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

// WaitFor polls until the named process is in one of states and returns
// its info, failing the test after 15s.
func (i *Instance) WaitFor(t testing.TB, name string, states ...supervisor.ProcessState) supervisor.ProcessInfo {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		p, err := i.client.GetProcessInfo(context.Background(), name)
		if err == nil && slices.Contains(states, p.State) {
			return p
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s never reached %v: last state %v, err %v", name, states, p.State, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (i *Instance) mainConfig() string {
	return fmt.Sprintf(`[unix_http_server]
file=%[1]s
chmod=0700

[supervisord]
logfile=%[2]s/supervisord.log
pidfile=%[2]s/supervisord.pid
childlogdir=%[2]s/logs
nodaemon=true

[rpcinterface:supervisor]
supervisor.rpcinterface_factory = supervisor.rpcinterface:make_main_rpcinterface

[include]
files = %[2]s/conf.d/*.ini
`, i.sock, i.dir)
}

func (i *Instance) waitReady(t testing.TB) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		select {
		case <-i.exited:
			out, _ := os.ReadFile(filepath.Join(i.dir, "supervisord.out"))
			t.Fatalf("supervisord exited during startup:\n%s", out)
		default:
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		st, err := i.client.GetState(ctx)
		cancel()
		if err == nil && st.Name == "RUNNING" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("supervisord not ready after 15s: state %+v, err %v", st, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// stop shuts supervisord down gracefully. If that takes longer than
// stopTimeout, it kills supervisord and each program's process group
// (supervisord makes every child a process group leader), so no
// `sleep 3600` outlives the test.
func (i *Instance) stop(t testing.TB) {
	select {
	case <-i.exited:
		return
	default:
	}
	ctx, cancel := context.WithTimeout(context.Background(), i.stopTimeout)
	defer cancel()
	var pids []int
	if procs, err := i.client.GetAllProcessInfo(ctx); err == nil {
		for _, p := range procs {
			if p.PID > 0 {
				pids = append(pids, p.PID)
			}
		}
	}
	_ = i.client.Shutdown(ctx)
	select {
	case <-i.exited:
		return
	case <-ctx.Done():
	}
	t.Logf("supervisortest: supervisord did not stop within %v; killing it and %d program(s)", i.stopTimeout, len(pids))
	_ = i.cmd.Process.Kill()
	for _, pid := range pids {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
	}
	<-i.exited
}
