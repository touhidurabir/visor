package app

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/touhidurabir/visor/internal/settings"
	"github.com/touhidurabir/visor/internal/supervisor/supervisortest"
)

// waitEvent waits for an activity event matching ok.
func waitEvent(t *testing.T, is *IssueService, what string, ok func(ActivityEventView) bool) ActivityEventView {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		for _, e := range is.Activity(ActivityQuery{}).Events {
			if ok(e) {
				return e
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("no activity event %s: %+v", what, is.Activity(ActivityQuery{}).Events)
	return ActivityEventView{}
}

func TestActivityViaMCP(t *testing.T) {
	old := activityGroupWait
	activityGroupWait = 200 * time.Millisecond
	t.Cleanup(func() { activityGroupWait = old })
	inst := supervisortest.Start(t, supervisortest.Sleeper, worker)
	sender := &fakeSender{}
	prefs := settings.Defaults().Notify
	prefs.Enabled = true
	rt, svc, _ := notifyRuntime(t, inst, t.TempDir(), sender, prefs)
	waitRunning(t, svc, "sleeper:sleeper")
	waitRunning(t, svc, "worker:worker")
	is := NewIssueService(rt)

	// Through MCP: restart sleeper as Claude Code. Plainly: restart worker.
	done := rt.markVia("Claude Code", expectProcess("sleeper"), false)
	if err := svc.Restart("sleeper"); err != nil {
		t.Fatal(err)
	}
	done()
	if err := svc.Restart("worker"); err != nil {
		t.Fatal(err)
	}
	e := waitEvent(t, is, "sleeper restarted via Claude Code", func(e ActivityEventView) bool {
		return e.Process == "sleeper:sleeper" && e.Kind == "process_restarted"
	})
	if !e.ByVisor || e.Via != "Claude Code" {
		t.Fatalf("sleeper's restart: %+v", e)
	}
	w := waitEvent(t, is, "worker restarted", func(e ActivityEventView) bool {
		return e.Process == "worker:worker" && e.Kind == "process_restarted"
	})
	if !w.ByVisor || w.Via != "" {
		t.Fatalf("worker's plain restart: %+v", w)
	}

	// A config saved through MCP: its events say via Codex.
	path := confFile(inst, "worker")
	fv, err := NewConfigService(rt).Read(path)
	if err != nil {
		t.Fatal(err)
	}
	done = rt.markVia("Codex", rt.expectFile(path), true)
	if _, err := NewConfigService(rt).Save(path, strings.Replace(fv.Content, "sleep 3600", "sleep 3601", 1), fv.Hash); err != nil {
		t.Fatal(err)
	}
	done()
	c := waitEvent(t, is, "worker's config change via Codex", func(e ActivityEventView) bool {
		return e.Via == "Codex" && (e.Process == "worker:worker" || e.Config == path)
	})
	if !c.ByVisor {
		t.Fatalf("a via event isn't Visor's: %+v", c)
	}

	// Nothing about these changes notified.
	time.Sleep(500 * time.Millisecond)
	for _, m := range sender.messages() {
		if strings.HasPrefix(m.ID, "activity-") {
			t.Fatalf("notified about Visor's own change: %+v", m)
		}
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
