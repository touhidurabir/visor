// Package monitor polls supervisord, diffs successive snapshots into events,
// and fans updates out to subscribers. It is the single source of truth for
// process state: the UI listens to it instead of polling supervisord.
package monitor

import (
	"time"

	"github.com/touhidurabir/visor/internal/supervisor"
)

// Snapshot is the result of one poll. When Reachable is false, Err says why
// and State and Processes are empty. Processes are sorted by FullName.
// The zero Snapshot (At is zero) means no poll has completed yet.
type Snapshot struct {
	At        time.Time
	Reachable bool
	Err       error
	State     supervisor.SupervisorState
	Processes []supervisor.ProcessInfo
}

// EventKind says what changed between two snapshots.
type EventKind int

const (
	// StateChanged: a process moved From one state To another.
	StateChanged EventKind = iota
	// Added: a process appeared, in state To.
	Added
	// Removed: a process disappeared; it was in state From.
	Removed
	// Unreachable: supervisord stopped answering. Reported once per outage.
	Unreachable
	// Reconnected: supervisord answers again after being unreachable.
	Reconnected
)

func (k EventKind) String() string {
	switch k {
	case StateChanged:
		return "StateChanged"
	case Added:
		return "Added"
	case Removed:
		return "Removed"
	case Unreachable:
		return "Unreachable"
	case Reconnected:
		return "Reconnected"
	}
	return "EventKind(?)"
}

// Event is one change. Process is the "group:name" full name and Info the
// process as last seen (the new info, or the old one for Removed); both
// are empty for Unreachable and Reconnected. At is the newer snapshot's time.
type Event struct {
	Kind     EventKind
	At       time.Time
	Process  string
	From, To supervisor.ProcessState
	Info     supervisor.ProcessInfo
}

// Diff returns the events that turn prev into next.
//
//   - next unreachable: Unreachable, unless prev was already unreachable
//     (one event per outage). Processes are not reported as Removed.
//   - prev unreachable, next reachable: Reconnected, then the process
//     changes from prev.Processes, so a caller that keeps the last known
//     processes in prev learns what changed during the outage.
//   - The zero prev (no poll yet) counts as reachable with no processes:
//     the first poll yields Added for every process, or Unreachable.
//
// Only state transitions count as changes; a new pid or description in
// the same state is not an event. Events follow next's process order, with
// removals last.
func Diff(prev, next Snapshot) []Event {
	first := prev.At.IsZero()
	if !next.Reachable {
		if first || prev.Reachable {
			return []Event{{Kind: Unreachable, At: next.At}}
		}
		return nil
	}

	var evs []Event
	if !first && !prev.Reachable {
		evs = append(evs, Event{Kind: Reconnected, At: next.At})
	}
	old := make(map[string]supervisor.ProcessInfo, len(prev.Processes))
	for _, p := range prev.Processes {
		old[p.FullName()] = p
	}
	seen := make(map[string]bool, len(next.Processes))
	for _, p := range next.Processes {
		name := p.FullName()
		seen[name] = true
		was, ok := old[name]
		switch {
		case !ok:
			evs = append(evs, Event{Kind: Added, At: next.At, Process: name, To: p.State, Info: p})
		case was.State != p.State:
			evs = append(evs, Event{Kind: StateChanged, At: next.At, Process: name, From: was.State, To: p.State, Info: p})
		}
	}
	for _, p := range prev.Processes {
		if name := p.FullName(); !seen[name] {
			evs = append(evs, Event{Kind: Removed, At: next.At, Process: name, From: p.State, Info: p})
		}
	}
	return evs
}
