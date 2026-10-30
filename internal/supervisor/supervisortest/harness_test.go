package supervisortest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/touhidurabir/visor/internal/supervisor"
)

func TestStartRunsPrograms(t *testing.T) {
	t.Parallel()
	i := Start(t, Sleeper, Failer, Spammer)

	if !strings.HasPrefix(i.Dir(), "/tmp/") {
		t.Errorf("dir %q should be under /tmp to keep the socket path short", i.Dir())
	}
	if len(i.sock) >= maxSockPath {
		t.Errorf("socket path %q is %d bytes, macOS allows < %d", i.sock, len(i.sock), maxSockPath)
	}
	st, err := i.Client().GetState(context.Background())
	if err != nil || st.Name != "RUNNING" {
		t.Fatalf("GetState = %+v, %v", st, err)
	}
	i.WaitFor(t, "sleeper", supervisor.StateRunning)
	i.WaitFor(t, "spammer", supervisor.StateRunning)
	i.WaitFor(t, "failer", supervisor.StateExited)
}

func TestWriteProgramRendersIni(t *testing.T) {
	t.Parallel()
	i := Start(t)
	i.WriteProgram(t, Program{Name: "extra", Command: "sleep 1", Extra: map[string]string{"priority": "5", "autostart": "false"}})
	got, err := os.ReadFile(filepath.Join(i.Dir(), "conf.d", "extra.ini"))
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf(`[program:extra]
command=sleep 1
autostart=false
priority=5
stderr_logfile=%[1]s/logs/extra.err.log
stdout_logfile=%[1]s/logs/extra.out.log
`, i.Dir())
	if string(got) != want {
		t.Errorf("ini =\n%s\nwant\n%s", got, want)
	}
}

// pidsGone waits for every pid to disappear.
func pidsGone(t *testing.T, pids ...int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for _, pid := range pids {
		for syscall.Kill(pid, 0) == nil {
			if time.Now().After(deadline) {
				t.Errorf("pid %d still alive after cleanup", pid)
				_ = syscall.Kill(pid, syscall.SIGKILL)
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
}

// A test that panics must not leave supervisord or its children running.
// The panic aborts the test binary, so it runs in a child process.
func TestCleanupAfterPanic(t *testing.T) {
	if pidFile := os.Getenv("VISOR_HARNESS_PANIC_PIDFILE"); pidFile != "" {
		i := Start(t, Sleeper)
		p := i.WaitFor(t, "sleeper", supervisor.StateRunning)
		pids := fmt.Sprintf("%d %d", i.cmd.Process.Pid, p.PID)
		if err := os.WriteFile(pidFile, []byte(pids), 0o600); err != nil {
			t.Fatal(err)
		}
		panic("deliberate panic to exercise harness cleanup")
	}
	if testing.Short() {
		t.Skip("integration test: skipped with -short")
	}
	if _, err := exec.LookPath("supervisord"); err != nil {
		t.Skip("supervisord not installed")
	}
	t.Parallel()
	pidFile := filepath.Join(t.TempDir(), "pids")
	cmd := exec.Command(os.Args[0], "-test.run=^TestCleanupAfterPanic$", "-test.count=1")
	cmd.Env = append(os.Environ(), "VISOR_HARNESS_PANIC_PIDFILE="+pidFile)
	out, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || !strings.Contains(string(out), "deliberate panic") {
		t.Fatalf("child should panic, got err=%v\n%s", err, out)
	}
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("child wrote no pids: %v\n%s", err, out)
	}
	var pids []int
	for _, f := range strings.Fields(string(raw)) {
		pid, _ := strconv.Atoi(f)
		pids = append(pids, pid)
	}
	if len(pids) != 2 {
		t.Fatalf("pids = %q", raw)
	}
	pidsGone(t, pids...)
}

// When graceful shutdown stalls (a program ignoring SIGTERM with a long
// stopwaitsecs), cleanup kills supervisord and the program's process group.
func TestCleanupKillsStuckShutdown(t *testing.T) {
	t.Parallel()
	stubborn := Program{
		Name:    "stubborn",
		Command: `sh -c 'trap "" TERM; sleep 3600'`,
		Extra:   map[string]string{"stopwaitsecs": "60"},
	}
	var pids []int
	t.Run("run", func(t *testing.T) {
		i := Start(t, stubborn)
		i.stopTimeout = time.Second
		p := i.WaitFor(t, "stubborn", supervisor.StateRunning)
		pids = []int{i.cmd.Process.Pid, p.PID}
	})
	if len(pids) == 0 {
		t.Skip("subtest skipped")
	}
	pidsGone(t, pids...)
}
