package notify

import (
	"testing"
	"time"

	"github.com/touhidurabir/visor/internal/issues"
	"github.com/touhidurabir/visor/internal/settings"
)

func allOn() settings.NotifyPrefs {
	return settings.NotifyPrefs{Enabled: true, Fatal: true, UnexpectedExit: true, CrashLoop: true, Unreachable: true}
}

func TestShouldNotify(t *testing.T) {
	fatal := issues.Issue{Kind: issues.KindFatal, Process: "a:a"}
	exit := issues.Issue{Kind: issues.KindUnexpectedExit, Process: "a:a"}
	loop := issues.Issue{Kind: issues.KindCrashLoop, Process: "a:a"}
	down := issues.Issue{Kind: issues.KindUnreachable}
	off := func(change func(*settings.NotifyPrefs)) settings.NotifyPrefs {
		p := allOn()
		change(&p)
		return p
	}
	tests := []struct {
		name    string
		is      issues.Issue
		prefs   settings.NotifyPrefs
		watched bool
		want    bool
	}{
		{"fatal watched", fatal, allOn(), true, true},
		{"exit watched", exit, allOn(), true, true},
		{"loop watched", loop, allOn(), true, true},
		{"unreachable", down, allOn(), true, true},
		{"fatal unwatched", fatal, allOn(), false, false},
		{"exit unwatched", exit, allOn(), false, false},
		{"loop unwatched", loop, allOn(), false, false},
		{"unreachable ignores watch", down, allOn(), false, true},
		{"master off", fatal, off(func(p *settings.NotifyPrefs) { p.Enabled = false }), true, false},
		{"master off unreachable", down, off(func(p *settings.NotifyPrefs) { p.Enabled = false }), false, false},
		{"fatal off", fatal, off(func(p *settings.NotifyPrefs) { p.Fatal = false }), true, false},
		{"exit off", exit, off(func(p *settings.NotifyPrefs) { p.UnexpectedExit = false }), true, false},
		{"loop off", loop, off(func(p *settings.NotifyPrefs) { p.CrashLoop = false }), true, false},
		{"unreachable off", down, off(func(p *settings.NotifyPrefs) { p.Unreachable = false }), true, false},
		{"other kinds stay on", fatal, off(func(p *settings.NotifyPrefs) { p.UnexpectedExit, p.CrashLoop = false, false }), true, true},
		{"unknown kind", issues.Issue{Kind: "other", Process: "a:a"}, allOn(), true, false},
	}
	for _, tt := range tests {
		if got := ShouldNotify(tt.is, tt.prefs, tt.watched); got != tt.want {
			t.Errorf("%s: ShouldNotify = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestCompose(t *testing.T) {
	tests := []struct {
		is          issues.Issue
		title, body string
	}{
		{
			issues.Issue{ID: "1", Kind: issues.KindFatal, Process: "mailer-digest:mailer-digest", Config: "/etc/conf.d/mailer.ini", Message: "Exited too quickly"},
			"mailer-digest can't start", "Exited too quickly · mailer.ini",
		},
		{
			issues.Issue{ID: "2", Kind: issues.KindUnexpectedExit, Process: "flaky:flaky", Config: "/c/flaky.ini", Message: "Exited with status 1"},
			"flaky exited unexpectedly", "Exited with status 1 · flaky.ini",
		},
		{
			issues.Issue{ID: "3", Kind: issues.KindCrashLoop, Process: "flaky:flaky", Config: "/c/flaky.ini", Message: "Restarted 5 times in 10 minutes"},
			"flaky keeps restarting", "Restarted 5 times in 10 minutes · flaky.ini",
		},
		{
			issues.Issue{ID: "4", Kind: issues.KindUnreachable, Message: "Cannot reach supervisord"},
			"supervisord isn't responding", "Cannot reach supervisord",
		},
		{ // a process named unlike its group keeps the full name
			issues.Issue{ID: "5", Kind: issues.KindFatal, Process: "shop-queue:shop-queue_00", Config: "/c/shop.ini", Message: "Exited too quickly"},
			"shop-queue:shop-queue_00 can't start", "Exited too quickly · shop.ini",
		},
		{ // no config file known: the reason alone
			issues.Issue{ID: "6", Kind: issues.KindUnexpectedExit, Process: "orphan:orphan", Message: "Exited with status 3"},
			"orphan exited unexpectedly", "Exited with status 3",
		},
		{ // no reason: the file alone
			issues.Issue{ID: "7", Kind: issues.KindFatal, Process: "w:w", Config: "/c/w.ini"},
			"w can't start", "w.ini",
		},
	}
	for _, tt := range tests {
		m := Compose(tt.is)
		if m.Title != tt.title || m.Body != tt.body {
			t.Errorf("Compose(%s) = %q / %q, want %q / %q", tt.is.ID, m.Title, m.Body, tt.title, tt.body)
		}
		if m.ID != tt.is.ID || m.IssueID != tt.is.ID || m.Process != tt.is.Process || m.Config != tt.is.Config {
			t.Errorf("Compose(%s) ids = %+v", tt.is.ID, m)
		}
	}
}

func TestLimiter(t *testing.T) {
	l := NewLimiter(time.Minute)
	t0 := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	exit := issues.Issue{Kind: issues.KindUnexpectedExit, Process: "flaky:flaky"}
	fatal := issues.Issue{Kind: issues.KindFatal, Process: "flaky:flaky"}
	other := issues.Issue{Kind: issues.KindUnexpectedExit, Process: "other:other"}

	if !l.Allow(exit, t0) {
		t.Fatal("first notification limited")
	}
	if l.Allow(exit, t0.Add(59*time.Second)) {
		t.Error("same process and kind within 60s allowed")
	}
	if !l.Allow(fatal, t0.Add(time.Second)) {
		t.Error("another kind for the same process limited")
	}
	if !l.Allow(other, t0.Add(time.Second)) {
		t.Error("another process limited")
	}
	// A limited attempt does not extend the window.
	if !l.Allow(exit, t0.Add(60*time.Second)) {
		t.Error("same process and kind after 60s limited")
	}
	if l.Allow(exit, t0.Add(61*time.Second)) {
		t.Error("window did not restart after the allowed notification")
	}
}

// A notification that could not be shown gives its turn back, so the next
// one for the same process and kind is not held back by it.
func TestLimiterReleaseAfterFailedSend(t *testing.T) {
	l := NewLimiter(time.Minute)
	t0 := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	exit := issues.Issue{Kind: issues.KindUnexpectedExit, Process: "flaky:flaky"}
	if !l.Allow(exit, t0) {
		t.Fatal("first notification limited")
	}
	l.Release(exit, t0)
	if !l.Allow(exit, t0.Add(time.Second)) {
		t.Fatal("limited by a notification that was never shown")
	}
	// Releasing an older turn does not undo a newer one.
	l.Release(exit, t0)
	if l.Allow(exit, t0.Add(2*time.Second)) {
		t.Error("a stale release undid the newer turn")
	}
}

func TestComposeRepeat(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		count  int
		after  time.Duration
		detail string
	}{
		{2, 40 * time.Second, "Exited with status 2 · 2nd time in 40 seconds"},
		{3, 10 * time.Minute, "Exited with status 2 · 3rd time in 10 minutes"},
		{4, 61 * time.Second, "Exited with status 2 · 4th time in 2 minutes"},
		{11, 3 * time.Hour, "Exited with status 2 · 11th time in 3 hours"},
		{22, time.Hour, "Exited with status 2 · 22nd time in 1 hour"},
	}
	for _, tt := range tests {
		is := issues.Issue{ID: "x", Kind: issues.KindUnexpectedExit, Process: "flaky:flaky", Config: "/c/flaky.ini",
			Message: "Exited with status 2", OpenedAt: t0, LastAt: t0.Add(tt.after), Count: tt.count}
		m := Compose(is)
		if m.Detail != tt.detail || m.Body != tt.detail+" · flaky.ini" {
			t.Errorf("count %d: detail %q body %q", tt.count, m.Detail, m.Body)
		}
		if m.ID == is.ID {
			t.Errorf("count %d: a repeat reuses the issue's notification ID", tt.count)
		}
	}
	once := Compose(issues.Issue{ID: "y", Kind: issues.KindUnexpectedExit, Process: "a:a", Message: "Exited with status 1", Count: 1})
	if once.Detail != "Exited with status 1" || once.ID != "y" || once.Kind != issues.KindUnexpectedExit {
		t.Errorf("once = %+v", once)
	}
}
