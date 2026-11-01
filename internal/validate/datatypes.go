package validate

// Ports of the value parsers supervisord 4.2.5 runs on a [program:x]
// section (supervisor/datatypes.py), so Visor accepts exactly what
// supervisord accepts. A value supervisord rejects makes its reloadConfig
// fail (CANT_REREAD) for every file, and stops it from starting at all.

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
)

// pyIntRe is Python's int() of a str in base 10 (after strip): an optional
// sign, then digits with single underscores between them.
var pyIntRe = regexp.MustCompile(`^[+-]?[0-9]+(_[0-9]+)*$`)

// pyInt is datatypes.integer: Python's int(value). It returns the value
// (clamped on overflow) and whether supervisord accepts it.
func pyInt(v string) (int64, bool) {
	v = strings.TrimSpace(v)
	if !pyIntRe.MatchString(v) {
		return 0, false
	}
	n, err := strconv.ParseInt(strings.ReplaceAll(strings.TrimPrefix(v, "+"), "_", ""), 10, 64)
	if err != nil { // out of range: Python has big ints, so it is valid
		if strings.HasPrefix(v, "-") {
			return -1 << 63, true
		}
		return 1<<63 - 1, true
	}
	return n, true
}

// The strings datatypes.boolean accepts (lower-cased).
var (
	truthy = map[string]bool{"yes": true, "true": true, "on": true, "1": true}
	falsy  = map[string]bool{"no": true, "false": true, "off": true, "0": true}
)

// isBoolean is datatypes.boolean.
func isBoolean(v string) bool {
	v = strings.ToLower(v)
	return truthy[v] || falsy[v]
}

// isAutoRestart is datatypes.auto_restart: a boolean or "unexpected".
func isAutoRestart(v string) bool {
	return isBoolean(v) || strings.ToLower(v) == "unexpected"
}

// signalNames are the names Python's signal module has on macOS (the SIG
// prefix is optional in a config); maxSignal is the highest signal number.
var signalNames = map[string]bool{
	"ABRT": true, "ALRM": true, "BUS": true, "CHLD": true, "CONT": true, "EMT": true, "FPE": true,
	"HUP": true, "ILL": true, "INFO": true, "INT": true, "IO": true, "IOT": true, "KILL": true,
	"PIPE": true, "PROF": true, "QUIT": true, "SEGV": true, "STOP": true, "SYS": true, "TERM": true,
	"TRAP": true, "TSTP": true, "TTIN": true, "TTOU": true, "URG": true, "USR1": true, "USR2": true,
	"VTALRM": true, "WINCH": true, "XCPU": true, "XFSZ": true,
	// Not signals, but Python's signal module has them as SIG_*, so
	// supervisord takes them (as 0 to 3).
	"_BLOCK": true, "_DFL": true, "_IGN": true, "_SETMASK": true, "_UNBLOCK": true,
}

const maxSignal = 31

// isSignal is datatypes.signal_number: a signal number, or a signal name
// with or without SIG, in any case.
func isSignal(v string) bool {
	if n, ok := pyInt(v); ok {
		return n >= 0 && n <= maxSignal
	}
	name := strings.ToUpper(strings.TrimSpace(v))
	return signalNames[strings.TrimPrefix(name, "SIG")] && name != "SIG"
}

// isByteSize is datatypes.byte_size: a whole number, optionally ending in
// KB, MB or GB (any case).
func isByteSize(v string) bool {
	l := strings.ToLower(v)
	for _, s := range []string{"kb", "mb", "gb"} {
		if strings.HasSuffix(l, s) {
			_, ok := pyInt(l[:len(l)-len(s)])
			return ok
		}
	}
	_, ok := pyInt(l)
	return ok
}

// isExitCodes is datatypes.list_of_exitcodes: comma-separated numbers from
// 0 to 255, or nothing.
func isExitCodes(v string) bool {
	if v == "" {
		return true
	}
	for _, p := range strings.Split(v, ",") {
		n, ok := pyInt(p)
		if !ok || n < 0 || n > 255 {
			return false
		}
	}
	return true
}

var octalRe = regexp.MustCompile(`^[+-]?(0[oO]_?)?[0-7]+(_[0-7]+)*$`)

// isOctal is datatypes.octal_type: Python's int(value, 8).
func isOctal(v string) bool { return octalRe.MatchString(strings.TrimSpace(v)) }

// isProcessName is datatypes.process_or_group_name: no space, colon or
// slash once trimmed.
func isProcessName(v string) bool {
	return !strings.ContainsAny(strings.TrimSpace(v), " :/")
}

// envPair is one KEY=value of environment=, with the value as supervisord
// sets it. Dropped is the token supervisord skipped after the value when it
// is not a comma or semicolon (text the user probably meant as part of the
// value, as in A=café or A=abc=).
type envPair struct {
	Key, Value, Dropped string
}

var errNoClosingQuote = errors.New("a quote is not closed")

// envPairs is datatypes.dict_of_key_value_pairs: the value is split into
// tokens as Python's shlex does (non-POSIX), and read as KEY = VALUE
// triples, each followed by one token that is skipped (meant to be the
// comma).
func envPairs(v string) ([]envPair, error) {
	toks, err := shlexTokens(v)
	if err != nil {
		return nil, err
	}
	var out []envPair
	for i := 0; i < len(toks); i += 4 {
		if i+3 > len(toks) || toks[i+1] != "=" {
			return nil, errors.New("a KEY=value pair is incomplete")
		}
		p := envPair{Key: toks[i], Value: strings.Trim(toks[i+2], `'"`)}
		if i+3 < len(toks) && toks[i+3] != "," && toks[i+3] != ";" {
			p.Dropped = toks[i+3]
		}
		out = append(out, p)
	}
	return out, nil
}

// shlexWord is shlex's default wordchars plus what supervisord adds.
func shlexWord(c rune) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("_/.+-():", c)
}

func shlexSpace(c rune) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' }

// shlexTokens is list(shlex.shlex(s)) with supervisord's wordchars:
// non-POSIX mode, so quotes stay in the token and a quote inside a word
// is part of it; '#' starts a comment to the end of the line; any other
// character is a token of its own.
func shlexTokens(s string) ([]string, error) {
	rs := []rune(s)
	var out []string
	var tok strings.Builder
	emit := func() {
		out = append(out, tok.String())
		tok.Reset()
	}
	// toEOL is the index of the newline ending the line at i (or the last
	// rune): readline() consumes through it.
	toEOL := func(i int) int {
		for ; i < len(rs) && rs[i] != '\n'; i++ {
		}
		return min(i, len(rs)-1)
	}
	state := ' ' // ' ' between tokens, 'a' in a word, or the open quote
	for i := 0; i <= len(rs); i++ {
		eof := i == len(rs)
		var c rune
		if !eof {
			c = rs[i]
		}
		switch state {
		case ' ':
			switch {
			case eof:
				return out, nil
			case shlexSpace(c):
			case c == '#':
				i = toEOL(i)
			case shlexWord(c):
				tok.WriteRune(c)
				state = 'a'
			case c == '\'' || c == '"':
				tok.WriteRune(c)
				state = c
			default:
				tok.WriteRune(c)
				emit()
			}
		case 'a':
			switch {
			case eof:
				emit()
				return out, nil
			case shlexSpace(c):
				state = ' '
				emit()
			case c == '#':
				i = toEOL(i) // non-POSIX shlex stays in the word
			case shlexWord(c) || c == '\'' || c == '"':
				tok.WriteRune(c)
			default:
				state = ' '
				emit()
				i-- // read c again as the start of the next token
			}
		default: // inside quotes
			if eof {
				return nil, errNoClosingQuote
			}
			tok.WriteRune(c)
			if c == state {
				state = ' '
				emit()
			}
		}
	}
	return out, nil
}
