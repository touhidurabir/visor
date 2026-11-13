package app

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/touhidurabir/visor/internal/activity"
	"github.com/touhidurabir/visor/internal/notify"
	"github.com/touhidurabir/visor/internal/settings"
)

// activityGroupWait is how long changes made outside Visor are gathered
// before one notification tells of them all.
var activityGroupWait = 3 * time.Second

// activityBatch holds outside changes waiting to be notified.
type activityBatch struct {
	mu      sync.Mutex
	pending []activity.Event
	timer   *time.Timer
}

// ActivityRoute is the main window's route to a tab's Activity.
func ActivityRoute(server string) string {
	if server == "" || server == LocalID || !settings.ValidServerID(server) {
		return "#/activity"
	}
	return "#/s/" + server + "/activity"
}

// mutes are the activity kinds this tab doesn't notify about.
func (r *Runtime) mutes() settings.ActivityMutes {
	if s, ok := r.remoteServer(); ok {
		return s.Mutes
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.prefs.Mutes
}

func muted(m settings.ActivityMutes, k activity.Kind) bool {
	switch k.Group() {
	case "processes":
		return m.Processes
	case "programs":
		return m.Programs
	case "files":
		return m.Files
	}
	return m.Daemon
}

// notifyActivity queues changes made outside Visor for one grouped
// notification. Visor's own changes never notify as outside.
func (r *Runtime) notifyActivity(events []activity.Event) {
	r.mu.Lock()
	on := r.prefs.Notify.Enabled
	r.mu.Unlock()
	if !on || r.opts.Notifier == nil {
		return
	}
	m := r.mutes()
	var keep []activity.Event
	for _, e := range events {
		if !e.ByVisor && !muted(m, e.Kind) {
			keep = append(keep, e)
		}
	}
	if len(keep) == 0 {
		return
	}
	r.mu.Lock()
	if r.actBatch == nil {
		r.actBatch = &activityBatch{}
	}
	b := r.actBatch
	r.mu.Unlock()
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pending = append(b.pending, keep...)
	if b.timer == nil {
		b.timer = time.AfterFunc(activityGroupWait, func() { r.flushActivity(b) })
	}
}

func (r *Runtime) flushActivity(b *activityBatch) {
	b.mu.Lock()
	events := b.pending
	b.pending, b.timer = nil, nil
	b.mu.Unlock()
	if len(events) == 0 {
		return
	}
	where := ""
	if s, ok := r.remoteServer(); ok {
		where = " on " + s.Name
	}
	msg := composeActivity(events, where)
	msg.Server = ""
	if s, ok := r.remoteServer(); ok {
		msg.Server = s.ID
	}
	msg.Route = ActivityRoute(msg.Server)
	r.sends.Add(1)
	go func() {
		defer r.sends.Done()
		if err := r.opts.Notifier.Send(msg); err != nil && !errors.Is(err, notify.ErrUnavailable) && !errors.Is(err, notify.ErrNotAllowed) {
			log.Printf("visor: cannot show the activity notification: %v", err)
		}
	}()
}

// composeActivity words changes made outside Visor as one notification:
// "web:web restarted outside Visor on cpcsl dev", or "3 processes restarted
// outside Visor on cpcsl dev" for several of a kind.
func composeActivity(events []activity.Event, where string) notify.Message {
	first := events[0]
	m := notify.Message{
		ID:     "activity-" + first.ID,
		Thread: "activity",
	}
	same := true
	for _, e := range events[1:] {
		same = same && e.Kind == first.Kind
	}
	var head string
	switch {
	case len(events) == 1:
		head = first.Text() + " outside Visor" + where // names stay as written
	case same:
		head = fmt.Sprintf("%d %s outside Visor%s", len(events), pluralKind(first.Kind), where)
	default:
		head = fmt.Sprintf("%d changes outside Visor%s", len(events), where)
	}
	var lines []string
	for i, e := range events {
		if i == 4 {
			lines = append(lines, fmt.Sprintf("and %d more", len(events)-i))
			break
		}
		lines = append(lines, e.Text())
	}
	m.Headline = head
	m.Title = notify.Info.Icon() + " " + head
	if len(events) > 1 {
		m.Body = strings.Join(lines, ", ") + "."
	}
	m.Detail = m.Body
	return m
}

func pluralKind(k activity.Kind) string {
	switch k {
	case activity.ProcessStarted:
		return "processes started"
	case activity.ProcessStopped:
		return "processes stopped"
	case activity.ProcessRestarted:
		return "processes restarted"
	case activity.ProgramAdded:
		return "programs added"
	case activity.ProgramRemoved:
		return "programs removed"
	case activity.FileAdded:
		return "config files added"
	case activity.FileChanged:
		return "config files changed"
	case activity.FileRemoved:
		return "config files removed"
	}
	return "changes"
}
