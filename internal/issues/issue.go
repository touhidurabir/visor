// Package issues turns monitor updates into a durable list of problems
// (failed, crashed, crash-looping or unreachable processes) and a history of
// state transitions. It knows nothing about Wails.
package issues

import (
	"errors"
	"time"
)

// Kind says what went wrong.
type Kind string

const (
	KindFatal          Kind = "fatal"
	KindUnexpectedExit Kind = "unexpected_exit"
	KindCrashLoop      Kind = "crash_loop"
	KindUnreachable    Kind = "unreachable"
	// KindHostDown: a server's SSH connection is lost, so supervisord there
	// can't be asked (not the same as supervisord not answering).
	KindHostDown Kind = "host_down"
)

// Status is where an issue is in its life.
type Status string

const (
	StatusOpen         Status = "open"
	StatusResolvedAuto Status = "resolved_auto"
	StatusResolvedUser Status = "resolved_user"
	StatusDismissed    Status = "dismissed"
)

// Issue is one problem. Process is empty for unreachable. ClosedAt is zero
// while the issue is open.
type Issue struct {
	ID       string    `json:"id"`
	Kind     Kind      `json:"kind"`
	Process  string    `json:"process"`
	Config   string    `json:"config"`
	Message  string    `json:"message"`
	OpenedAt time.Time `json:"openedAt"`
	ClosedAt time.Time `json:"closedAt"`
	Status   Status    `json:"status"`
	Notified bool      `json:"notified"`
	Note     string    `json:"note,omitempty"`
	// Count is how many times the failure happened while the issue was
	// open (0 in records from before it was counted: once). LastAt is
	// when it last happened (zero: only at OpenedAt).
	Count  int       `json:"count,omitempty"`
	LastAt time.Time `json:"lastAt,omitzero"`
}

// Times is how many times the failure happened while the issue was open.
func (i Issue) Times() int { return max(i.Count, 1) }

// Latest is when the failure last happened.
func (i Issue) Latest() time.Time {
	if i.LastAt.IsZero() {
		return i.OpenedAt
	}
	return i.LastAt
}

// Transition is one process state change.
type Transition struct {
	At      time.Time `json:"at"`
	Process string    `json:"process"`
	From    string    `json:"from"`
	To      string    `json:"to"`
	Note    string    `json:"note"`
}

// Rules tune when issues open and close.
type Rules struct {
	// CrashLoopRestarts restarts within CrashLoopWindow open a crash loop.
	CrashLoopRestarts int
	CrashLoopWindow   time.Duration
	// StableFor is how long a process must stay RUNNING to close a fatal issue.
	StableFor time.Duration
	// UnreachablePolls consecutive failed polls open an unreachable issue.
	UnreachablePolls int
}

// DefaultRules are the rules Visor ships with.
var DefaultRules = Rules{
	CrashLoopRestarts: 5,
	CrashLoopWindow:   10 * time.Minute,
	StableFor:         60 * time.Second,
	UnreachablePolls:  2,
}

// ErrHostDown is wrapped in a poll's error when the SSH connection to the
// server is down: the tracker opens KindHostDown for it, not KindUnreachable.
var ErrHostDown = errors.New("the server can't be reached over SSH")
