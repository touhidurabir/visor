package describe

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/touhidurabir/visor/internal/config"
)

// Explanation explains a config file line by line: a short summary, then
// every setting in file order, grouped by section, with what it means.
// It is deterministic and rule-based, like Program.
type Explanation struct {
	Summary  string
	Sections []Section
}

// Section is one [name] block. Program is the program's name for a
// [program:x] section and "" for any other kind.
type Section struct {
	Name    string // as written: "program:web", "group:shop"
	Program string
	Lines   []Line
}

// Line kinds.
const (
	KindSection = "section" // the [name] header
	KindKey     = "key"     // a setting
	KindUnset   = "unset"   // settings the section leaves out (not a line in the file)
)

// Line is one explained line. Line is 1-based (for KindUnset, the line
// after which settings would be added). Text is the line as written, with
// continuation lines joined. For KindUnset, Unset lists the keys left out,
// Suggest says why setting some of them would help, and Add are lines
// that would.
type Line struct {
	Line    int
	Text    string
	Key     string
	Kind    string
	Meaning string
	Unset   []string
	Suggest string
	Add     []string
}

// Hints are facts from outside the file that make suggestions concrete.
// LogDir maps a program to a folder its suggested log files can go in
// (supervisord's log folder, from where its AUTO logs are).
type Hints struct {
	LogDir map[string]string
}

// Explain explains content. A file that does not parse returns the
// parse error.
func Explain(content string, h Hints) (Explanation, error) {
	f, err := config.ParseINI(strings.NewReader(content), "")
	if err != nil {
		return Explanation{}, err
	}
	raw := strings.Split(content, "\n")
	var ex Explanation
	var progs []string
	for _, s := range f.Sections {
		sec := Section{Name: s.Name}
		if name, ok := strings.CutPrefix(s.Name, "program:"); ok {
			sec.Program = name
			progs = append(progs, programSummary(s))
		}
		sec.Lines = append(sec.Lines, Line{Line: s.Line, Text: rawLine(raw, s.Line), Kind: KindSection, Meaning: sectionMeaning(s)})
		o := opts{s}
		for _, k := range s.Keys {
			l := Line{Line: k.Line, Text: keyText(raw, k), Key: k.Name, Kind: KindKey}
			if sec.Program != "" {
				l.Meaning = keyMeaning(k, o, sec.Program)
			} else {
				l.Meaning = otherMeaning(s.Name, k)
			}
			sec.Lines = append(sec.Lines, l)
		}
		if sec.Program != "" {
			if u, ok := unsetRow(s, sec.Program, h.LogDir[sec.Program]); ok {
				sec.Lines = append(sec.Lines, u)
			}
		}
		ex.Sections = append(ex.Sections, sec)
	}
	ex.Summary = fileSummary(progs)
	return ex, nil
}

func rawLine(raw []string, n int) string {
	if n < 1 || n > len(raw) {
		return ""
	}
	return strings.TrimSpace(raw[n-1])
}

// keyText is a setting as written: its first line plus any continuation
// lines (indented lines right after it).
func keyText(raw []string, k config.Key) string {
	text := rawLine(raw, k.Line)
	for i := k.Line; i < len(raw) && strings.Count(k.Value, "\n") > 0; i++ {
		line := raw[i]
		if line == "" || (line[0] != ' ' && line[0] != '\t') {
			break
		}
		text += " " + strings.TrimSpace(line)
	}
	return text
}

var numberWords = []string{"no", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine"}

func count(n int, one, many string) string {
	w := strconv.Itoa(n)
	if n < len(numberWords) {
		w = numberWords[n]
	}
	if n == 1 {
		return w + " " + one
	}
	return w + " " + many
}

func fileSummary(progs []string) string {
	if len(progs) == 0 {
		return "This file has no [program:x] section, so it starts nothing."
	}
	return "This file sets up " + count(len(progs), "background program", "background programs") + ". " + strings.Join(progs, " ")
}

// programSummary is one sentence about a program: what it runs, when it
// starts, and where its output goes.
func programSummary(s config.Section) string {
	o := opts{s}
	name := strings.TrimPrefix(s.Name, "program:")
	n := max(o.integer("numprocs", 1), 1)
	what := purpose(o.str("command"))
	if what == "" {
		return name + " has no command yet."
	}
	var b strings.Builder
	b.WriteString(name + " runs ")
	if n > 1 {
		b.WriteString(count(n, "copy", "copies") + " of ")
	}
	b.WriteString(what)
	if o.boolean("autostart", true) {
		b.WriteString(" and starts with supervisord")
	} else {
		b.WriteString(" and waits for you to start it")
	}
	if out := o.str("stdout_logfile"); !isAuto(out) && !isNone(out) && !strings.EqualFold(out, "syslog") {
		b.WriteString(". Its output goes to " + out)
	}
	return b.String() + "."
}

var (
	queueWork  = regexp.MustCompile(`artisan\s+queue:(work|listen)\b`)
	horizon    = regexp.MustCompile(`artisan\s+horizon\b`)
	schedule   = regexp.MustCompile(`artisan\s+schedule:work\b`)
	flagNumber = func(name string) *regexp.Regexp { return regexp.MustCompile(`--` + name + `[= ](\d+)`) }
	triesFlag  = flagNumber("tries")
	maxTime    = flagNumber("max-time")
	queueFlag  = regexp.MustCompile(`--queue[= ]([\w,.-]+)`)
)

// purpose names what a command is, as plainly as Visor can tell: "a
// Laravel queue worker", "a Node.js app", "a shell command", else the
// program's file name.
func purpose(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	switch {
	case cmd == "":
		return ""
	case horizon.MatchString(cmd):
		return "Laravel Horizon"
	case queueWork.MatchString(cmd):
		return "a Laravel queue worker"
	case schedule.MatchString(cmd):
		return "Laravel’s scheduler"
	}
	full := firstField(cmd)
	bin := filepath.Base(full)
	switch bin {
	case "node", "npm", "npx", "yarn", "pnpm", "bun":
		return "a Node.js app"
	case "php":
		return "a PHP script"
	case "python", "python3":
		return "a Python program"
	case "ruby", "bundle":
		return "a Ruby program"
	case "sh", "bash", "zsh":
		if strings.Contains(cmd, " -c") {
			return "a shell command"
		}
		return "a shell script"
	}
	if filepath.IsAbs(full) {
		return "the program at " + full
	}
	return bin
}

func firstField(cmd string) string {
	if f := strings.Fields(strings.Trim(cmd, `"'`)); len(f) > 0 {
		return strings.Trim(f[0], `"'`)
	}
	return ""
}

func sectionMeaning(s config.Section) string {
	kind, name, _ := strings.Cut(s.Name, ":")
	switch kind {
	case "program":
		return "Starts a program section. Visor and supervisorctl call it " + name + "."
	case "group":
		return "A group: its programs can be started and stopped together as " + name + "."
	case "eventlistener":
		return "An event listener called " + name + ": supervisord sends it events, such as a process changing state."
	case "fcgi-program":
		return "A FastCGI program called " + name + "."
	}
	return "A [" + s.Name + "] section."
}

func otherMeaning(section string, k config.Key) string {
	if strings.HasPrefix(section, "group:") {
		switch k.Name {
		case "programs":
			return "The programs in this group: " + strings.ReplaceAll(strings.TrimSpace(k.Value), ",", ", ") + "."
		case "priority":
			return "Start order of the group: lower numbers start first and stop last."
		}
	}
	return "A setting of this section."
}

// keyMeaning says what one [program:x] setting does with its value.
func keyMeaning(k config.Key, o opts, name string) string {
	v := strings.TrimSpace(strings.ReplaceAll(k.Value, "\n", " "))
	on := func(def bool) bool { return o.boolean(k.Name, def) }
	switch k.Name {
	case "command":
		return commandMeaning(v)
	case "process_name":
		n := max(o.integer("numprocs", 1), 1)
		if n > 1 {
			return "How each copy is named: " + copyNames(name, v, o.integer("numprocs_start", 0), n) + "."
		}
		return "The process is named " + config.Expand(v, map[string]string{"program_name": name, "group_name": name, "process_num": "0"}) + "."
	case "numprocs":
		if n := o.integer("numprocs", 1); n > 1 {
			if queueWork.MatchString(o.str("command")) || horizon.MatchString(o.str("command")) {
				return fmt.Sprintf("Runs %d copies side by side, so %d jobs can be handled at once.", n, n)
			}
			return fmt.Sprintf("Runs %d copies side by side.", n)
		}
		return "Runs one copy."
	case "numprocs_start":
		return "Copy numbers start at " + v + "."
	case "priority":
		return "Start order: lower numbers start first and stop last (the default is 999)."
	case "autostart":
		if on(true) {
			return "Starts when supervisord starts."
		}
		return "Doesn’t start with supervisord. It runs only when you start it."
	case "startsecs":
		return "It has to stay up " + seconds(o.integer("startsecs", 1)) + " to count as started; exiting sooner is a failed start."
	case "startretries":
		return fmt.Sprintf("If it fails to start, supervisord tries %d more times, then gives up (FATAL).", o.integer("startretries", 3))
	case "autorestart":
		switch strings.ToLower(v) {
		case "true", "yes", "on", "1":
			return "Restarts it whenever it stops, even after a clean exit."
		case "false", "no", "off", "0":
			return "Never restarts it after it stops."
		}
		return "Restarts it only if it exits with a code that isn’t in exitcodes."
	case "exitcodes":
		return "Exit codes that count as a normal exit: " + strings.ReplaceAll(v, ",", ", ") + "."
	case "stopsignal":
		return "Stops it with the " + strings.ToUpper(strings.TrimPrefix(strings.ToUpper(v), "SIG")) + " signal (the default is TERM)."
	case "stopwaitsecs":
		return "When stopping, waits up to " + waitTime(o.integer("stopwaitsecs", 10)) + " for it to finish before killing it."
	case "stopasgroup":
		if on(false) {
			return "When you stop it, the stop signal also goes to everything it started, so nothing is left behind."
		}
		return "Stopping it signals only the main process; anything it started may keep running."
	case "killasgroup":
		if on(false) {
			return "If it has to be killed, its child processes are killed too."
		}
		return "A forced kill reaches only the main process."
	case "user":
		if v == "root" {
			return "Runs as root, with full access to the Mac."
		}
		return "Runs as user " + v + "."
	case "directory":
		return "Runs it inside " + v + "."
	case "umask":
		return "Files it creates get the permission mask " + v + "."
	case "environment":
		var names []string
		for _, pair := range strings.Split(v, ",") {
			if key, _, ok := strings.Cut(pair, "="); ok && strings.TrimSpace(key) != "" {
				names = append(names, strings.TrimSpace(key))
			}
		}
		if len(names) == 0 {
			return "Sets environment variables for it."
		}
		return "Sets environment variables for it: " + strings.Join(names, ", ") + "."
	case "redirect_stderr":
		if on(false) {
			return "Errors go into the same file as its output."
		}
		return "Errors are kept apart from its output."
	case "serverurl":
		return "Where it reaches supervisord, for programs that talk to supervisord themselves."
	case "stdout_logfile", "stderr_logfile":
		what := "output"
		if k.Name == "stderr_logfile" {
			what = "errors"
		}
		switch {
		case isAuto(v):
			return "Its " + what + " goes to a file supervisord names itself, in its log folder."
		case isNone(v):
			return "Its " + what + " is thrown away."
		case strings.EqualFold(v, "syslog"):
			return "Its " + what + " goes to the system log."
		}
		if k.Name == "stdout_logfile" && o.boolean("redirect_stderr", false) {
			return "Its output and errors go to this file. supervisord writes it, and the Logs tab reads it."
		}
		return "Its " + what + " goes to this file. supervisord writes it, and the Logs tab reads it."
	case "stdout_logfile_maxbytes", "stderr_logfile_maxbytes":
		if o.zeroBytes(k.Name) {
			return "The log is never rotated, so it keeps growing."
		}
		return "The log is rotated when it reaches " + v + " (the default is 50MB)."
	case "stdout_logfile_backups", "stderr_logfile_backups":
		if v == "0" {
			return "Keeps no old copies when the log is rotated."
		}
		return "Keeps " + v + " old copies of the log when it is rotated."
	case "stdout_capture_maxbytes", "stderr_capture_maxbytes":
		return "How much output to capture for event listeners in capture mode."
	case "stdout_events_enabled", "stderr_events_enabled":
		return "Whether writing to this stream sends an event to event listeners."
	case "stdout_syslog", "stderr_syslog":
		if on(false) {
			return "Also sends this stream to the system log."
		}
		return "Doesn’t send this stream to the system log."
	}
	return "supervisord doesn’t know this setting, so it ignores it."
}

func commandMeaning(cmd string) string {
	what := purpose(cmd)
	if what == "" {
		return "What it runs: nothing yet."
	}
	m := "What it runs: " + what + "."
	if queueWork.MatchString(cmd) {
		if q := queueFlag.FindStringSubmatch(cmd); q != nil {
			if qs := strings.Split(q[1], ","); len(qs) > 1 {
				m += " It takes jobs from the " + joinAnd(qs) + " queues, in that order."
			} else {
				m += " It takes jobs from the " + q[1] + " queue."
			}
		}
		if t := triesFlag.FindStringSubmatch(cmd); t != nil {
			m += " A failed job is tried up to " + t[1] + " times."
		}
		if t := maxTime.FindStringSubmatch(cmd); t != nil {
			n, _ := strconv.Atoi(t[1])
			m += " Each worker exits after " + waitTime(n) + ", and supervisord starts a fresh one."
		}
	}
	return m
}

// waitTime is seconds in the largest unit that fits: "10 seconds",
// "1 hour", "90 seconds".
func waitTime(n int) string {
	switch {
	case n >= 3600 && n%3600 == 0:
		if n == 3600 {
			return "1 hour"
		}
		return strconv.Itoa(n/3600) + " hours"
	case n >= 60 && n%60 == 0:
		if n == 60 {
			return "1 minute"
		}
		return strconv.Itoa(n/60) + " minutes"
	}
	return seconds(n)
}

// unsetRow lists the settings a program leaves out that change what it
// does, with the defaults that apply, and suggests log files when its
// output or errors go to files supervisord names itself.
func unsetRow(s config.Section, name, logDir string) (Line, bool) {
	o := opts{s}
	has := func(k string) bool { _, ok := s.Get(k); return ok }
	redirect := o.boolean("redirect_stderr", false)
	var unset, defaults []string
	if !has("autostart") {
		unset = append(unset, "autostart")
		defaults = append(defaults, "it starts with supervisord")
	}
	if !has("autorestart") {
		unset = append(unset, "autorestart")
		defaults = append(defaults, "it’s restarted only if it exits with an unexpected code")
	}
	outAuto, errAuto := isAuto(o.str("stdout_logfile")), !redirect && isAuto(o.str("stderr_logfile"))
	if !has("stdout_logfile") {
		unset = append(unset, "stdout_logfile")
	}
	if !redirect && !has("stderr_logfile") {
		unset = append(unset, "stderr_logfile")
	}
	switch {
	case !has("stdout_logfile") && !redirect && !has("stderr_logfile"):
		defaults = append(defaults, "its output and errors go to files supervisord names itself")
	case !has("stdout_logfile") && redirect:
		defaults = append(defaults, "its output and errors go to a file supervisord names itself")
	case !has("stdout_logfile"):
		defaults = append(defaults, "its output goes to a file supervisord names itself")
	case !redirect && !has("stderr_logfile"):
		defaults = append(defaults, "its errors go to a file supervisord names itself")
	}
	if len(unset) == 0 {
		return Line{}, false
	}
	l := Line{Line: lastLine(s), Text: "not set: " + strings.Join(unset, ", "), Kind: KindUnset, Unset: unset,
		Meaning: "Defaults apply: " + joinAnd(defaults) + "."}

	// Suggest real log files for streams that go to auto-named ones.
	dir := logDir
	for _, k := range []string{"stdout_logfile", "stderr_logfile"} {
		if v := o.str(k); !isAuto(v) && !isNone(v) && filepath.IsAbs(v) && !strings.Contains(v, "%(") {
			dir = filepath.Dir(v)
			break
		}
	}
	switch {
	case outAuto && errAuto:
		l.Suggest = "Files supervisord names itself are hard to find and are deleted when supervisord restarts. Set both log paths."
	case outAuto:
		l.Suggest = "Its output goes to a file supervisord names itself. It’s hard to find and is deleted when supervisord restarts."
	case errAuto:
		l.Suggest = "Its errors go to a file supervisord names itself. It’s hard to find and is deleted when supervisord restarts."
	}
	if dir != "" {
		if outAuto && !has("stdout_logfile") {
			l.Add = append(l.Add, "stdout_logfile="+filepath.Join(dir, name+".log"))
		}
		if errAuto && !has("stderr_logfile") {
			l.Add = append(l.Add, "stderr_logfile="+filepath.Join(dir, name+".err.log"))
		}
	}
	return l, true
}

func lastLine(s config.Section) int {
	n := s.Line
	for _, k := range s.Keys {
		n = max(n, k.Line+strings.Count(k.Value, "\n"))
	}
	return n
}

func joinAnd(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	}
	return strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
}
