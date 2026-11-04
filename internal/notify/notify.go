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
// what it is about. Body is Detail plus the config file's name.
type Message struct {
	ID       string
	Title    string
	Subtitle string // how serious, e.g. "🔴 Error" (macOS can't colour a banner)
	Body     string
	Detail   string
	IssueID  string
	Kind     issues.Kind
	Process  string
	Config   string
}

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

// Label is the severity as a notification shows it. The coloured circle is
// the only colour a macOS banner can carry.
func (s Severity) Label() string {
	switch s {
	case Error:
		return "🔴 Error"
	case Warning:
		return "🟠 Warning"
	}
	return "🔵 Info"
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

// Compose words the notification for an issue: the title says what
// happened to which process, the detail gives supervisord's reason (and,
// for a failure that keeps happening, "3rd time in 10 minutes"), and the
// body adds the config file it comes from.
func Compose(is issues.Issue) Message {
	m := Message{ID: is.ID, IssueID: is.ID, Kind: is.Kind, Process: is.Process, Config: is.Config, Detail: is.Message}
	if n := is.Times(); n > 1 {
		m.ID = fmt.Sprintf("%s-%d", is.ID, n)
		again := ordinal(n) + " time in " + span(is.Latest().Sub(is.OpenedAt))
		if m.Detail == "" {
			m.Detail = upperFirst(again)
		} else {
			m.Detail += " · " + again
		}
	}
	m.Body = m.Detail
	m.Subtitle = SeverityOf(is.Kind).Label()
	name := DisplayName(is.Process)
	switch is.Kind {
	case issues.KindFatal:
		m.Title = name + " can't start"
	case issues.KindUnexpectedExit:
		m.Title = name + " exited unexpectedly"
	case issues.KindCrashLoop:
		m.Title = name + " keeps restarting"
	case issues.KindUnreachable:
		m.Title = "supervisord isn't responding"
	default:
		m.Title = name + " has a problem"
	}
	if is.Config != "" && is.Kind != issues.KindUnreachable {
		file := filepath.Base(is.Config)
		if m.Body == "" {
			m.Body = file
		} else {
			m.Body += " · " + file
		}
	}
	return m
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
