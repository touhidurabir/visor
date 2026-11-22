// Package mcp is Visor's MCP server: the tools AI agents (Claude Code,
// Codex, Cursor) call, who may call which on each machine, asking in Visor
// before changes, rate limits, and the HTTP handler with its token and
// Origin checks. It talks to Visor through Backend and Target, which
// internal/app implements over its tabs, and knows nothing about Wails.
package mcp

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// Level is how much AI agents may do on a machine. Each includes the ones
// before it.
type Level int

const (
	Off     Level = iota // the machine is invisible to MCP
	Read                 // read tools only
	Control              // start, stop, restart, signal, apply
	Configs              // save, create, remove and restore configs
)

var levelNames = []string{"off", "read", "control", "configs"}

// String is the level as settings store it: "off", "read", "control" or "configs".
func (l Level) String() string {
	if l < Off || l > Configs {
		return fmt.Sprintf("level(%d)", int(l))
	}
	return levelNames[l]
}

// Label is the level as the MCP page shows it.
func (l Level) Label() string {
	switch l {
	case Read:
		return "Read"
	case Control:
		return "Control"
	case Configs:
		return "Change configs"
	}
	return "Off"
}

// ParseLevel reads a stored level.
func ParseLevel(s string) (Level, error) {
	for i, n := range levelNames {
		if n == s {
			return Level(i), nil
		}
	}
	return Off, fmt.Errorf("unknown MCP access level %q: use off, read, control or configs", s)
}

// TagProduction is settings.TagProduction (kept here, so the core needs no
// settings import for one string).
const TagProduction = "production"

// Machine is this computer or a saved server, as MCP sees it.
type Machine struct {
	ID   string // "local" for this computer, else the server id
	Name string
	Tag  string
	// Local is this computer; Connected: its tab is open and connected.
	Local, Connected bool
	// ReadOnly is the server's Read only setting (M7).
	ReadOnly bool
	// Level and Ask are as set on the MCP page; Effective applies the limits.
	Level Level
	Ask   bool
}

// Production reports whether the machine is tagged production.
func (m Machine) Production() bool { return m.Tag == TagProduction }

// Effective is the level MCP applies on m and whether a change there asks in
// Visor first. A Read only server goes no higher than Read; a production
// server always asks before a change. Reads never ask.
func Effective(m Machine) (Level, bool) {
	l := m.Level
	if m.ReadOnly && l > Read {
		l = Read
	}
	return l, l > Read && (m.Ask || m.Production())
}

// Default is a machine's level and asking before the person chooses: this
// computer at Control without asking, servers Off and asking.
func Default(local bool) (Level, bool) {
	if local {
		return Control, false
	}
	return Off, true
}

// ErrRateLimited: too many calls; the error says how long to wait.
var ErrRateLimited = errors.New("too many calls")

// Rate limits (spec §5).
const (
	changeGap   = time.Second
	callsWindow = 10 * time.Second
	maxCalls    = 30
)

// Limiter keeps an agent stuck in a loop from hammering a machine: at most
// one change per second per machine, and 30 calls per 10 seconds overall.
type Limiter struct {
	mu      sync.Mutex
	now     func() time.Time
	calls   []time.Time
	changes map[string]time.Time
}

// NewLimiter returns a Limiter on the given clock (time.Now in the app).
func NewLimiter(now func() time.Time) *Limiter {
	return &Limiter{now: now, changes: map[string]time.Time{}}
}

// Allow records a call on machineID and refuses it when it is over a limit.
func (l *Limiter) Allow(machineID string, change bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	keep := l.calls[:0]
	for _, t := range l.calls {
		if now.Sub(t) < callsWindow {
			keep = append(keep, t)
		}
	}
	l.calls = keep
	if len(l.calls) >= maxCalls {
		return fmt.Errorf("%w: at most %d calls in %s; wait %s and try again",
			ErrRateLimited, maxCalls, callsWindow, roundUp(callsWindow-now.Sub(l.calls[0])))
	}
	if last, ok := l.changes[machineID]; change && ok && now.Sub(last) < changeGap {
		return fmt.Errorf("%w: at most one change per second on a machine; wait %s and try again",
			ErrRateLimited, roundUp(changeGap-now.Sub(last)))
	}
	l.calls = append(l.calls, now)
	if change {
		l.changes[machineID] = now
	}
	return nil
}

func roundUp(d time.Duration) time.Duration {
	if d < time.Second {
		return time.Second
	}
	return d.Round(time.Second)
}
