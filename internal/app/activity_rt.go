package app

import (
	"log"
	"slices"
	"strings"
	"time"

	"github.com/touhidurabir/visor/internal/activity"
	"github.com/touhidurabir/visor/internal/config"
	"github.com/touhidurabir/visor/internal/issues"
	"github.com/touhidurabir/visor/internal/monitor"
)

// EventActivity carries an ActivityView with the events just recorded.
const EventActivity = "visor:activity"

// ActivityEventView is one activity event for the frontend.
type ActivityEventView struct {
	ID       string
	AtMs     int64
	Kind     string
	Group    string // processes, programs, files or daemon
	Process  string
	Config   string
	ByVisor  bool
	Text     string // "web:web restarted"
	FileName string
}

// ActivityView is a tab's new events (on EventActivity) or a page of them.
type ActivityView struct {
	Server string
	Events []ActivityEventView
}

func toActivityView(e activity.Event) ActivityEventView {
	v := ActivityEventView{
		ID: e.ID, AtMs: e.At.UnixMilli(), Kind: string(e.Kind), Group: e.Kind.Group(),
		Process: e.Process, Config: e.Config, ByVisor: e.ByVisor, Text: e.Text(),
	}
	if e.Config != "" {
		v.FileName = baseName(e.Config)
	}
	return v
}

func baseName(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[i+1:]
		}
	}
	return p
}

// visorAct is one of this Visor's own actions, for telling its changes from
// changes made outside it.
type visorAct struct {
	e           expectation
	files       bool // config files Visor writes, moves or removes
	from, until time.Time
}

// visorWindow is how long after an action ends its changes are still
// Visor's: the activity deriver's restart window plus two polls.
const visorWindow = 15 * time.Second

// recordAct remembers an action from now; done moves its end to grace
// after it (at least visorWindow; a daemon action also gets a file check,
// since supervisord's restart is seen there on a server).
func (r *Runtime) recordAct(e expectation, grace time.Duration) (done func()) {
	tail := max(visorWindow, grace)
	if e.daemon {
		tail += remoteFileCheck
	}
	return r.addAct(&visorAct{e: e}, actionTimeout+tail, tail)
}

// markWrites marks config file changes as Visor's while it writes, moves
// or removes files (and a file check after).
func (r *Runtime) markWrites() (done func()) {
	tail := visorWindow + remoteFileCheck
	end := r.addAct(&visorAct{files: true}, time.Hour, tail)
	return func() {
		end()
		// A server's files are re-read at the next poll, not in up to 10 s.
		r.mu.Lock()
		r.filesAt = time.Time{}
		r.mu.Unlock()
	}
}

func (r *Runtime) addAct(a *visorAct, during, after time.Duration) func() {
	now := time.Now()
	a.from, a.until = now.Add(-time.Second), now.Add(during)
	r.mu.Lock()
	r.visorActs = slices.DeleteFunc(r.visorActs, func(x *visorAct) bool { return now.Sub(x.until) > time.Minute })
	r.visorActs = append(r.visorActs, a)
	r.mu.Unlock()
	return func() {
		r.mu.Lock()
		a.until = time.Now().Add(after)
		r.mu.Unlock()
	}
}

// byVisor reports whether one of Visor's own actions covers process ("" is
// supervisord itself) at t.
func (r *Runtime) byVisor(process string, t time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	file := strings.HasPrefix(process, "file:")
	for _, a := range r.visorActs {
		if t.Before(a.from) || t.After(a.until) {
			continue
		}
		switch {
		case file:
			if a.files {
				return true
			}
		case a.e.daemon, process != "" && a.e.match != nil && a.e.match(process):
			return true
		}
	}
	return false
}

// openActivity opens the activity log in the state folder. Caller holds life.
func (r *Runtime) openActivity() {
	r.mu.Lock()
	open, retention := r.actStore != nil, r.prefs.Retention()
	r.mu.Unlock()
	if open {
		return
	}
	s, err := activity.Open(r.opts.StateDir, retention, time.Now())
	if err != nil {
		log.Printf("visor: activity log in %s: %v", r.opts.StateDir, err)
	}
	r.mu.Lock()
	r.actStore = s
	r.mu.Unlock()
}

// recordActivity derives this update's events, stores them, tells the
// frontend and notifies about changes made outside Visor.
func (r *Runtime) recordActivity(u monitor.Update, d config.Discovery, changes config.ChangeSet, checked bool, res issues.Result) {
	r.mu.Lock()
	der, store := r.deriver, r.actStore
	r.mu.Unlock()
	if der == nil || store == nil {
		return
	}
	// A process the tracker sees crashing (now, or with an issue still
	// open, as inside a crash loop) restarts by supervisord's autorestart.
	crashed := map[string]bool{}
	r.mu.Lock()
	tracker := r.tracker
	r.mu.Unlock()
	open := []issues.Issue(nil)
	if tracker != nil {
		open = tracker.Open()
	}
	for _, is := range slices.Concat(res.Opened, res.Repeated, open) {
		if is.Process != "" && is.Kind != issues.KindUnreachable && is.Kind != issues.KindHostDown {
			crashed[is.Process] = true
		}
	}
	in := activity.Input{
		Update: u, Changes: changes, Checked: checked, ConfigOf: r.configOf, ByVisor: r.byVisor,
		Crashed: func(p string) bool { return crashed[p] },
	}
	reconnected := false
	for _, ev := range u.Events {
		reconnected = reconnected || ev.Kind == monitor.Reconnected
	}
	// Read on every file check, and when supervisord answers again: a
	// restart is told from an outage by the pidfile.
	if (checked || reconnected) && d.PidFile != "" {
		if fi, err := d.Files().Stat(d.PidFile); err == nil {
			in.PidMtime = fi.ModTime()
		}
	}
	events := der.Next(in)
	if len(events) == 0 {
		return
	}
	if err := store.Add(events...); err != nil {
		log.Printf("visor: activity log: %v", err)
	}
	v := ActivityView{Server: r.ID()}
	for _, e := range events {
		v.Events = append(v.Events, toActivityView(e))
	}
	r.opts.Emit(EventActivity, v)
	r.notifyActivity(events)
}

// Activity lists the tab's recorded events, newest first.
func (r *Runtime) Activity(q activity.Query) ActivityView {
	r.mu.Lock()
	store := r.actStore
	r.mu.Unlock()
	v := ActivityView{Server: r.ID(), Events: []ActivityEventView{}}
	if store == nil {
		return v
	}
	for _, e := range store.List(q) {
		v.Events = append(v.Events, toActivityView(e))
	}
	return v
}
