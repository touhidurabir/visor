package supervisor

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ExitLine is one "exited:" or "stopped:" line of supervisord's main log: a
// process ended, and supervisord either decided whether it meant to
// (exited) or had been asked to stop it (stopped).
type ExitLine struct {
	At       time.Time // parsed in loc, millisecond precision
	Name     string    // process name, no group
	Desc     string    // "exit status 1", "terminated by SIGKILL"
	Status   int       // exit code; -1 when terminated by a signal
	Expected bool
	// Stopped marks a "stopped:" line: supervisord stopped the process
	// because someone asked it to (a stop, a restart, a reload of the
	// daemon, or an update that re-adds the group), whatever the exit
	// code. Expected is then false.
	Stopped bool
}

// SpawnLine is one "spawned:" line: supervisord started a process.
type SpawnLine struct {
	At   time.Time
	Name string // process name, no group
	PID  int
}

// ProcessLog is what a stretch of supervisord's main log says about
// processes ending and starting, each list oldest first.
type ProcessLog struct {
	Exits  []ExitLine
	Spawns []SpawnLine
}

// mainLogTime is supervisord's asctime layout, in its local time.
const mainLogTime = "2006-01-02 15:04:05,000"

// supervisord 4's process.finish() and spawn() write these, after
// `<asctime> <LEVEL> `. <desc> may itself hold parentheses ("terminated by
// SIGSEGV (core dumped)"); a name holds none.
var (
	exitLineRE  = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2},\d{3}) [A-Z]+ exited: ([^ ()]+) \((.+); (expected|not expected)\)$`)
	stopLineRE  = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2},\d{3}) [A-Z]+ stopped: ([^ ()]+) \((.+)\)$`)
	spawnLineRE = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2},\d{3}) [A-Z]+ spawned: '([^ ()']+)' with pid (\d+)$`)
)

// ParseExitLines returns the "exited:" and "stopped:" lines in text, oldest
// first, with times read in loc.
func ParseExitLines(text string, loc *time.Location) []ExitLine {
	return ParseProcessLog(text, loc).Exits
}

// ParseProcessLog returns the exit, stop and spawn lines in text, with
// times read in loc. Other lines, malformed ones, and a partial first line
// (text cut from the middle of the log never starts with a whole
// timestamp) are ignored.
func ParseProcessLog(text string, loc *time.Location) ProcessLog {
	var out ProcessLog
	for line := range strings.Lines(text) {
		line = strings.TrimRight(line, "\r\n")
		if m := spawnLineRE.FindStringSubmatch(line); m != nil {
			at, err := time.ParseInLocation(mainLogTime, m[1], loc)
			pid, perr := strconv.Atoi(m[3])
			if err == nil && perr == nil {
				out.Spawns = append(out.Spawns, SpawnLine{At: at, Name: m[2], PID: pid})
			}
			continue
		}
		var l ExitLine
		if m := exitLineRE.FindStringSubmatch(line); m != nil {
			l = ExitLine{Name: m[2], Desc: m[3], Expected: m[4] == "expected"}
			line = m[1]
		} else if m := stopLineRE.FindStringSubmatch(line); m != nil {
			l = ExitLine{Name: m[2], Desc: m[3], Stopped: true}
			line = m[1]
		} else {
			continue
		}
		at, err := time.ParseInLocation(mainLogTime, line, loc)
		if err != nil {
			continue
		}
		l.At, l.Status = at, exitStatus(l.Desc)
		out.Exits = append(out.Exits, l)
	}
	return out
}

// exitStatus reads the code from "exit status N"; -1 for a signal.
func exitStatus(desc string) int {
	if code, ok := strings.CutPrefix(desc, "exit status "); ok {
		if n, err := strconv.Atoi(code); err == nil {
			return n
		}
	}
	return -1
}

// RecentProcessLog reads the last tailBytes of supervisord's main log and
// returns its exit, stop and spawn lines. It fails with ErrNoFile when
// supervisord logs to syslog or nowhere. With loglevel above info, expected
// exits, spawns, and stops with an expected code are not logged.
func (c *Client) RecentProcessLog(ctx context.Context, tailBytes int) (ProcessLog, error) {
	text, err := c.ReadLog(ctx, -tailBytes, 0)
	if err != nil {
		return ProcessLog{}, err
	}
	return ParseProcessLog(text, time.Local), nil
}
