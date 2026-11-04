// Package notify decides which issues become macOS notifications and what
// they say. It knows nothing about Wails: internal/app sends the messages
// through the native notification centre.
package notify

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/touhidurabir/visor/internal/issues"
	"github.com/touhidurabir/visor/internal/settings"
)

// ErrUnavailable is returned by a Sender that cannot show notifications in
// this build: the server build, or an app without a bundle identifier
// (`task dev`). It is not worth logging on every issue.
var ErrUnavailable = errors.New("notifications are not available in this build")

// ErrNotAllowed is returned by a Sender when the person has not allowed
// notifications (yet). Nothing was shown, so the issue is not Notified.
var ErrNotAllowed = errors.New("notifications are not allowed for Visor")

// Message is one notification. ID is unique per notification (the issue's
// ID, plus the count for a repeat); IssueID, Kind, Process and Config say
// what it is about.
//
// A banner shows Title and Body. Title is the severity icon and what
// happened ("🔴 flaky can't start"); Body is one sentence that starts with
// the config file's name ("flaky.ini: it exited with status 2."). Subtitle
// stays empty, because macOS shows it as a line of its own. Headline and
// Detail are the same words without the icon and the file name, for
// Visor's own list, which shows those itself. Thread groups the banners:
// the config file's path, or DaemonThread for supervisord itself.
type Message struct {
	ID       string
	Title    string
	Subtitle string
	Body     string
	Headline string
	Detail   string
	Thread   string
	IssueID  string
	Kind     issues.Kind
	Process  string
	Config   string
}

// DaemonThread is the banner thread of notifications that belong to no
// config: supervisord not responding, or a process in no known config.
const DaemonThread = "supervisord"

// Severity is how serious a notification is: Error (a process can't run,
// keeps crashing, or supervisord doesn't answer), Warning (it crashed and
// supervisord restarted it) or Info.
type Severity string

const (
	Error   Severity = "error"
	Warning Severity = "warning"
	Info    Severity = "info"
)

// SeverityOf is how serious an issue of kind k is.
func SeverityOf(k issues.Kind) Severity {
	switch k {
	case issues.KindFatal, issues.KindCrashLoop, issues.KindUnreachable:
		return Error
	case issues.KindUnexpectedExit:
		return Warning
	}
	return Info
}

// Icon is the severity as a banner shows it, before the title: a coloured
// circle, the only colour a macOS banner can carry.
func (s Severity) Icon() string {
	switch s {
	case Error:
		return "🔴"
	case Warning:
		return "🟠"
	}
	return "🔵"
}

// Sender shows a notification.
type Sender interface {
	Send(Message) error
}

// ShouldNotify reports whether a newly opened issue becomes a
// notification: notifications are on, so is the issue's kind, and its
// config is watched. supervisord not responding is about every config, so
// it ignores watching.
func ShouldNotify(is issues.Issue, prefs settings.NotifyPrefs, watched bool) bool {
	if !prefs.Enabled {
		return false
	}
	switch is.Kind {
	case issues.KindFatal:
		return prefs.Fatal && watched
	case issues.KindUnexpectedExit:
		return prefs.UnexpectedExit && watched
	case issues.KindCrashLoop:
		return prefs.CrashLoop && watched
	case issues.KindUnreachable:
		return prefs.Unreachable
	}
	return false
}

// Compose words the notification for an issue. The headline says what
// happened to which process. The detail is one sentence with the reason
// and, for a failure that keeps happening, "the 3rd time in 10 minutes".
// The banner puts the severity icon before the headline and the config
// file's name before the detail.
func Compose(is issues.Issue) Message {
	m := Message{ID: is.ID, IssueID: is.ID, Kind: is.Kind, Process: is.Process, Config: is.Config, Thread: is.Config}
	if m.Thread == "" {
		m.Thread = DaemonThread
	}
	text := clause(is)
	if n := is.Times(); n > 1 {
		m.ID = fmt.Sprintf("%s-%d", is.ID, n)
		again := "the " + ordinal(n) + " time in " + span(is.Latest().Sub(is.OpenedAt))
		if text == "" {
			text = again
		} else {
			text += ", " + again
		}
	}
	if text != "" {
		m.Detail = sentence(text)
		if !strings.HasPrefix(text, "supervisord") { // a name, written in lower case
			m.Detail = upperFirst(m.Detail)
		}
	}
	m.Body = m.Detail
	if is.Config != "" && is.Kind != issues.KindUnreachable {
		file := filepath.Base(is.Config)
		if text == "" {
			m.Body = "From " + file + "."
		} else {
			m.Body = sentence(file + ": " + text)
		}
	}
	name := DisplayName(is.Process)
	switch is.Kind {
	case issues.KindFatal:
		m.Headline = name + " can't start"
	case issues.KindUnexpectedExit:
		m.Headline = name + " exited unexpectedly"
	case issues.KindCrashLoop:
		m.Headline = name + " keeps restarting"
	case issues.KindUnreachable:
		m.Headline = "supervisord isn't responding"
	default:
		m.Headline = name + " has a problem"
	}
	m.Title = SeverityOf(is.Kind).Icon() + " " + m.Headline
	return m
}

// unreachablePrefix starts the issues package's message for supervisord
// not responding; the error after it says why.
const unreachablePrefix = "supervisord is not responding"

// clause turns an issue's message into the middle of a sentence, to follow
// "flaky.ini: ". Visor's own wording ("Exited with status 2", "Restarted 5
// times in 10 minutes") becomes "it exited with status 2", "it was
// restarted 5 times in 10 minutes"; supervisord not responding becomes
// "Visor can't reach it" plus the error. supervisord's own reason ("can't
// find command '/x'") is kept as it is.
func clause(is issues.Issue) string {
	msg := strings.TrimSuffix(strings.TrimSpace(is.Message), ".")
	if msg == "" {
		return ""
	}
	if is.Kind == issues.KindUnreachable {
		rest, ok := strings.CutPrefix(msg, unreachablePrefix)
		if !ok {
			return msg
		}
		if why := strings.TrimSpace(strings.TrimPrefix(rest, ":")); why != "" {
			return "Visor can't reach it: " + why
		}
		return "Visor can't reach it"
	}
	for _, own := range [...]struct{ prefix, as string }{
		{"Exited", "it exited"},
		{"Was ", "it was "},
		{"Restarted", "it was restarted"},
	} {
		if rest, ok := strings.CutPrefix(msg, own.prefix); ok {
			return own.as + rest
		}
	}
	return msg
}

// sentence ends s with a full stop, unless it already ends a sentence.
func sentence(s string) string {
	if strings.HasSuffix(s, ".") || strings.HasSuffix(s, "!") || strings.HasSuffix(s, "?") {
		return s
	}
	return s + "."
}

// ordinal is 1st, 2nd, 3rd, 4th, ..., 11th, 12th, 13th, 21st, ...
func ordinal(n int) string {
	suffix := "th"
	if n%100 < 11 || n%100 > 13 {
		switch n % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return strconv.Itoa(n) + suffix
}

// span says how long d is in one unit, rounding up: "40 seconds",
// "2 minutes", "3 hours", "2 days".
func span(d time.Duration) string {
	unit := func(n int64, one string) string {
		if n == 1 {
			return "1 " + one
		}
		return strconv.FormatInt(n, 10) + " " + one + "s"
	}
	up := func(u time.Duration) int64 { return int64((d + u - 1) / u) }
	switch {
	case d < time.Minute:
		return unit(max(up(time.Second), 1), "second")
	case d < time.Hour:
		return unit(up(time.Minute), "minute")
	case d < 48*time.Hour:
		return unit(up(time.Hour), "hour")
	}
	return unit(up(24*time.Hour), "day")
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// DisplayName is the shortest name for a process ("group:name"): the bare
// name when the group has the same name, as supervisorctl shows it.
func DisplayName(process string) string {
	if group, name, ok := strings.Cut(process, ":"); ok && group == name {
		return name
	}
	return process
}

// Limiter lets one notification through per process and kind within a
// window, so a process that fails over and over does not flood the
// notification centre. The zero value is not usable; use NewLimiter.
type Limiter struct {
	window time.Duration

	mu   sync.Mutex
	last map[limitKey]time.Time
}

type limitKey struct {
	process string
	kind    issues.Kind
}

// NewLimiter returns a Limiter that allows one notification per process
// and kind every window.
func NewLimiter(window time.Duration) *Limiter {
	return &Limiter{window: window, last: map[limitKey]time.Time{}}
}

// Allow reports whether a notification for is may be sent at now, and if
// so counts it. Call Release when that notification then could not be
// shown.
func (l *Limiter) Allow(is issues.Issue, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	k := limitKey{is.Process, is.Kind}
	if last, ok := l.last[k]; ok && now.Sub(last) < l.window {
		return false
	}
	l.last[k] = now
	for key, at := range l.last { // forget what can no longer limit
		if now.Sub(at) >= l.window {
			delete(l.last, key)
		}
	}
	return true
}

// Release gives back the turn Allow(is, at) took, when that notification
// could not be shown, so it does not hold back the next one. A later turn
// for the same process and kind is kept.
func (l *Limiter) Release(is issues.Issue, at time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	k := limitKey{is.Process, is.Kind}
	if last, ok := l.last[k]; ok && last.Equal(at) {
		delete(l.last, k)
	}
}
