package supervisor

import (
	"fmt"
	"time"
)

// ProcessState is a process state code from supervisord.
type ProcessState int

const (
	StateStopped  ProcessState = 0
	StateStarting ProcessState = 10
	StateRunning  ProcessState = 20
	StateBackoff  ProcessState = 30
	StateStopping ProcessState = 40
	StateExited   ProcessState = 100
	StateFatal    ProcessState = 200
	StateUnknown  ProcessState = 1000
)

func (s ProcessState) String() string {
	switch s {
	case StateStopped:
		return "STOPPED"
	case StateStarting:
		return "STARTING"
	case StateRunning:
		return "RUNNING"
	case StateBackoff:
		return "BACKOFF"
	case StateStopping:
		return "STOPPING"
	case StateExited:
		return "EXITED"
	case StateFatal:
		return "FATAL"
	}
	return "UNKNOWN"
}

// SupervisorState is supervisord's own state: FATAL 2, RUNNING 1,
// RESTARTING 0, SHUTDOWN -1.
type SupervisorState struct {
	Code int
	Name string
}

// ProcessInfo is one entry of getProcessInfo / getAllProcessInfo.
type ProcessInfo struct {
	Name        string
	Group       string
	Description string
	State       ProcessState
	StateName   string
	SpawnErr    string
	ExitStatus  int
	PID         int
	// Start, Stop and Now are zero when supervisord reports 0.
	Start, Stop, Now time.Time

	Logfile       string
	StdoutLogfile string
	StderrLogfile string
}

// FullName is the "group:name" form RPC methods accept. It is required for
// processes in a group with numprocs > 1, and always valid.
func (p ProcessInfo) FullName() string { return p.Group + ":" + p.Name }

// TailResult is a chunk of a process log. Offset is where the next tail
// should start; Overflow means bytes before the chunk were skipped.
type TailResult struct {
	Bytes    string
	Offset   int
	Overflow bool
}

// ProcessResult is the per-process outcome of a group or "all" operation.
type ProcessResult struct {
	Name        string
	Group       string
	Status      int
	Description string
}

// ConfigDiff is the result of reloadConfig: program groups whose config was
// added, changed or removed since supervisord last loaded it.
type ConfigDiff struct {
	Added   []string
	Changed []string
	Removed []string
}

// fields reads typed members from a decoded struct, keeping the first error.
type fields struct {
	m   map[string]any
	err error
}

func newFields(v any) (*fields, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected struct, got %T", v)
	}
	return &fields{m: m}, nil
}

func (f *fields) str(key string) string {
	s, ok := f.m[key].(string)
	if !ok && f.err == nil {
		f.err = fmt.Errorf("field %q: expected string, got %T", key, f.m[key])
	}
	return s
}

func (f *fields) num(key string) int {
	n, ok := f.m[key].(int)
	if !ok && f.err == nil {
		f.err = fmt.Errorf("field %q: expected int, got %T", key, f.m[key])
	}
	return n
}

func (f *fields) time(key string) time.Time {
	if n := f.num(key); n > 0 {
		return time.Unix(int64(n), 0)
	}
	return time.Time{}
}

func toProcessInfo(v any) (ProcessInfo, error) {
	f, err := newFields(v)
	if err != nil {
		return ProcessInfo{}, err
	}
	p := ProcessInfo{
		Name:          f.str("name"),
		Group:         f.str("group"),
		Description:   f.str("description"),
		State:         ProcessState(f.num("state")),
		StateName:     f.str("statename"),
		SpawnErr:      f.str("spawnerr"),
		ExitStatus:    f.num("exitstatus"),
		PID:           f.num("pid"),
		Start:         f.time("start"),
		Stop:          f.time("stop"),
		Now:           f.time("now"),
		Logfile:       f.str("logfile"),
		StdoutLogfile: f.str("stdout_logfile"),
		StderrLogfile: f.str("stderr_logfile"),
	}
	return p, f.err
}

func toProcessResults(v any) ([]ProcessResult, error) {
	items, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("expected array, got %T", v)
	}
	out := make([]ProcessResult, 0, len(items))
	for _, item := range items {
		f, err := newFields(item)
		if err != nil {
			return nil, err
		}
		r := ProcessResult{
			Name:        f.str("name"),
			Group:       f.str("group"),
			Status:      f.num("status"),
			Description: f.str("description"),
		}
		if f.err != nil {
			return nil, f.err
		}
		out = append(out, r)
	}
	return out, nil
}

func toStrings(v any) ([]string, error) {
	items, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("expected array, got %T", v)
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("expected string element, got %T", item)
		}
		out = append(out, s)
	}
	return out, nil
}
