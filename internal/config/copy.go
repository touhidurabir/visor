package config

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Rename is one name a copy changed. Kind is "program" or "group".
type Rename struct {
	Kind, From, To string
}

// CopyDoubt is a setting of a copy that may not do what the person wants,
// because it is kept as written: a value naming the original program, a log
// file the two would share, a group member defined in another file, or a
// section CopyAs does not rename. Line is the line in the copy (copies keep
// line numbers), or 0 for the file name; Section is the copy's section name,
// e.g. "program:mailer-copy"; Name is the name the setting mentions (the
// program, for a shared log; the file, for the file name).
type CopyDoubt struct {
	Line               int
	Section, Key, Name string
	Message            string
}

// Copy is a config rewritten so it can sit next to the original: a new file
// name, new program and group names, and everything else as it was.
type Copy struct {
	FileName, Content string
	Renames           []Rename
	Doubts            []CopyDoubt
}

// otherSectionKinds also make supervisord process groups, but CopyAs does
// not rename them (Visor does not list them): it keeps them and says so.
var otherSectionKinds = []string{"fcgi-program:", "eventlistener:"}

// CopyAs copies the config fileName (its content) under names that are free.
// Each [program:x] and [group:g] becomes the first of x-copy, x-copy-2, …
// that is not in taken (the program and group names of the include files)
// and not already given to another section of the copy; a group's
// programs= list follows its renamed programs. supervisord keeps programs
// without a group and groups in one process-group namespace, so one taken
// set covers both. The file name gets the same suffix before its extension
// and is not in takenFiles (compared ignoring case, as macOS does). When
// patterns (the include patterns' file names, e.g. "*-svc.ini") are given,
// it also matches one of them, so supervisord loads the copy: the suffix
// moves in front of the part the pattern needs (web-copy-svc.ini) if it
// has to, and a name that can't match any is a doubt.
//
// Comments, key order, other settings and spacing are kept line for line.
// Values that still name the original (other than %(program_name)s, which
// supervisord expands for the copy) are kept too and listed in Doubts, as
// are log files the copy would share with the original.
func CopyAs(fileName, content string, taken, takenFiles, patterns []string) (Copy, error) {
	f, err := ParseINI(strings.NewReader(content), fileName)
	if err != nil {
		return Copy{}, fmt.Errorf("can't copy it: %w", err)
	}
	progs := f.SectionsWithPrefix("program:")
	if len(progs) == 0 {
		return Copy{}, fmt.Errorf("can't copy %s: it has no [program:name] section", fileName)
	}

	used := map[string]bool{} // taken names, and the original's own
	for _, n := range taken {
		used[n] = true
	}
	for _, s := range f.Sections {
		if _, n, ok := strings.Cut(s.Name, ":"); ok {
			used[strings.TrimSpace(n)] = true
		}
	}
	free := func(base string, ok func(string) bool) string {
		for i := 1; ; i++ {
			c := base + "-copy"
			if i > 1 {
				c += "-" + strconv.Itoa(i)
			}
			if ok(c) {
				return c
			}
		}
	}

	var c Copy
	sections := map[string]string{} // old section name -> new
	progNew := map[string]string{}  // old program name -> new
	chosen := map[string]string{}   // new name -> kind that took it
	for _, s := range progs {
		old := strings.TrimSpace(strings.TrimPrefix(s.Name, "program:"))
		if n, done := progNew[old]; done {
			// [program:web ] is another section to ParseINI, but the same
			// program to supervisord: its header gets the same new name.
			sections[s.Name] = "program:" + n
			continue
		}
		n := free(old, func(c string) bool { return !used[c] && chosen[c] == "" })
		progNew[old], chosen[n] = n, "program"
		sections[s.Name] = "program:" + n
		c.Renames = append(c.Renames, Rename{Kind: "program", From: old, To: n})
	}
	for _, s := range f.SectionsWithPrefix("group:") {
		old := strings.TrimSpace(strings.TrimPrefix(s.Name, "group:"))
		members := listOf(s)
		// A program in a group is not a process group itself, so the group
		// may share a name with one of its own programs, as the original may.
		mine := func(c string) bool {
			return slices.ContainsFunc(members, func(m string) bool { return progNew[m] == c })
		}
		n := free(old, func(c string) bool {
			return !used[c] && (chosen[c] == "" || chosen[c] == "program" && mine(c))
		})
		chosen[n] = "group"
		sections[s.Name] = "group:" + n
		c.Renames = append(c.Renames, Rename{Kind: "group", From: old, To: n})
		line, _ := keyLine(s, "programs")
		for _, m := range members {
			if _, here := progNew[m]; !here {
				c.Doubts = append(c.Doubts, CopyDoubt{
					Line: line, Section: "group:" + n, Key: "programs", Name: m,
					Message: fmt.Sprintf("programs= lists %s, which another file defines, so the copied group would run it a second time", m),
				})
			}
		}
	}

	c.Content = rewriteSections(content, sections, progNew)
	c.Doubts = append(c.Doubts, mentionDoubts(f, sections, c.Renames)...)
	for _, s := range f.Sections {
		for _, k := range otherSectionKinds {
			if strings.HasPrefix(s.Name, k) {
				c.Doubts = append(c.Doubts, CopyDoubt{
					Line: s.Line, Section: s.Name, Name: strings.TrimSpace(strings.TrimPrefix(s.Name, k)),
					Message: fmt.Sprintf("[%s] is copied with its name unchanged, so supervisord would see it twice. Rename it", s.Name),
				})
			}
		}
	}
	var fits bool
	c.FileName, fits = copyFileName(filepath.Base(fileName), takenFiles, patterns)
	if !fits {
		c.Doubts = append(c.Doubts, CopyDoubt{
			Name: c.FileName,
			Message: fmt.Sprintf("%s does not match the include setting (%s), so supervisord would not load it. Rename the file so it matches",
				c.FileName, strings.Join(patterns, ", ")),
		})
	}
	slices.SortStableFunc(c.Doubts, func(a, b CopyDoubt) int { return a.Line - b.Line })
	return c, nil
}

// copyFileName is base with -copy (-copy-2, …) before its extension, not in
// takenFiles (ignoring case) and matching one of patterns, if any. When the
// end of the name can't take the suffix (web-svc-copy.ini is not *-svc.ini),
// it goes before each '-', '_' or '.' of the name in turn, from the right
// (web-copy-svc.ini). fits is false when no such name matches: name is then
// the plain -copy one.
func copyFileName(base string, takenFiles, patterns []string) (name string, fits bool) {
	used := map[string]bool{strings.ToLower(base): true}
	for _, n := range takenFiles {
		used[strings.ToLower(n)] = true
	}
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	matches := func(n string) bool {
		if len(patterns) == 0 {
			return true
		}
		return slices.ContainsFunc(patterns, func(p string) bool {
			ok, _ := filepath.Match(p, n)
			return ok
		})
	}
	// Where the suffix may go: the end of the stem, then before each
	// separator, right to left.
	points := []int{len(stem)}
	for i := len(stem) - 1; i > 0; i-- {
		if strings.IndexByte("-_.", stem[i]) >= 0 {
			points = append(points, i)
		}
	}
	// Per point, a free name is among the first len(takenFiles)+1 suffixes.
	try := func(at int, match func(string) bool) string {
		for i := 1; i <= len(takenFiles)+1; i++ {
			n := stem[:at] + "-copy"
			if i > 1 {
				n += "-" + strconv.Itoa(i)
			}
			n += stem[at:] + ext
			if !used[strings.ToLower(n)] && match(n) {
				return n
			}
		}
		return ""
	}
	for _, at := range points {
		if n := try(at, matches); n != "" {
			return n, true
		}
	}
	return try(len(stem), func(string) bool { return true }), false
}

// listOf is a group's programs= list, as supervisord's list_of_strings
// splits it (on commas).
func listOf(s Section) []string {
	v, _ := s.Get("programs")
	var out []string
	for _, n := range strings.Split(v, ",") {
		if n = strings.TrimSpace(n); n != "" {
			out = append(out, n)
		}
	}
	return out
}

func keyLine(s Section, key string) (int, bool) {
	for _, k := range s.Keys {
		if k.Name == key {
			return k.Line, true
		}
	}
	return s.Line, false
}

// rewriteSections renames section headers (sections: old name -> new) and,
// in a renamed [group:], the programs it lists (progNew), line by line as
// ParseINI reads them, so every other byte stays as it was.
func rewriteSections(content string, sections, progNew map[string]string) string {
	lines := strings.SplitAfter(content, "\n")
	inGroup, inList := false, false
	for i, line := range lines {
		body := strings.TrimRight(line, "\r\n")
		end := line[len(body):]
		trimmed := strings.TrimSpace(body)
		switch {
		case trimmed == "" || trimmed[0] == ';' || trimmed[0] == '#':
			continue // a comment does not end a value, as in ParseINI
		case body[0] == ' ' || body[0] == '\t':
			if inList {
				lines[i] = renameList(body, progNew) + end
			}
			continue
		case trimmed[0] == '[':
			head := strings.TrimSpace(stripInlineComment(trimmed))
			name := strings.TrimSuffix(strings.TrimPrefix(head, "["), "]")
			inGroup, inList = false, false
			if n, ok := sections[name]; ok && strings.HasSuffix(head, "]") {
				lines[i] = "[" + n + "]" + body[len(head):] + end
				inGroup = strings.HasPrefix(name, "group:")
			}
			continue
		}
		j := strings.IndexAny(body, "=:")
		if j < 0 {
			continue
		}
		inList = inGroup && strings.ToLower(strings.TrimSpace(body[:j])) == "programs"
		if inList {
			lines[i] = body[:j+1] + renameList(body[j+1:], progNew) + end
		}
	}
	return strings.Join(lines, "")
}

// renameList renames the comma-separated names of s that progNew has,
// keeping the spacing around each and any inline comment.
func renameList(s string, progNew map[string]string) string {
	val := stripInlineComment(s)
	parts := strings.Split(val, ",")
	for i, p := range parts {
		t := strings.TrimSpace(p)
		if n, ok := progNew[t]; ok && t != "" {
			at := strings.Index(p, t)
			parts[i] = p[:at] + n + p[at+len(t):]
		}
	}
	return strings.Join(parts, ",") + s[len(val):]
}

// expansionRe matches %(name)s-style expansions and "%%", which supervisord
// works out per program, so they never count as naming the original.
var expansionRe = regexp.MustCompile(`%%|%\(\w+\)[0-9]*[sd]`)

// mentionDoubts lists the settings of the copied programs whose value names
// an original program or group as a whole word, and the log files the copy
// would share with the original.
func mentionDoubts(f File, sections map[string]string, renames []Rename) []CopyDoubt {
	var out []CopyDoubt
	for _, s := range f.SectionsWithPrefix("program:") {
		prog := strings.TrimSpace(strings.TrimPrefix(s.Name, "program:"))
		for _, k := range s.Keys {
			if k.Name == "stdout_logfile" || k.Name == "stderr_logfile" {
				if sharedLog(k.Value) {
					out = append(out, CopyDoubt{Line: k.Line, Section: sections[s.Name], Key: k.Name, Name: prog,
						Message: fmt.Sprintf("%s is a fixed path, so the copy and %s would write to the same log file. Put %%(program_name)s in it, or name another file", k.Name, prog)})
				}
				continue
			}
			v := expansionRe.ReplaceAllString(k.Value, " ")
			for _, r := range renames {
				if !mentions(v, r.From) {
					continue
				}
				out = append(out, CopyDoubt{Line: k.Line, Section: sections[s.Name], Key: k.Name, Name: r.From,
					Message: fmt.Sprintf("%s still mentions %s: the copy keeps it as written", k.Name, r.From)})
				break
			}
		}
	}
	return out
}

// sharedLog reports whether a stdout_logfile or stderr_logfile value is the
// same file for the copy as for the original: a path, not AUTO, NONE or
// syslog (as the checks in internal/validate read them), with nothing
// supervisord works out per program. %(process_num)s does not count: the
// copy's processes are numbered as the original's are.
func sharedLog(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" || strings.EqualFold(v, "AUTO") || strings.EqualFold(v, "NONE") || strings.EqualFold(v, "syslog") {
		return false
	}
	return !programExpansionRe.MatchString(v)
}

// programExpansionRe matches the expansions that differ between a program
// and its copy.
var programExpansionRe = regexp.MustCompile(`%\((program_name|group_name)\)[0-9]*s`)

// mentions reports whether name appears in v with no name character
// (letter, digit, '_' or '-') right before or after it.
func mentions(v, name string) bool {
	if name == "" {
		return false
	}
	nameChar := func(b byte) bool {
		return b == '_' || b == '-' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
	}
	for from := 0; ; {
		i := strings.Index(v[from:], name)
		if i < 0 {
			return false
		}
		i += from
		j := i + len(name)
		if (i == 0 || !nameChar(v[i-1])) && (j == len(v) || !nameChar(v[j])) {
			return true
		}
		from = i + 1
	}
}
