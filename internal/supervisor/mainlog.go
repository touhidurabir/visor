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

// MainLogLine is one entry of supervisord's main log: a timestamped line
// plus any continuation lines (a traceback) that follow it.
type MainLogLine struct {
	At    time.Time // parsed in loc, millisecond precision
	Level string    // CRIT, ERRO, WARN, INFO, DEBG, TRAC or BLAT
	Text  string    // the message; continuation lines are joined with "\n"
}

var mainLogLineRE = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2},\d{3}) ([A-Z]+) (.*)$`)

// ParseMainLogLines returns every entry in text, oldest first, with times
// read in loc. A line without a timestamp prefix (a traceback) joins the
// previous entry's Text. Lines before the first timestamped one are dropped:
// text cut from the middle of the log starts with a partial line.
func ParseMainLogLines(text string, loc *time.Location) []MainLogLine {
	var out []MainLogLine
	for line := range strings.Lines(text) {
		line = strings.TrimRight(line, "\r\n")
		if m := mainLogLineRE.FindStringSubmatch(line); m != nil {
			if at, err := time.ParseInLocation(mainLogTime, m[1], loc); err == nil {
				out = append(out, MainLogLine{At: at, Level: m[2], Text: m[3]})
				continue
			}
		}
		if len(out) > 0 {
			out[len(out)-1].Text += "\n" + line
		}
	}
	return out
}

// MainLogChunk is text read from the end of supervisord's main log.
type MainLogChunk struct {
	// Text holds whole lines only: a line supervisord is still writing is
	// left for the next read. A full read may start with a partial line
	// (ParseMainLogLines skips it).
	Text string
	// Cursor marks where Text ends. Pass it to the next ReadMainLog to get
	// only what was written after it.
	Cursor string
	// Since says Text holds only the lines after the cursor passed in.
	// Otherwise it is a full read of the last maxBytes.
	Since bool
	// Truncated says a full read filled maxBytes, so the log is likely
	// longer than what was read.
	Truncated bool
}

// mainLogCursorBytes is how much of the end of the text read a cursor
// keeps: enough to be unique in a log whose lines carry millisecond times.
const mainLogCursorBytes = 256

// ReadMainLog reads the end of supervisord's main log. With a cursor from
// an earlier read it reads only the last followBytes and returns what was
// written after the cursor, so following a long log costs a small read.
// When the cursor is not in that window (more than followBytes were
// written, or the log was rotated or cleared), or there is no cursor, it
// reads the last maxBytes instead. readLog does not report the log's
// size, so the cursor is a piece of the log's text, not an offset.
func (c *Client) ReadMainLog(ctx context.Context, cursor string, followBytes, maxBytes int) (MainLogChunk, error) {
	return readMainLog(func(offset, length int) (string, error) {
		return c.ReadLog(ctx, offset, length)
	}, cursor, followBytes, maxBytes)
}

func readMainLog(read func(offset, length int) (string, error), cursor string, followBytes, maxBytes int) (MainLogChunk, error) {
	if cursor != "" {
		data, err := read(-followBytes, 0)
		if err != nil {
			return MainLogChunk{}, err
		}
		if i := strings.LastIndex(data, cursor); i >= 0 {
			end := i + len(cursor)
			text := wholeLines(data[end:])
			return MainLogChunk{Text: text, Cursor: cursorOf(data[:end+len(text)]), Since: true}, nil
		}
	}
	data, err := read(-maxBytes, 0)
	if err != nil {
		return MainLogChunk{}, err
	}
	text := wholeLines(data)
	return MainLogChunk{Text: text, Cursor: cursorOf(text), Truncated: len(data) >= maxBytes}, nil
}

// wholeLines cuts s after its last newline.
func wholeLines(s string) string { return s[:strings.LastIndexByte(s, '\n')+1] }

// cursorOf is the end of the text read so far.
func cursorOf(read string) string { return read[max(len(read)-mainLogCursorBytes, 0):] }
