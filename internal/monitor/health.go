package monitor

import (
	"slices"

	"github.com/touhidurabir/visor/internal/supervisor"
)

// Health is the aggregate state shown by the tray icon.
type Health int

const (
	HealthOK Health = iota
	HealthWarning
	HealthError
	HealthUnknown
)

func (h Health) String() string {
	switch h {
	case HealthOK:
		return "ok"
	case HealthWarning:
		return "warning"
	case HealthError:
		return "error"
	case HealthUnknown:
		return "unknown"
	}
	return "Health(?)"
}

// defaultExitCodes is supervisord's default for exitcodes=.
var defaultExitCodes = []int{0}

// HealthOf summarises a snapshot:
//
//   - Unknown before the first poll (s.At is zero).
//   - Error if supervisord is unreachable, any process is FATAL, or any
//     process is EXITED with a status not in expectedExit(fullName).
//   - Warning if any process is STARTING, BACKOFF or STOPPING.
//   - OK otherwise.
//
// expectedExit returns a program's exitcodes=; a nil func or an empty
// result means supervisord's default, [0].
func HealthOf(s Snapshot, expectedExit func(fullName string) []int) Health {
	if s.At.IsZero() {
		return HealthUnknown
	}
	if !s.Reachable {
		return HealthError
	}
	h := HealthOK
	for _, p := range s.Processes {
		switch p.State {
		case supervisor.StateFatal:
			return HealthError
		case supervisor.StateExited:
			codes := defaultExitCodes
			if expectedExit != nil {
				if c := expectedExit(p.FullName()); len(c) > 0 {
					codes = c
				}
			}
			if !slices.Contains(codes, p.ExitStatus) {
				return HealthError
			}
		case supervisor.StateStarting, supervisor.StateBackoff, supervisor.StateStopping:
			h = HealthWarning
		}
	}
	return h
}
