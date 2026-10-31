package monitor

import (
	"testing"

	"github.com/touhidurabir/visor/internal/supervisor"
)

func exited(name string, status int) supervisor.ProcessInfo {
	p := proc(name, supervisor.StateExited)
	p.ExitStatus = status
	return p
}

func TestHealthOfRules(t *testing.T) {
	running := proc("ok", supervisor.StateRunning)
	tests := []struct {
		name string
		s    Snapshot
		want Health
	}{
		{"unknown before the first poll", Snapshot{}, HealthUnknown},
		{"ok when everything runs", up(0, running, proc("idle", supervisor.StateStopped)), HealthOK},
		{"ok with no processes", up(0), HealthOK},
		{"ok when exited with 0", up(0, running, exited("job", 0)), HealthOK},
		{"warning when starting", up(0, running, proc("s", supervisor.StateStarting)), HealthWarning},
		{"warning when in backoff", up(0, running, proc("s", supervisor.StateBackoff)), HealthWarning},
		{"warning when stopping", up(0, running, proc("s", supervisor.StateStopping)), HealthWarning},
		{"error when fatal", up(0, running, proc("f", supervisor.StateFatal)), HealthError},
		{"error when exited unexpectedly", up(0, running, exited("job", 3)), HealthError},
		{"error when unreachable", down(0), HealthError},
		{"error beats warning", up(0, proc("s", supervisor.StateBackoff), proc("f", supervisor.StateFatal)), HealthError},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := HealthOf(tc.s, nil); got != tc.want {
				t.Errorf("HealthOf = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestHealthOfExpectedExitCodes(t *testing.T) {
	s := up(0, exited("job", 2))
	codes := func(fullName string) []int {
		if fullName == "job:job" {
			return []int{0, 2} // exitcodes=0,2
		}
		return nil
	}
	if got := HealthOf(s, codes); got != HealthOK {
		t.Errorf("EXITED 2 with exitcodes=0,2: HealthOf = %v, want OK", got)
	}
	if got := HealthOf(up(0, exited("other", 2)), codes); got != HealthError {
		t.Errorf("EXITED 2 with default exitcodes: HealthOf = %v, want Error", got)
	}
	if got := HealthOf(up(0, exited("job", 0)), func(string) []int { return []int{2} }); got != HealthError {
		t.Errorf("EXITED 0 with exitcodes=2: HealthOf = %v, want Error", got)
	}
}
