package validate

import (
	"fmt"
	"regexp"
	"strings"
)

// Hidden replaces an environment value an AI agent may not read.
const Hidden = "«hidden»"

// envKey is an environment= line, commented out or not (old secrets are
// often kept as comments): its key and separator, kept as written.
var envKey = regexp.MustCompile(`(?i)^[ \t]*(?:[;#][ \t]*)?environment[ \t]*[=:][ \t]*`)

// envEntry is one environment= setting: as an agent sees it (one line,
// values hidden) and as written (with its continuation lines).
type envEntry struct{ hidden, original string }

// walkEnv calls text for every other line and entry for every environment=
// setting of text.
func walkEnv(text string, other func(line string), entry func(envEntry)) {
	lines := strings.SplitAfter(text, "\n")
	for i := 0; i < len(lines); i++ {
		head := envKey.FindString(lines[i])
		if head == "" {
			other(lines[i])
			continue
		}
		orig := lines[i]
		value := strings.TrimRight(lines[i][len(head):], "\r\n")
		eol := lines[i][len(strings.TrimRight(lines[i], "\r\n")):]
		for i+1 < len(lines) && continuation(lines[i+1]) {
			i++
			orig += lines[i]
			value += " " + strings.TrimSpace(lines[i])
			eol = lines[i][len(strings.TrimRight(lines[i], "\r\n")):]
		}
		entry(envEntry{hidden: head + HideEnvValue(value) + eol, original: orig})
	}
}

// HideEnv returns config text with every environment= value replaced by
// Hidden, keeping the keys (KEY="«hidden»") so an agent still sees which
// variables a program gets. Continuation lines of the value are folded into
// its line; every other line is kept byte for byte.
func HideEnv(text string) string {
	var b strings.Builder
	walkEnv(text, func(l string) { b.WriteString(l) }, func(e envEntry) { b.WriteString(e.hidden) })
	return b.String()
}

// RestoreEnv puts the real environment= settings of disk (the file as it is)
// back in place of the hidden lines of edited (that file as an agent read it
// and changed it). It fails when a hidden line was changed, since Visor can't
// tell which values it should hold.
func RestoreEnv(edited, disk string) (string, error) {
	if !HasHidden(edited) {
		return edited, nil
	}
	originals := map[string]string{}
	ambiguous := map[string]bool{}
	walkEnv(disk, func(string) {}, func(e envEntry) {
		key := strings.TrimRight(e.hidden, "\r\n")
		if o, ok := originals[key]; ok && o != e.original {
			ambiguous[key] = true
		}
		originals[key] = e.original
	})
	var b strings.Builder
	for _, line := range strings.SplitAfter(edited, "\n") {
		key := strings.TrimRight(line, "\r\n")
		if !HasHidden(key) {
			b.WriteString(line)
			continue
		}
		o, ok := originals[key]
		switch {
		case !ok:
			return "", fmt.Errorf("this line holds «hidden» values but isn't as Visor showed it, so Visor can't put them back: %q. Keep hidden lines exactly as read_config gave them, or edit them in Visor", key)
		case ambiguous[key]:
			return "", fmt.Errorf("two programs have the same hidden environment line (%q), so Visor can't tell their values apart: edit this file in Visor", key)
		}
		b.WriteString(strings.TrimRight(o, "\r\n") + line[len(key):])
	}
	return b.String(), nil
}

// continuation reports an indented, non-blank line: part of the value above.
func continuation(line string) bool {
	return strings.TrimSpace(line) != "" && (line[0] == ' ' || line[0] == '\t')
}

// HideEnvValue hides the values of one environment= value
// (A="1",B=2 → A="«hidden»",B="«hidden»"). A value Visor can't read is
// hidden whole.
func HideEnvValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return value
	}
	pairs, err := envPairs(value)
	if err != nil || len(pairs) == 0 {
		return Hidden
	}
	keys := make([]string, len(pairs))
	for i, p := range pairs {
		keys[i] = p.Key + `="` + Hidden + `"`
	}
	return strings.Join(keys, ",")
}

// HasHidden reports text that still holds a hidden value: saving it would
// write «hidden» over the real one.
func HasHidden(text string) bool { return strings.Contains(text, Hidden) }
