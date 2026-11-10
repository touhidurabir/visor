package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/touhidurabir/visor/internal/issues"
	"github.com/touhidurabir/visor/internal/notify"
	"github.com/touhidurabir/visor/internal/settings"
)

func (r *recorder) inboxes() []NotificationsView {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []NotificationsView
	for _, e := range r.events {
		if e.name == EventNotifications {
			out = append(out, e.data.(NotificationsView))
		}
	}
	return out
}

func inboxRuntime(t *testing.T, sender notify.Sender, prefs settings.NotifyPrefs) (*Runtime, *recorder) {
	t.Helper()
	rec := &recorder{}
	s := settings.Defaults()
	s.Notify = prefs
	rt := NewRuntime(RuntimeOptions{StateDir: t.TempDir(), Notifier: sender, Settings: s, Emit: rec.emit})
	return rt, rec
}

func exitIssue(id, proc string, count int) issues.Issue {
	return issues.Issue{ID: id, Kind: issues.KindUnexpectedExit, Process: proc, Config: "/c/" + proc + ".ini",
		Message: "Exited with status 2", Status: issues.StatusOpen, Count: count, OpenedAt: time.Now()}
}

// Every issue the settings allow lands in Visor's own list, even when
// macOS shows no banner: not allowed yet, or held back as a repeat.
func TestInboxRecordsWhatBannersMiss(t *testing.T) {
	sender := &fakeSender{err: notify.ErrNotAllowed}
	rt, rec := inboxRuntime(t, sender, allNotify())
	rt.notifyIssues(issues.Result{Opened: []issues.Issue{exitIssue("1", "flaky:flaky", 1)}}, nil)
	rt.sends.Wait()
	sender.mu.Lock()
	sender.err = nil
	sender.mu.Unlock()
	rt.notifyIssues(issues.Result{Opened: []issues.Issue{exitIssue("2", "flaky:flaky", 1)}}, nil)
	rt.notifyIssues(issues.Result{Opened: []issues.Issue{exitIssue("3", "flaky:flaky", 1)}}, nil) // limited
	rt.sends.Wait()

	v := NewInboxService(rt).Notifications()
	if v.Unread != 3 || len(v.Items) != 3 {
		t.Fatalf("inbox = %+v", v)
	}
	shown := map[string]bool{}
	for _, it := range v.Items {
		shown[it.IssueID] = it.Shown
		if it.ConfigName != "flaky:flaky.ini" || it.Title != "flaky exited unexpectedly" {
			t.Errorf("item = %+v", it)
		}
	}
	if shown["1"] || !shown["2"] || shown["3"] {
		t.Errorf("shown = %v, want only issue 2", shown)
	}
	if got := rec.inboxes(); len(got) == 0 || got[len(got)-1].Unread != 3 {
		t.Errorf("events = %+v", got)
	}
}

// A process that keeps crashing updates its unread entry, and the banner
// says how often.
func TestRepeatUpdatesInboxAndBanner(t *testing.T) {
	sender := &fakeSender{}
	rt, _ := inboxRuntime(t, sender, allNotify())
	rt.limiter = notify.NewLimiter(0)
	rt.notifyIssues(issues.Result{Opened: []issues.Issue{exitIssue("1", "flaky:flaky", 1)}}, nil)
	rep := exitIssue("1", "flaky:flaky", 3)
	rep.LastAt = rep.OpenedAt.Add(10 * time.Minute)
	rt.notifyIssues(issues.Result{Repeated: []issues.Issue{rep}}, nil)
	rt.sends.Wait()

	v := NewInboxService(rt).Notifications()
	if len(v.Items) != 1 || v.Items[0].Count != 3 || !strings.Contains(v.Items[0].Detail, "3rd time in 10 minutes") {
		t.Fatalf("inbox = %+v", v.Items)
	}
	// Each banner is sent from its own goroutine, so they may arrive in
	// either order: look for the repeat by its text, not its position.
	msgs := sender.messages()
	repeats := 0
	for _, m := range msgs {
		if strings.Contains(m.Body, "3rd time in 10 minutes") {
			repeats++
		}
	}
	if len(msgs) != 2 || repeats != 1 || msgs[0].ID == msgs[1].ID {
		t.Errorf("banners = %+v", msgs)
	}
}

func TestInboxFollowsSettingsAndWatch(t *testing.T) {
	off := allNotify()
	off.UnexpectedExit = false
	rt, _ := inboxRuntime(t, &fakeSender{}, off)
	rt.notifyIssues(issues.Result{Opened: []issues.Issue{exitIssue("1", "a:a", 1)}}, nil)
	if n := len(NewInboxService(rt).Notifications().Items); n != 0 {
		t.Fatalf("a kind turned off reached the inbox (%d)", n)
	}
	rt2, _ := inboxRuntime(t, &fakeSender{}, allNotify())
	if err := rt2.watch.set("/c/b:b.ini", false); err != nil {
		t.Fatal(err)
	}
	rt2.notifyIssues(issues.Result{Opened: []issues.Issue{exitIssue("2", "b:b", 1)}}, nil)
	if n := len(NewInboxService(rt2).Notifications().Items); n != 0 {
		t.Fatalf("an unwatched config reached the inbox (%d)", n)
	}
}

func TestRecoveredSupervisordIsReadNote(t *testing.T) {
	rt, _ := inboxRuntime(t, &fakeSender{}, allNotify())
	now := time.Now()
	down := issues.Issue{ID: "u", Kind: issues.KindUnreachable, OpenedAt: now.Add(-8 * time.Second), ClosedAt: now, Status: issues.StatusResolvedAuto}
	rt.notifyIssues(issues.Result{Closed: []issues.Issue{down, exitIssue("x", "a:a", 1)}}, nil)
	v := NewInboxService(rt).Notifications()
	if len(v.Items) != 1 || !v.Items[0].Read || v.Items[0].Title != "supervisord is responding again" || v.Unread != 0 {
		t.Fatalf("inbox = %+v", v)
	}
}

func TestInboxServiceMarksRead(t *testing.T) {
	rt, rec := inboxRuntime(t, &fakeSender{}, allNotify())
	rt.notifyIssues(issues.Result{Opened: []issues.Issue{exitIssue("1", "a:a", 1), exitIssue("2", "b:b", 1)}}, nil)
	rt.sends.Wait()
	svc := NewInboxService(rt)
	items := svc.Notifications().Items
	if err := svc.MarkRead(items[0].ID); err != nil {
		t.Fatal(err)
	}
	if v := svc.Notifications(); v.Unread != 1 || !v.Items[0].Read {
		t.Fatalf("after MarkRead = %+v", v)
	}
	if err := svc.MarkRead("gone"); !errors.Is(err, notify.ErrNoEntry) {
		t.Errorf("MarkRead(gone) = %v", err)
	}
	if err := svc.MarkAllRead(); err != nil || svc.Notifications().Unread != 0 {
		t.Fatalf("MarkAllRead: %v", err)
	}
	if got := rec.inboxes(); got[len(got)-1].Unread != 0 {
		t.Errorf("last event = %+v", got[len(got)-1])
	}
}

// The menu-bar count needs the unread number from launch, before anything
// new arrives.
func TestStartSendsTheInbox(t *testing.T) {
	rt, rec := inboxRuntime(t, &fakeSender{}, allNotify())
	if err := rt.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer rt.Stop()
	if got := rec.inboxes(); len(got) == 0 {
		t.Fatal("Start sent no visor:notifications")
	}
}

func TestNotificationsViewSaysHowSerious(t *testing.T) {
	b := notify.OpenInbox("", time.Now)
	if _, err := b.Record(issues.Issue{ID: "f", Kind: issues.KindFatal, Process: "a:a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Record(issues.Issue{ID: "x", Kind: issues.KindUnexpectedExit, Process: "b:b"}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Recovered(issues.Issue{ID: "u", Kind: issues.KindUnreachable, OpenedAt: time.Now().Add(-time.Second)}); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, n := range notificationsView(b).Items {
		got[n.IssueID] = n.Severity
	}
	if got["f"] != "error" || got["x"] != "warning" || got["u"] != "info" {
		t.Errorf("severities = %v", got)
	}
}
