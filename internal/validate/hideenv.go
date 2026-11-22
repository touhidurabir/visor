package validate

import (
	"regexp"
	"strings"
)

// Hidden replaces an environment value an AI agent may not read.
const Hidden = "«hidden»"

// envKey is an environment= line: its key and separator, kept as written.
var envKey = regexp.MustCompile(`(?i)^[ \t]*environment[ \t]*[=:][ \t]*`)

// HideEnv returns config text with every environment= value replaced by
// Hidden, keeping the keys (KEY="«hidden»") so an agent still sees which
// variables a program gets. Continuation lines of the value are folded into
// its line; every other line is kept byte for byte.
func HideEnv(text string) string {
	lines := strings.SplitAfter(text, "\n")
	var b strings.Builder
	for i := 0; i < len(lines); i++ {
		head := envKey.FindString(lines[i])
		if head == "" {
			b.WriteString(lines[i])
			continue
		}
		value := strings.TrimRight(lines[i][len(head):], "\r\n")
		eol := lines[i][len(strings.TrimRight(lines[i], "\r\n")):]
		for i+1 < len(lines) && continuation(lines[i+1]) {
			i++
			value += " " + strings.TrimSpace(lines[i])
			eol = lines[i][len(strings.TrimRight(lines[i], "\r\n")):]
		}
		b.WriteString(head + HideEnvValue(value) + eol)
	}
	return b.String()
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
