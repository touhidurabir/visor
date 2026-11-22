package logs

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Log levels, ported from frontend/src/lib/logview.ts (levelOf), so the
// Logs tab's Errors filter and MCP's errors_only read lines the same way. A
// line gets a level only when it says so in a form that is hard to mistake;
// keep the two in step (level_test.go shares the frontend's cases).

var levelWords = map[string]string{
	"emergency": "error", "emerg": "error", "alert": "error", "critical": "error", "crit": "error", "fatal": "error", "panic": "error",
	"error": "error", "err": "error", "severe": "error", "fail": "error", "failed": "error",
	"warning": "warn", "warn": "warn",
	"notice": "info", "info": "info", "information": "info", "done": "info", "processed": "info",
	"debug": "debug", "trace": "debug", "verbose": "debug", "running": "debug", "processing": "debug",
}

var levelLetters = map[string]string{"F": "error", "E": "error", "W": "warn", "I": "info", "D": "debug"}

var (
	jsonLevel   = regexp.MustCompile(`(?i)"(?:level|level_name|levelname|severity|lvl|loglevel|log\.level)"\s*:\s*"?([A-Za-z]+|\d+)`)
	kvLevel     = regexp.MustCompile(`(?i)(?:^|\s)(?:level|lvl|severity)=["']?([A-Za-z]+)`)
	monolog     = regexp.MustCompile(`^\[[^\]]{8,40}\]\s+[\w.-]+\.([A-Z]+):`)
	rails       = regexp.MustCompile(`^([DIWEF]), \[`)
	klog        = regexp.MustCompile(`^([IWEF])\d{4} \d\d:`)
	queueStatus = regexp.MustCompile(`\s(DONE|FAIL|FAILED|RUNNING)\s*$`)
	queueOld    = regexp.MustCompile(`\]\s*(Processing|Processed|Failed):\s`)
	thrown      = regexp.MustCompile(`^(?:Traceback \(most recent call last\)|panic: |PHP (?:Fatal|Parse) error|Uncaught |(?:[\w$]+[.\\])*[\w$]*(?:Error|Exception)(?::|\s+in\s|$))`)
	upper       = regexp.MustCompile(`\b(EMERGENCY|EMERG|ALERT|CRITICAL|CRIT|FATAL|PANIC|ERROR|ERR|SEVERE|WARNING|WARN|NOTICE|INFO|DEBUG|TRACE)\b`)
	bracketed   = regexp.MustCompile(`(?i)[\[<(](emergency|alert|critical|crit|fatal|error|err|warning|warn|notice|info|debug|trace)[\]>)]`)
	continues   = regexp.MustCompile(`^(?:\s|#\d+ |\[stacktrace\]|Stack trace:|Caused by|Next |\.\.\. \d+ more|"?\}|\))`)
)

// levelOfNumber reads pino and bunyan's 10 to 60, and Monolog's 100 to 600.
func levelOfNumber(n int) string {
	t := [3]int{50, 40, 30} // error, warn, info
	if n >= 100 {
		t = [3]int{400, 300, 200}
	}
	switch {
	case n >= t[0]:
		return "error"
	case n >= t[1]:
		return "warn"
	case n >= t[2]:
		return "info"
	}
	return "debug"
}

func prefix(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func word(m []string) string {
	if m == nil {
		return ""
	}
	return levelWords[strings.ToLower(m[1])]
}

// LineLevel returns the level a log line states ("error", "warn", "info",
// "debug"), or "" when it states none.
func LineLevel(text string) string {
	head := prefix(text, 240)
	if strings.HasPrefix(strings.TrimLeft(head, " \t"), "{") {
		if m := jsonLevel.FindStringSubmatch(prefix(text, 4000)); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil {
				return levelOfNumber(n)
			}
			return word(m)
		}
	}
	if l := word(kvLevel.FindStringSubmatch(head)); l != "" {
		return l
	}
	if l := word(monolog.FindStringSubmatch(head)); l != "" {
		return l
	}
	if m := rails.FindStringSubmatch(head); m != nil {
		return levelLetters[m[1]]
	}
	if m := klog.FindStringSubmatch(head); m != nil {
		return levelLetters[m[1]]
	}
	if m := queueStatus.FindStringSubmatch(text); m != nil {
		return word(m)
	}
	if m := queueOld.FindStringSubmatch(head); m != nil {
		return word(m)
	}
	if thrown.MatchString(head) {
		return "error"
	}
	if m := upper.FindStringSubmatch(prefix(head, 120)); m != nil {
		return word(m)
	}
	return word(bracketed.FindStringSubmatch(head))
}

// ErrorsOnly keeps the error lines of text and the stack-trace lines under
// them, as the Logs tab's Errors filter shows them.
func ErrorsOnly(text string) string {
	var b strings.Builder
	prev := ""
	for _, line := range strings.SplitAfter(text, "\n") {
		bare := strings.TrimRight(line, "\r\n")
		if line == "" {
			continue
		}
		l := LineLevel(bare)
		if l == "" && prev != "" && continues.MatchString(bare) {
			l = prev
		}
		prev = l
		if l == "error" {
			b.WriteString(line)
		}
	}
	return b.String()
}

// CutFront keeps at most max bytes from the end of text, starting at a line
// when it can, as valid UTF-8; cut reports that something was dropped.
func CutFront(text string, max int) (string, bool) {
	text = strings.ToValidUTF8(text, "�")
	if len(text) <= max {
		return text, false
	}
	start := len(text) - max
	if text[start-1] != '\n' {
		if i := strings.IndexByte(text[start:], '\n'); i >= 0 && start+i+1 < len(text) {
			start += i + 1
		}
	}
	for start < len(text) && !utf8.RuneStart(text[start]) {
		start++
	}
	return text[start:], true
}
