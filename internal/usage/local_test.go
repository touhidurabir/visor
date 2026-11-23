//go:build linux || darwin

package usage

import (
	"context"
	"os/exec"
	"runtime"
	"testing"
	"time"
)

func start(t *testing.T, script string) int {
	t.Helper()
	cmd := exec.Command("sh", "-c", script)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	return cmd.Process.Pid
}

func TestLocalCountsChildren(t *testing.T) {
	pid := start(t, "sleep 30 & sleep 30 & wait")
	var got Totals
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		m, err := Local().Sample(context.Background(), []int{pid, 1 << 30})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := m[1<<30]; ok {
			t.Fatal("a pid that doesn't exist was measured")
		}
		if got = m[pid]; got.Procs >= 3 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if got.Procs < 3 || got.RSS == 0 {
		t.Fatalf("sh with two sleeps: %+v", got)
	}
}

func TestLocalCPU(t *testing.T) {
	pid := start(t, "while :; do :; done")
	h := NewHistory(10)
	for i := range 2 {
		m, err := Local().Sample(context.Background(), []int{pid})
		if err != nil {
			t.Fatal(err)
		}
		tot, ok := m[pid]
		h.Add(time.Now(), "busy", pid, tot, ok)
		if i == 0 {
			time.Sleep(time.Second)
		}
	}
	if c := h.Current("busy").CPU; c < 30 || c > 150 {
		t.Fatalf("a busy loop uses %.1f %% CPU", c)
	}
}

// BenchmarkLocal is one sample of 50 trees over this machine's whole process
// table: Visor's cost is this every 10 s.
func BenchmarkLocal(b *testing.B) {
	pids := make([]int, 50)
	for i := range pids {
		pids[i] = 1 + i
	}
	s := Local()
	for b.Loop() {
		if _, err := s.Sample(context.Background(), pids); err != nil {
			b.Fatal(err)
		}
	}
}

func TestLocalMachine(t *testing.T) {
	m, err := LocalMachine()
	if err != nil {
		t.Fatal(err)
	}
	if m.Cores != runtime.NumCPU() || m.MemBytes < 256<<20 {
		t.Fatalf("machine %+v, want %d cores and some memory", m, runtime.NumCPU())
	}
}
