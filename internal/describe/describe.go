// Package describe explains program configs in plain English. It is
// deterministic and rule-based (no AI): the text comes from the program's
// keys and supervisord's defaults, and "worth a look" flags point at risky
// settings.
package describe

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/touhidurabir/visor/internal/config"
)

// Program is the description of one [program:x] section. Flags are short
// "worth a look" sentences, each starting with the program name.
type Program struct {
	Name, Text string
	Flags      []string
}

const autoLogs = "an auto-named file in supervisord's log folder"

// File describes every [program:x] in content, in file order. A file that
// does not parse returns the parse error.
func File(content string) ([]Program, error) {
	f, err := config.ParseINI(strings.NewReader(content), "")
	if err != nil {
		return nil, err
	}
	var out []Program
	for _, s := range f.SectionsWithPrefix("program:") {
		out = append(out, describe(s))
	}
	return out, nil
}

// opts reads a section's keys with supervisord's defaults.
type opts struct{ s config.Section }

func (o opts) str(key string) string {
	v, _ := o.s.Get(key)
	return strings.TrimSpace(strings.ReplaceAll(v, "\n", " "))
}

func (o opts) integer(key string, def int) int {
	if n, err := strconv.Atoi(o.str(key)); err == nil {
		return n
	}
	return def
}

func (o opts) boolean(key string, def bool) bool {
	switch strings.ToLower(o.str(key)) {
	case "true", "yes", "on", "1":
		return true
	case "false", "no", "off", "0":
		return false
	}
	return def
}

// zeroBytes reports whether key is set to a size of zero ("0", "0MB").
func (o opts) zeroBytes(key string) bool {
	v := strings.ToLower(o.str(key))
	if v == "" {
		return false
	}
	v = strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(v, "kb"), "mb"), "gb")
	n, err := strconv.Atoi(v)
	return err == nil && n == 0
}

func isAuto(v string) bool { return v == "" || strings.EqualFold(v, "AUTO") }
func isNone(v string) bool { return strings.EqualFold(v, "NONE") }

func describe(s config.Section) Program {
	o := opts{s}
	name := strings.TrimPrefix(s.Name, "program:")
	n := max(o.integer("numprocs", 1), 1)
	autorestart := strings.ToLower(o.str("autorestart"))
	if autorestart != "true" && autorestart != "false" {
		autorestart = "unexpected"
	}
	redirect := o.boolean("redirect_stderr", false)
	stdout, stderr := o.str("stdout_logfile"), o.str("stderr_logfile")
	user := o.str("user")
	startsecs := o.integer("startsecs", 1)
	procName := o.str("process_name")
	if procName == "" {
		procName = "%(program_name)s"
	}
	needsNum := strings.Contains(procName, "%(process_num)")

	// 1. What runs, where, as whom.
	var text strings.Builder
	if cmd := o.str("command"); cmd == "" {
		text.WriteString("Has no command yet.")
	} else {
		text.WriteString("Runs " + cmd)
		if dir := o.str("directory"); dir != "" {
			text.WriteString(" in " + dir)
		}
		if user != "" {
			text.WriteString(" as user " + user + ".")
		} else {
			text.WriteString(" as you.")
		}
	}

	// 2. Start, restart and stop.
	copies := ""
	if n > 1 && needsNum {
		copies = " (" + copyNames(name, procName, o.integer("numprocs_start", 0), n) + ")"
	}
	it, each := "it", "it"
	if n > 1 {
		it, each = "them", "each copy"
	}
	var start string
	switch {
	case n == 1 && o.boolean("autostart", true):
		start = "Starts with supervisord"
	case n == 1:
		start = "Waits for you to start it"
	case o.boolean("autostart", true):
		start = fmt.Sprintf("Starts %d copies%s when supervisord starts", n, copies)
	default:
		start = fmt.Sprintf("Waits for you to start its %d copies%s", n, copies)
	}
	var restart string
	switch autorestart {
	case "true":
		restart = "restarts " + each + " whenever it exits"
	case "false":
		restart = "never restarts " + it
	default:
		restart = "restarts " + each + " only if it exits with an unexpected code"
	}
	stopTo := "it"
	if n > 1 {
		stopTo = "each"
	}
	text.WriteString(" " + start + ", " + restart + ", and gives " + stopTo + " " +
		seconds(o.integer("stopwaitsecs", 10)) + " to stop.")

	// 3. Logs.
	text.WriteString(" " + output(stdout, stderr, redirect))

	p := Program{Name: name, Text: text.String()}

	// Worth a look.
	if autorestart == "true" && startsecs <= 1 {
		p.Flags = append(p.Flags, fmt.Sprintf("%s: startsecs=%d with autorestart on, so a crash loop burns through its %d retries in seconds.",
			name, startsecs, o.integer("startretries", 3)))
	}
	if user == "root" {
		p.Flags = append(p.Flags, name+" runs as root.")
	}
	if !redirect && isAuto(stderr) {
		p.Flags = append(p.Flags, name+" has no stderr_logfile, so errors go to "+autoLogs+".")
	}
	for _, l := range []struct{ key, file string }{
		{"stdout_logfile_maxbytes", stdout}, {"stderr_logfile_maxbytes", stderr},
	} {
		isErr := strings.HasPrefix(l.key, "stderr")
		if (isErr && redirect) || isNone(l.file) || strings.EqualFold(l.file, "syslog") {
			continue
		}
		if o.zeroBytes(l.key) {
			p.Flags = append(p.Flags, fmt.Sprintf("%s: %s=0, so that log is never rotated and keeps growing.", name, l.key))
		}
	}
	if n > 1 && !needsNum {
		p.Flags = append(p.Flags, fmt.Sprintf("%s: numprocs=%d but process_name has no %%(process_num), so supervisord will refuse to load it.", name, n))
	}
	return p
}

func seconds(n int) string {
	if n == 1 {
		return "1 second"
	}
	return fmt.Sprintf("%d seconds", n)
}

// copyNames shows the first and last copy name, shortening the last one:
// "mailer_00 and _01", "mailer_01 to _04".
func copyNames(program, procName string, start, n int) string {
	name := func(i int) string {
		return config.Expand(procName, map[string]string{
			"program_name": program, "group_name": program, "process_num": strconv.Itoa(i),
		})
	}
	first, last := name(start), name(start+n-1)
	short := last
	common := 0
	for common < len(first) && common < len(last) && first[common] == last[common] {
		common++
	}
	for i := common - 1; i >= 0; i-- {
		c := last[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') {
			short = last[i:]
			break
		}
	}
	if n == 2 {
		return first + " and " + short
	}
	return first + " to " + short
}

// dest names where one stream goes.
func dest(v string) string {
	switch {
	case isAuto(v):
		return autoLogs
	case strings.EqualFold(v, "syslog"):
		return "syslog"
	}
	return v
}

func output(stdout, stderr string, redirect bool) string {
	if redirect {
		if isNone(stdout) {
			return "Output and errors are discarded."
		}
		return "Output and errors both go to " + dest(stdout) + "."
	}
	out := "Output goes to " + dest(stdout) + "."
	if isNone(stdout) {
		out = "Output is discarded."
	}
	if isNone(stderr) {
		return out + " Errors are discarded."
	}
	return out + " Errors go to " + dest(stderr) + "."
}
