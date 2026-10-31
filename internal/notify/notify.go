// Package notify decides which issues become macOS notifications and what
// they say. It knows nothing about Wails: internal/app sends the messages
// through the native notification centre.
package notify

import (
	"errors"
	"path/filepath"
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
// ID); IssueID, Process and Config say what it is about.
type Message struct {
	ID      string
	Title   string
	Body    string
	IssueID string
	Process string
	Config  string
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
// happened to which process, the body gives supervisord's reason and the
// config file it comes from.
func Compose(is issues.Issue) Message {
	m := Message{ID: is.ID, IssueID: is.ID, Process: is.Process, Config: is.Config, Body: is.Message}
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
// so counts it.
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
