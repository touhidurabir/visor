package supervisor

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ExitLine is one "exited:" line of supervisord's main log: a process ended
// and supervisord decided whether it meant to.
type ExitLine struct {
	At       time.Time // parsed in loc, millisecond precision
	Name     string    // process name, no group
	Desc     string    // "exit status 1", "terminated by SIGKILL"
	Status   int       // exit code; -1 when terminated by a signal
	Expected bool
}

// mainLogTime is supervisord's asctime layout, in its local time.
const mainLogTime = "2006-01-02 15:04:05,000"

// exitLineRE matches `<asctime> <LEVEL> exited: <name> (<desc>; [not ]expected)`
// as supervisord 4's process.finish() writes it. <desc> may itself hold
// parentheses ("terminated by SIGSEGV (core dumped)"); a name holds none.
var exitLineRE = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2},\d{3}) [A-Z]+ exited: ([^ ()]+) \((.+); (expected|not expected)\)$`)

// ParseExitLines returns the "exited:" lines in text, oldest first, with
// times read in loc. Other lines, malformed ones, and a partial first line
// (text cut from the middle of the log never starts with a whole timestamp)
// are ignored. "stopped:" lines (stops asked for) are not exits.
func ParseExitLines(text string, loc *time.Location) []ExitLine {
	var out []ExitLine
	for line := range strings.Lines(text) {
		m := exitLineRE.FindStringSubmatch(strings.TrimRight(line, "\r\n"))
		if m == nil {
			continue
		}
		at, err := time.ParseInLocation(mainLogTime, m[1], loc)
		if err != nil {
			continue
		}
		status := -1
		if code, ok := strings.CutPrefix(m[3], "exit status "); ok {
			if n, err := strconv.Atoi(code); err == nil {
				status = n
			}
		}
		out = append(out, ExitLine{At: at, Name: m[2], Desc: m[3], Status: status, Expected: m[4] == "expected"})
	}
	return out
}

// RecentExits reads the last tailBytes of supervisord's main log and returns
// its exit lines. It fails with ErrNoFile when supervisord logs to syslog
// or nowhere. With loglevel above info, expected exits are not logged, so
// only unexpected ones come back.
func (c *Client) RecentExits(ctx context.Context, tailBytes int) ([]ExitLine, error) {
	text, err := c.ReadLog(ctx, -tailBytes, 0)
	if err != nil {
		return nil, err
	}
	return ParseExitLines(text, time.Local), nil
}
