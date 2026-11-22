// Package validate checks a program config file before Visor saves it. It is
// shared by the form, the raw editor and the Config tab.
package validate

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/touhidurabir/visor/internal/config"
	"github.com/touhidurabir/visor/internal/platform"
)

// Level is how serious a Check is. Errors block saving; warnings do not.
type Level string

const (
	OK      Level = "ok"
	Warning Level = "warning"
	Error   Level = "error"
)

// Check is one finding. Line is 1-based, 0 when it is not about one line.
// A problem (warning or error) has Fix, a sentence on how to fix it, and
// Edit when the fix is one exact change to the file.
type Check struct {
	Level                 Level
	Line                  int
	Key, Program, Message string
	Fix                   string
	Edit                  *Edit
}

// Edit is one change to the file: Text replaces line Line, or with Insert
// goes in as a new line after it.
type Edit struct {
	Line   int
	Insert bool
	Text   string
	// Rename, when set, renames only the key on line Line, keeping its
	// value, inline comment and continuation lines; Text then shows the
	// result.
	Rename string
}

// with sets how to fix c.
func (c *Check) with(fix string, e *Edit) {
	c.Fix, c.Edit = fix, e
}

// Env is everything a check needs from the machine, so tests can fake it.
// A nil function falls back to the real thing.
type Env struct {
	Inventory    config.Inventory
	IncludeGlobs []string
	SelfPath     string // the file being edited; "" for a new one
	LookPath     func(string) (string, error)
	UserExists   func(string) bool
	Stat         func(string) (fs.FileInfo, error)
	Writable     func(dir string) bool
	// Remote: the file is on a server; the functions above look there, and
	// the fixes say "on the server" instead of "in Terminal".
	Remote bool
	// SkipWritable: whether supervisord could write a folder is unknown
	// (on a server, Visor's helper doesn't run as root), so log folders are
	// only checked to exist.
	SkipWritable bool
}

// where is the machine, and where to run a command on it, for the messages.
func (e Env) where() (machine, in string) {
	if e.Remote {
		return "the server", "on the server"
	}
	return "this Mac", "in Terminal"
}

func (e Env) withDefaults() Env {
	if e.LookPath == nil {
		path := platform.LoginPATH()
		e.LookPath = func(b string) (string, error) { return platform.LookPath(b, path) }
	}
	if e.UserExists == nil {
		e.UserExists = userExists
	}
	if e.Stat == nil {
		e.Stat = os.Stat
	}
	if e.Writable == nil {
		e.Writable = dirWritable
	}
	return e
}

// userExists is name_to_uid in supervisord: a user name, or a uid that
// has a user.
func userExists(u string) bool {
	if n, ok := pyInt(u); ok {
		_, err := user.LookupId(strconv.FormatInt(n, 10))
		return err == nil
	}
	_, err := user.Lookup(u)
	return err == nil
}

func dirWritable(dir string) bool {
	f, err := os.CreateTemp(dir, ".visor-write-check-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}

var lineRe = regexp.MustCompile(`:(\d+): `)

// coreSections are supervisord's own settings. Include files are read into
// the same parser as supervisord.conf, so one of these in an include file
// would override the main config (brief §5: Visor never edits them).
var coreSections = []string{"supervisord", "unix_http_server", "inet_http_server", "supervisorctl", "include"}

func isCoreSection(name string) bool {
	return slices.Contains(coreSections, name) || strings.HasPrefix(name, "rpcinterface:")
}

// File checks content as the file named fileName. Results are sorted errors
// first, then warnings, then ok lines; order within a level follows the file.
func File(fileName, content string, env Env) []Check {
	env = env.withDefaults()
	var cs []Check
	add := func(l Level, line int, prog, key, msg string, args ...any) *Check {
		cs = append(cs, Check{Level: l, Line: line, Program: prog, Key: key, Message: fmt.Sprintf(msg, args...)})
		return &cs[len(cs)-1]
	}

	if len(env.IncludeGlobs) > 0 {
		base := filepath.Base(fileName)
		inside := false
		for _, g := range env.IncludeGlobs {
			if ok, _ := filepath.Match(filepath.Base(g), base); ok {
				inside = true
				break
			}
		}
		if !inside {
			add(Error, 0, "", "", "%s does not match the include setting (%s), so supervisord will not load it", base, strings.Join(env.IncludeGlobs, ", ")).
				with("Name the file so it matches "+strings.Join(env.IncludeGlobs, " or ")+".", nil)
		}
	}

	f, err := config.ParseINI(strings.NewReader(content), fileName)
	if err != nil {
		line := 0
		if m := lineRe.FindStringSubmatch(err.Error()); m != nil {
			line, _ = strconv.Atoi(m[1])
		}
		msg := strings.TrimSpace(lineRe.ReplaceAllString(strings.TrimPrefix(err.Error(), fileName), ""))
		add(Error, line, "", "", "%s", strings.TrimPrefix(msg, ": ")).
			with("Fix this line so the file can be read; nothing else can be checked until then.", nil)
		return sorted(cs)
	}

	for _, sec := range f.Sections {
		if isCoreSection(sec.Name) {
			add(Error, sec.Line, "", "", "[%s] belongs in supervisord.conf, not in an include file: Visor doesn't edit supervisord's own settings. Remove this section", sec.Name).
				with("Remove this section and its settings from this file.", nil)
		}
	}

	progs := f.SectionsWithPrefix("program:")
	if len(progs) == 0 {
		add(Error, 0, "", "", "Add a [program:name] section").
			with("Start the file with a line like [program:my-worker], then a command= line.", nil)
	}
	for _, s := range progs {
		checkProgram(s, env, add)
	}
	return sorted(cs)
}

type adder func(l Level, line int, prog, key, msg string, args ...any) *Check

func checkProgram(s config.Section, env Env, add adder) {
	name := strings.TrimPrefix(s.Name, "program:")
	val := func(k string) (string, int, bool) {
		for _, key := range s.Keys {
			if key.Name == k {
				return key.Value, key.Line, true
			}
		}
		return "", 0, false
	}
	e := func(line int, key, msg string, args ...any) *Check { return add(Error, line, name, key, msg, args...) }
	w := func(line int, key, msg string, args ...any) *Check {
		return add(Warning, line, name, key, msg, args...)
	}
	ok := func(key, msg string, args ...any) { add(OK, 0, name, key, msg, args...) }

	// Name uniqueness across the inventory, except the file being edited.
	dupIn := ""
	for _, cf := range env.Inventory.Files {
		if env.SelfPath != "" && cf.Path == env.SelfPath {
			continue
		}
		for _, p := range cf.Programs {
			if p.Name == name {
				dupIn = filepath.Base(cf.Path)
			}
		}
	}
	if dupIn != "" {
		e(s.Line, "", "[program:%s] already exists in %s", name, dupIn).
			with("Give this program another name, e.g. [program:"+name+"-2]. supervisord needs every program name to be different.", nil)
	} else {
		ok("", "Name %s is unique", name)
	}

	// Command.
	dir, _, hasDir := val("directory")
	if cmd, line, has := val("command"); !has || strings.TrimSpace(cmd) == "" {
		e(s.Line, "command", "%s has no command", name).
			with("Add a command= line with the full command to run.", nil)
	} else {
		checkCommand(cmd, line, dir, hasDir, env, e, w, ok)
	}

	// Unknown keys and value types.
	for _, k := range s.Keys {
		if !isKnown(k.Name) {
			msg := "unknown key " + k.Name
			fix, edit := "supervisord ignores settings it doesn't know. Check the spelling, or remove the line.", (*Edit)(nil)
			if best, d := closest(k.Name); d <= 2 {
				msg += ". Did you mean " + best + "?"
				fix = "Rename it to " + best + "."
				first, _, more := strings.Cut(k.Value, "\n")
				if more {
					first += " …"
				}
				edit = &Edit{Line: k.Line, Text: best + "=" + first, Rename: best}
			}
			w(k.Line, k.Name, "%s", msg).with(fix, edit)
			continue
		}
		checkValue(k, e)
	}
	if v, line, has := val("process_name"); has && !isProcessName(v) {
		e(line, "process_name", "process_name can't contain a space, colon or slash").
			with("Use letters, numbers, dashes and underscores, e.g. process_name=%(program_name)s.", nil)
	}
	if !isProcessName(name) {
		e(s.Line, "", "[program:%s]: a program name can't contain a space, colon or slash", name).
			with("Use letters, numbers, dashes and underscores in the name.", nil)
	}
	if sg, _, has := val("stopasgroup"); has && truthy[strings.ToLower(sg)] {
		if kg, line, has := val("killasgroup"); has && falsy[strings.ToLower(kg)] {
			e(line, "killasgroup", "killasgroup can't be false when stopasgroup is true").
				with("Set killasgroup=true, or remove the line: stopasgroup=true already turns it on.", &Edit{Line: line, Text: "killasgroup=true"})
		}
	}

	// Copies.
	if v, line, has := val("numprocs"); has {
		if n, ok := pyInt(v); ok && n > 1 {
			if pn, pline, hasName := val("process_name"); !strings.Contains(pn, "%(process_num)") {
				const named = "process_name=%(program_name)s_%(process_num)02d"
				fix := fmt.Sprintf("Every copy needs its own name: add the copy number. %s gives %s.", named, copyNames(name, int(n)))
				if hasName {
					e(pline, "process_name", "numprocs=%d needs %%(process_num) in process_name, e.g. %%(program_name)s_%%(process_num)02d", n).
						with(fix, &Edit{Line: pline, Text: named})
				} else {
					e(line, "numprocs", "numprocs=%d needs %%(process_num) in process_name, e.g. %%(program_name)s_%%(process_num)02d", n).
						with(fix, &Edit{Line: line, Insert: true, Text: named})
				}
			}
		}
	}

	// Directory.
	if hasDir && !strings.Contains(dir, "%(") {
		_, line, _ := val("directory")
		switch {
		case !filepath.IsAbs(dir):
			e(line, "directory", "directory must be an absolute path").
				with("Give the full path, starting with /.", nil)
		default:
			if fi, err := env.Stat(dir); err != nil {
				w(line, "directory", "%s does not exist yet", dir).
					with("It won't start until that folder exists. Point it at your project folder, or create the folder.", nil)
			} else if !fi.IsDir() {
				e(line, "directory", "%s is not a folder", dir).
					with("Point it at the folder the program should run in.", nil)
			} else {
				ok("directory", "%s exists", dir)
			}
		}
	}

	// User.
	if u, line, has := val("user"); has && strings.TrimSpace(u) != "" && !strings.Contains(u, "%(") {
		u = strings.TrimSpace(u)
		switch {
		case u == "root":
			w(line, "user", "%s runs as root", name).
				with("Run it as a normal user unless it really needs root: anything it does, it does with full access.", nil)
		case !env.UserExists(u):
			machine, _ := env.where()
			e(line, "user", "user %s does not exist on %s, so supervisord would refuse this config", u, machine).
				with("Use a user that exists on "+machine+", or remove the line to run it as the user supervisord runs as.", nil)
		}
	}

	// Log folders.
	seen := map[string]bool{}
	for _, key := range []string{"stdout_logfile", "stderr_logfile"} {
		v, line, has := val(key)
		v = strings.TrimSpace(v)
		if !has || v == "" || strings.EqualFold(v, "AUTO") || strings.EqualFold(v, "NONE") || strings.EqualFold(v, "syslog") {
			continue
		}
		d := filepath.Dir(v)
		if !filepath.IsAbs(v) || strings.Contains(d, "%(") {
			continue
		}
		if fi, err := env.Stat(d); err != nil || !fi.IsDir() {
			_, in := env.where()
			mkdir := "mkdir -p "
			if env.Remote {
				mkdir = "sudo mkdir -p "
			}
			e(line, key, "log folder %s does not exist", d).
				with("Create it ("+in+": "+mkdir+d+"), or point the log at a folder that exists.", nil)
		} else if env.SkipWritable {
			if !seen[d] {
				seen[d] = true
				ok(key, "log folder %s exists", d)
			}
		} else if !env.Writable(d) {
			e(line, key, "log folder %s is not writable", d).
				with("Point the log at a folder you can write to, or change that folder's permissions.", nil)
		} else if !seen[d] {
			seen[d] = true
			ok(key, "log folder %s is writable", d)
		}
	}

	// Environment: supervisord expands %(...)s first, then reads the pairs.
	if v, line, has := val("environment"); has {
		pairs, err := envPairs(expandForCheck(v))
		switch {
		case errors.Is(err, errNoClosingQuote):
			e(line, "environment", "environment has a quote that is not closed").
				with("Close the quote around that value.", nil)
		case err != nil:
			e(line, "environment", `environment must look like KEY="value",OTHER="value": put values with spaces or symbols such as @ or = in quotes`).
				with(`Write it as KEY="value",OTHER="value".`, nil)
		}
		for _, p := range pairs {
			if p.Dropped != "" {
				w(line, "environment", `supervisord sets %s to "%s" and drops the "%s" after it. Put the value in quotes`, p.Key, p.Value, p.Dropped).
					with(fmt.Sprintf(`Write %s="%s%s".`, p.Key, p.Value, p.Dropped), nil)
			}
		}
	}
}

// expandRe matches what Python's % formatting replaces in a config value.
var expandRe = regexp.MustCompile(`%%|%\([^)]*\)[#0 +-]*[0-9]*(\.[0-9]+)?[diouxXeEfFgGcrsa]`)

// expandForCheck stands in for supervisord's expansion of a value: "%%" is
// "%", and each %(name)s a word (its real value is not known here).
func expandForCheck(v string) string {
	return expandRe.ReplaceAllStringFunc(v, func(m string) string {
		if m == "%%" {
			return "%"
		}
		return "x"
	})
}

// checkValue checks a known key's value as supervisord converts it
// (datatypes.go). A value with %(...)s is expanded by supervisord first,
// so it is not checked.
func checkValue(k config.Key, e func(int, string, string, ...any) *Check) {
	v := strings.TrimSpace(k.Value)
	if strings.Contains(v, "%(") {
		return
	}
	switch typeOf(k.Name) {
	case typeInt:
		if _, ok := pyInt(v); !ok {
			e(k.Line, k.Name, "%s must be a whole number", k.Name).with("Use a whole number such as 10.", nil)
		}
	case typeBytes:
		if !isByteSize(v) {
			e(k.Line, k.Name, "%s must be a number of bytes, optionally ending in KB, MB or GB", k.Name).with("Use a size such as 50MB.", nil)
		}
	case typeBool:
		if !isBoolean(v) {
			e(k.Line, k.Name, "%s must be true or false", k.Name).with("Use true or false.", nil)
		}
	}
	switch k.Name {
	case "autorestart":
		if !isAutoRestart(v) {
			e(k.Line, k.Name, "autorestart must be true, false or unexpected").
				with("Use true (always restart), false (never) or unexpected (only after an unexpected exit code).", nil)
		}
	case "stopsignal":
		if !isSignal(v) {
			e(k.Line, k.Name, "stopsignal must be a signal name such as TERM, INT, QUIT, HUP, KILL or USR1, or a signal number").
				with("Use TERM unless the program asks for another signal.", nil)
		}
	case "exitcodes":
		if !isExitCodes(v) {
			e(k.Line, k.Name, "exitcodes must be exit codes from 0 to 255, separated by commas").with("Write them like 0,2.", nil)
		}
	case "umask":
		if !isOctal(v) {
			e(k.Line, k.Name, "umask must be an octal number such as 022").with("Use an octal number such as 022.", nil)
		}
	}
}

func checkCommand(cmd string, line int, dir string, hasDir bool, env Env,
	e, w func(int, string, string, ...any) *Check, ok func(string, string, ...any)) {
	bin := firstWord(cmd)
	if bin == "" || strings.Contains(bin, "%(") {
		return
	}
	_, in := env.where()
	if strings.Contains(bin, "/") && !filepath.IsAbs(bin) {
		if !hasDir || !filepath.IsAbs(dir) {
			return
		}
		bin = filepath.Join(dir, bin)
	}
	if filepath.IsAbs(bin) {
		if _, err := env.Stat(bin); err != nil {
			w(line, "command", "%s does not exist yet", bin).
				with(fmt.Sprintf("Use the full path to a program that exists and can run. To find one, run which %s %s.", filepath.Base(bin), in), nil)
		} else if _, err := env.LookPath(bin); err != nil {
			e(line, "command", "%s is not executable. Run chmod +x on it", bin).
				with(strings.ToUpper(in[:1])+in[1:]+", run chmod +x "+bin+" so it can run.", nil)
		} else {
			ok("command", "%s exists and is executable", bin)
		}
		return
	}
	if p, err := env.LookPath(bin); err != nil {
		if env.Remote {
			w(line, "command", "Could not find %s on the server's PATH", bin).
				with(fmt.Sprintf("supervisord searches only the system folders there: use the full path. On the server, which %s shows it.", bin), nil)
			return
		}
		w(line, "command", "Could not find %s on your PATH", bin).
			with(fmt.Sprintf("supervisord may not search the same folders as your Terminal: use the full path. In Terminal, which %s shows it.", bin), nil)
	} else {
		ok("command", "%s found at %s", bin, p)
	}
}

// copyNames lists the names copies get with %(program_name)s_%(process_num)02d:
// "acme_00, acme_01, acme_02", or the first two and the last for more.
func copyNames(prog string, n int) string {
	name := func(i int) string { return fmt.Sprintf("%s_%02d", prog, i) }
	if n <= 3 {
		out := make([]string, n)
		for i := range out {
			out[i] = name(i)
		}
		return strings.Join(out, ", ")
	}
	return name(0) + ", " + name(1) + " … " + name(n-1)
}

// firstWord is the program a command line starts, honouring one level of
// quotes around it.
func firstWord(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return ""
	}
	if q := cmd[0]; q == '"' || q == '\'' {
		if i := strings.IndexByte(cmd[1:], q); i >= 0 {
			return cmd[1 : i+1]
		}
		return cmd[1:]
	}
	if i := strings.IndexFunc(cmd, unicode.IsSpace); i >= 0 {
		return cmd[:i]
	}
	return cmd
}

func sorted(cs []Check) []Check {
	rank := map[Level]int{Error: 0, Warning: 1, OK: 2}
	sort.SliceStable(cs, func(i, j int) bool { return rank[cs[i].Level] < rank[cs[j].Level] })
	return cs
}

// HasErrors reports whether any check is an error (saving is blocked).
func HasErrors(cs []Check) bool {
	for _, c := range cs {
		if c.Level == Error {
			return true
		}
	}
	return false
}
