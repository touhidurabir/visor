// Package config reads supervisord configuration. It is read-only: parsing
// and discovery only, following the rules of Python's ConfigParser as
// supervisord uses it.
package config

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// File is a parsed supervisord-style INI file.
type File struct {
	Path     string
	Sections []Section
}

// Section is one [name] block. Name is as written, e.g. "program:web".
type Section struct {
	Name string
	Line int
	Keys []Key
}

// Key is one option. Name is lower-cased; Value has continuation lines
// joined by "\n" and inline comments removed.
type Key struct {
	Name, Value string
	Line        int
}

// ParseINI parses r. Rules: full-line comments start with ';' or '#'; an
// inline comment starts at a ';' preceded by whitespace; a line with leading
// whitespace continues the previous value; '=' or ':' separates key from
// value; keys are lower-cased; duplicate sections merge and the last value of
// a repeated key wins. path is recorded on the File and used in errors.
func ParseINI(r io.Reader, path string) (File, error) {
	f := File{Path: path}
	cur := -1 // index into f.Sections
	curKey := -1
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for n := 1; sc.Scan(); n++ {
		raw := strings.TrimRight(sc.Text(), "\r")
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || trimmed[0] == ';' || trimmed[0] == '#' {
			continue
		}
		if raw[0] == ' ' || raw[0] == '\t' {
			if cur < 0 || curKey < 0 {
				return f, fmt.Errorf("%s:%d: unexpected indented line", path, n)
			}
			if part := strings.TrimSpace(stripInlineComment(raw)); part != "" {
				k := &f.Sections[cur].Keys[curKey]
				k.Value += "\n" + part
			}
			continue
		}
		if trimmed[0] == '[' {
			head := strings.TrimSpace(stripInlineComment(trimmed))
			if !strings.HasSuffix(head, "]") {
				return f, fmt.Errorf("%s:%d: unterminated section header %q", path, n, trimmed)
			}
			name := head[1 : len(head)-1]
			cur = f.sectionIndex(name)
			if cur < 0 {
				f.Sections = append(f.Sections, Section{Name: name, Line: n})
				cur = len(f.Sections) - 1
			}
			curKey = -1
			continue
		}
		if cur < 0 {
			return f, fmt.Errorf("%s:%d: option outside any section", path, n)
		}
		i := strings.IndexAny(raw, "=:")
		if i < 0 {
			return f, fmt.Errorf("%s:%d: expected key=value, got %q", path, n, trimmed)
		}
		name := strings.ToLower(strings.TrimSpace(raw[:i]))
		val := strings.TrimSpace(stripInlineComment(raw[i+1:]))
		s := &f.Sections[cur]
		curKey = -1
		for j := range s.Keys {
			if s.Keys[j].Name == name {
				s.Keys[j].Value, s.Keys[j].Line = val, n
				curKey = j
				break
			}
		}
		if curKey < 0 {
			s.Keys = append(s.Keys, Key{Name: name, Value: val, Line: n})
			curKey = len(s.Keys) - 1
		}
	}
	if err := sc.Err(); err != nil {
		return f, fmt.Errorf("%s: %w", path, err)
	}
	return f, nil
}

// stripInlineComment cuts at the first ';' that follows whitespace, so
// "sh -c 'a;b'" survives but "run ; note" becomes "run ".
func stripInlineComment(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == ';' && (i == 0 || s[i-1] == ' ' || s[i-1] == '\t') {
			return s[:i]
		}
	}
	return s
}

func (f File) sectionIndex(name string) int {
	for i := range f.Sections {
		if f.Sections[i].Name == name {
			return i
		}
	}
	return -1
}

// Section returns the section with exactly this name.
func (f File) Section(name string) (Section, bool) {
	if i := f.sectionIndex(name); i >= 0 {
		return f.Sections[i], true
	}
	return Section{}, false
}

// SectionsWithPrefix returns sections whose name starts with prefix, such as
// "program:", in file order.
func (f File) SectionsWithPrefix(prefix string) []Section {
	var out []Section
	for _, s := range f.Sections {
		if strings.HasPrefix(s.Name, prefix) {
			out = append(out, s)
		}
	}
	return out
}

// Get returns the value of key (case-insensitive).
func (s Section) Get(key string) (string, bool) {
	key = strings.ToLower(key)
	for _, k := range s.Keys {
		if k.Name == key {
			return k.Value, true
		}
	}
	return "", false
}

var expandRe = regexp.MustCompile(`%%|%\((\w+)\)([0-9]*)([sd])`)

// Expand substitutes %(name)s and %(name)02d from vars and turns "%%" into
// "%". Unknown names (and non-numeric values for %d) are left as written.
func Expand(v string, vars map[string]string) string {
	return expandRe.ReplaceAllStringFunc(v, func(m string) string {
		if m == "%%" {
			return "%"
		}
		sub := expandRe.FindStringSubmatch(m)
		val, ok := vars[sub[1]]
		if !ok {
			return m
		}
		if sub[3] == "s" {
			return val
		}
		n, err := strconv.Atoi(val)
		if err != nil {
			return m
		}
		width, _ := strconv.Atoi(sub[2])
		if strings.HasPrefix(sub[2], "0") {
			return fmt.Sprintf("%0*d", width, n)
		}
		return fmt.Sprintf("%*d", width, n)
	})
}
