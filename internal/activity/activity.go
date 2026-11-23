// Package activity records what changed on a supervisord: processes
// started, stopped or restarted, programs added or removed, config files
// changed on disk, supervisord itself restarted or down, each marked as done
// by Visor or outside it. It derives events from the monitor's updates and
// the config watcher's checks, and keeps them in a file per server. It knows
// nothing about Wails.
package activity

import (
	"fmt"
	"path/filepath"
	"time"

	"errors"

	"github.com/touhidurabir/visor/internal/config"
	"github.com/touhidurabir/visor/internal/issues"
	"github.com/touhidurabir/visor/internal/monitor"
	"github.com/touhidurabir/visor/internal/supervisor"
)

// Kind is what happened.
type Kind string

const (
	ProcessStarted   Kind = "process_started"
	ProcessStopped   Kind = "process_stopped"
	ProcessRestarted Kind = "process_restarted"
	ProgramAdded     Kind = "program_added"
	ProgramRemoved   Kind = "program_removed"
	FileAdded        Kind = "file_added"
	FileChanged      Kind = "file_changed"
	FileRemoved      Kind = "file_removed"
	DaemonRestarted  Kind = "daemon_restarted"
	DaemonDown       Kind = "daemon_down"
	DaemonBack       Kind = "daemon_back"
)

// Group is the mute group a kind belongs to (settings.ActivityMutes).
func (k Kind) Group() string {
	switch k {
	case ProcessStarted, ProcessStopped, ProcessRestarted:
		return "processes"
	case ProgramAdded, ProgramRemoved:
		return "programs"
	case FileAdded, FileChanged, FileRemoved:
		return "files"
	}
	return "daemon"
}

// Event is one thing that happened.
type Event struct {
	ID      string    `json:"id"`
	At      time.Time `json:"at"`
	Kind    Kind      `json:"kind"`
	Process string    `json:"process,omitempty"` // group:name
	Config  string    `json:"config,omitempty"`  // the config file it is about
	ByVisor bool      `json:"byVisor"`
	// Via is the AI tool that made the change through MCP ("Claude Code"),
	// or "" when it was the person in Visor or outside Visor.
	Via    string `json:"via,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// Text is the event as a sentence fragment: "web:web restarted".
func (e Event) Text() string {
	switch e.Kind {
	case ProcessStarted:
		return e.Process + " started"
	case ProcessStopped:
		return e.Process + " stopped"
	case ProcessRestarted:
		return e.Process + " restarted"
	case ProgramAdded:
		return e.Process + " was added"
	case ProgramRemoved:
		return e.Process + " was removed"
	case FileAdded:
		return filepath.Base(e.Config) + " was added on disk"
	case FileChanged:
		return filepath.Base(e.Config) + " changed on disk"
	case FileRemoved:
		return filepath.Base(e.Config) + " was removed from disk"
	case DaemonRestarted:
		return "supervisord restarted"
	case DaemonDown:
		return "supervisord stopped answering"
	case DaemonBack:
		return "supervisord is back"
	}
	return string(e.Kind)
}

// Input is one poll's worth of what the deriver needs.
type Input struct {
	Update monitor.Update
	// Changes is the config watcher's latest result; Checked says it was
	// taken with this update (servers check files every few polls).
	Changes config.ChangeSet
	Checked bool
	// PidMtime is the pidfile's modification time (supervisord's start);
	// zero when unknown.
	PidMtime time.Time
	// ConfigOf finds a process's config file ("" when unknown).
	ConfigOf func(process string) string
	// ByVisor reports whether this Visor's own action covers the process
	// ("" is supervisord itself; "file:<path>" a config file) at time t.
	ByVisor func(process string, t time.Time) bool
	// Via names the AI tool whose MCP call covers the process at t, or "".
	Via func(process string, t time.Time) string
	// Crashed reports a process the issue tracker found crashing in this
	// update: its restart is supervisord's autorestart, not a change.
	Crashed func(process string) bool
}

// Deriver turns consecutive inputs into events. A stop followed by a start
// within RestartWindow is one restart; the stop is held back that long so
// the two can merge. Not safe for concurrent use.
type Deriver struct {
	RestartWindow time.Duration

	held      map[string]Event // stops waiting for a start, by process
	files     map[string]Kind  // the last ChangeSet, by path
	haveFiles bool
	pid       time.Time
	failed    int  // polls in a row supervisord didn't answer (not counting SSH drops and restarts)
	down      bool // DaemonDown was reported
	seen      bool // a reachable update was handled: the next ones are changes
	seq       int
}

// DownPolls failed polls in a row make supervisord "down".
const DownPolls = 2

// NewDeriver returns a Deriver with a 10 s restart window.
func NewDeriver() *Deriver {
	return &Deriver{RestartWindow: 10 * time.Second, held: map[string]Event{}, files: map[string]Kind{}}
}

func (d *Deriver) id(at time.Time) string {
	d.seq++
	return fmt.Sprintf("%d-%d", at.UnixMilli(), d.seq)
}

func running(s supervisor.ProcessState) bool {
	return s == supervisor.StateRunning || s == supervisor.StateStarting || s == supervisor.StateBackoff
}

// Next returns the events in, plus stops held long enough.
func (d *Deriver) Next(in Input) []Event {
	now := in.Update.Snapshot.At
	if now.IsZero() {
		now = time.Now()
	}
	var out []Event
	mk := func(k Kind, process, conf string, at time.Time) Event {
		e := Event{ID: d.id(at), At: at, Kind: k, Process: process, Config: conf}
		p := process
		switch k.Group() {
		case "daemon":
			p = ""
		case "files":
			p = "file:" + conf
		}
		if in.ByVisor != nil {
			e.ByVisor = in.ByVisor(p, at)
		}
		if in.Via != nil {
			e.Via = in.Via(p, at)
		}
		return e
	}
	configOf := func(p string) string {
		if in.ConfigOf == nil {
			return ""
		}
		return in.ConfigOf(p)
	}

	// supervisord itself.
	restartedDaemon := false
	if !in.PidMtime.IsZero() {
		if !d.pid.IsZero() && in.PidMtime.After(d.pid) {
			restartedDaemon = true
		}
		d.pid = in.PidMtime
	}
	// Down after DownPolls failed polls, like the tracker's unreachable
	// issue; a lost SSH connection or supervisord restarting is not
	// supervisord going down.
	snap := in.Update.Snapshot
	switch {
	case snap.Reachable:
		d.failed = 0
		if d.down {
			d.down = false
			if !restartedDaemon {
				out = append(out, mk(DaemonBack, "", "", now))
			}
		}
	case !snap.At.IsZero() && !errors.Is(snap.Err, issues.ErrHostDown) && !errors.Is(snap.Err, supervisor.ErrShutdownState):
		d.failed++
		if d.failed >= DownPolls && !d.down {
			d.down = true
			out = append(out, mk(DaemonDown, "", "", now))
		}
	}
	if restartedDaemon {
		out = append(out, mk(DaemonRestarted, "", "", now))
	}

	// Processes. Around a daemon restart every process restarts: that is
	// the daemon's event, not one per process. The first reachable update,
	// and one after an outage, list processes rather than changes.
	relist := !d.seen
	for _, ev := range in.Update.Events {
		relist = relist || ev.Kind == monitor.Reconnected
	}
	if in.Update.Snapshot.Reachable {
		d.seen = true
	}
	crashed := func(p string) bool { return in.Crashed != nil && in.Crashed(p) }
	if !restartedDaemon && !relist && in.Update.Snapshot.Reachable {
		for _, ev := range in.Update.Events {
			at := ev.At
			if at.IsZero() {
				at = now
			}
			conf := configOf(ev.Process)
			switch ev.Kind {
			case monitor.Added:
				out = append(out, mk(ProgramAdded, ev.Process, conf, at))
			case monitor.Removed:
				delete(d.held, ev.Process)
				out = append(out, mk(ProgramRemoved, ev.Process, conf, at))
			case monitor.Restarted:
				if running(ev.To) && !crashed(ev.Process) {
					out = append(out, mk(ProcessRestarted, ev.Process, conf, at))
				}
			case monitor.StateChanged:
				switch {
				case ev.To == supervisor.StateStopped && (running(ev.From) || ev.From == supervisor.StateStopping):
					// Held: a start soon after makes it a restart.
					d.held[ev.Process] = mk(ProcessStopped, ev.Process, conf, at)
				case (ev.From == supervisor.StateRunning && ev.To == supervisor.StateStarting ||
					ev.From == supervisor.StateStopping && running(ev.To)) && !crashed(ev.Process):
					// Stopped and started again between two polls (a slow
					// stop is seen as STOPPING first, then up again).
					out = append(out, mk(ProcessRestarted, ev.Process, conf, at))
				case (ev.From == supervisor.StateStopped || ev.From == supervisor.StateFatal) && running(ev.To):
					if stop, ok := d.held[ev.Process]; ok && at.Sub(stop.At) <= d.RestartWindow {
						delete(d.held, ev.Process)
						r := mk(ProcessRestarted, ev.Process, conf, at)
						r.ByVisor = r.ByVisor || stop.ByVisor
						if r.Via == "" {
							r.Via = stop.Via
						}
						out = append(out, r)
						continue
					}
					out = append(out, mk(ProcessStarted, ev.Process, conf, at))
				}
			}
		}
	}
	for p, stop := range d.held {
		if now.Sub(stop.At) > d.RestartWindow {
			delete(d.held, p)
			out = append(out, stop)
		}
	}

	// Config files changed on disk (Visor's own saves are marked loaded at
	// once, so they never show here).
	if in.Checked {
		cur := map[string]Kind{}
		for _, p := range in.Changes.Added {
			cur[p] = FileAdded
		}
		for _, p := range in.Changes.Changed {
			cur[p] = FileChanged
		}
		for _, p := range in.Changes.Removed {
			cur[p] = FileRemoved
		}
		if d.haveFiles {
			for p, k := range cur {
				if d.files[p] != k {
					out = append(out, mk(k, "", p, now))
				}
			}
		}
		d.files, d.haveFiles = cur, true
	}
	return out
}
