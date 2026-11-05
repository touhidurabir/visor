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

// Each kind's banner: the severity icon before what happened, no
// subtitle, and one sentence that starts with the config file's name. The
// bell gets the same words without the icon and the file name.
func TestCompose(t *testing.T) {
	tests := []struct {
		name                string
		is                  issues.Issue
		title, body, detail string
		headline, thread    string
	}{
		{
			"can't start, supervisord's reason",
			issues.Issue{ID: "1", Kind: issues.KindFatal, Process: "reports-sync:reports-sync", Config: "/etc/conf.d/reports.ini", Message: "can't find command '/nonexistent/sync'"},
			"🔴 reports-sync can't start", "reports.ini: can't find command '/nonexistent/sync'.", "Can't find command '/nonexistent/sync'.",
			"reports-sync can't start", "/etc/conf.d/reports.ini",
		},
		{
			"can't start, Visor's fallback",
			issues.Issue{ID: "2", Kind: issues.KindFatal, Process: "mailer-digest:mailer-digest", Config: "/etc/conf.d/mailer.ini", Message: "Exited too quickly (process log may have details)"},
			"🔴 mailer-digest can't start", "mailer.ini: it exited too quickly (process log may have details).", "It exited too quickly (process log may have details).",
			"mailer-digest can't start", "/etc/conf.d/mailer.ini",
		},
		{
			"exited",
			issues.Issue{ID: "3", Kind: issues.KindUnexpectedExit, Process: "flaky:flaky", Config: "/c/flaky.ini", Message: "Exited with status 1"},
			"🟠 flaky exited unexpectedly", "flaky.ini: it exited with status 1.", "It exited with status 1.",
			"flaky exited unexpectedly", "/c/flaky.ini",
		},
		{
			"exited and restarted",
			issues.Issue{ID: "4", Kind: issues.KindUnexpectedExit, Process: "flaky:flaky", Config: "/c/flaky.ini", Message: "Exited with status 2 and was restarted by supervisord"},
			"🟠 flaky exited unexpectedly", "flaky.ini: it exited with status 2 and was restarted by supervisord.", "It exited with status 2 and was restarted by supervisord.",
			"flaky exited unexpectedly", "/c/flaky.ini",
		},
		{
			"terminated by a signal",
			issues.Issue{ID: "5", Kind: issues.KindUnexpectedExit, Process: "flaky:flaky", Config: "/c/flaky.ini", Message: "Was terminated by SIGKILL and restarted by supervisord"},
			"🟠 flaky exited unexpectedly", "flaky.ini: it was terminated by SIGKILL and restarted by supervisord.", "It was terminated by SIGKILL and restarted by supervisord.",
			"flaky exited unexpectedly", "/c/flaky.ini",
		},
		{
			"crash loop",
			issues.Issue{ID: "6", Kind: issues.KindCrashLoop, Process: "flaky:flaky", Config: "/c/flaky.ini", Message: "Restarted 5 times in 10 minutes"},
			"🔴 flaky keeps restarting", "flaky.ini: it was restarted 5 times in 10 minutes.", "It was restarted 5 times in 10 minutes.",
			"flaky keeps restarting", "/c/flaky.ini",
		},
		{
			"supervisord down, with the error",
			issues.Issue{ID: "7", Kind: issues.KindUnreachable, Message: "supervisord is not responding: dial unix /tmp/s.sock: connect: connection refused"},
			"🔴 supervisord isn't responding", "Visor can't reach it: dial unix /tmp/s.sock: connect: connection refused.", "Visor can't reach it: dial unix /tmp/s.sock: connect: connection refused.",
			"supervisord isn't responding", DaemonThread,
		},
		{
			"supervisord down, no error",
			issues.Issue{ID: "8", Kind: issues.KindUnreachable, Message: "supervisord is not responding"},
			"🔴 supervisord isn't responding", "Visor can't reach it.", "Visor can't reach it.",
			"supervisord isn't responding", DaemonThread,
		},
		{
			"supervisord turned down the password",
			issues.Issue{ID: "8a", Kind: issues.KindUnreachable, Message: "supervisord is not responding: supervisord: unauthorized"},
			"🔴 supervisord wants a password", "supervisord turned down the username and password Visor used.", "supervisord turned down the username and password Visor used.",
			"supervisord wants a password", DaemonThread,
		},
		{
			"supervisord down, other wording kept",
			issues.Issue{ID: "9", Kind: issues.KindUnreachable, Message: "supervisord said no"},
			"🔴 supervisord isn't responding", "supervisord said no.", "supervisord said no.",
			"supervisord isn't responding", DaemonThread,
		},
		{
			"a process named unlike its group keeps the full name",
			issues.Issue{ID: "10", Kind: issues.KindFatal, Process: "shop-queue:shop-queue_00", Config: "/c/shop.ini", Message: "Exited too quickly."},
			"🔴 shop-queue:shop-queue_00 can't start", "shop.ini: it exited too quickly.", "It exited too quickly.",
			"shop-queue:shop-queue_00 can't start", "/c/shop.ini",
		},
		{
			"no config file known: no prefix",
			issues.Issue{ID: "11", Kind: issues.KindUnexpectedExit, Process: "orphan:orphan", Message: "Exited with status 3"},
			"🟠 orphan exited unexpectedly", "It exited with status 3.", "It exited with status 3.",
			"orphan exited unexpectedly", DaemonThread,
		},
		{
			"no reason: the file alone",
			issues.Issue{ID: "12", Kind: issues.KindFatal, Process: "w:w", Config: "/c/w.ini"},
			"🔴 w can't start", "From w.ini.", "",
			"w can't start", "/c/w.ini",
		},
		{
			"another kind",
			issues.Issue{ID: "13", Kind: "something_new", Process: "w:w", Config: "/c/w.ini", Message: "odd"},
			"🔵 w has a problem", "w.ini: odd.", "Odd.",
			"w has a problem", "/c/w.ini",
		},
	}
	for _, tt := range tests {
		m := Compose(tt.is)
		if m.Title != tt.title || m.Body != tt.body || m.Detail != tt.detail || m.Headline != tt.headline {
			t.Errorf("%s:\n got %q / %q / %q / %q\nwant %q / %q / %q / %q", tt.name,
				m.Title, m.Body, m.Detail, m.Headline, tt.title, tt.body, tt.detail, tt.headline)
		}
		if m.Subtitle != "" {
			t.Errorf("%s: subtitle %q, want none", tt.name, m.Subtitle)
		}
		if m.Thread != tt.thread {
			t.Errorf("%s: thread %q, want %q", tt.name, m.Thread, tt.thread)
		}
		if m.ID != tt.is.ID || m.IssueID != tt.is.ID || m.Process != tt.is.Process || m.Config != tt.is.Config || m.Kind != tt.is.Kind {
			t.Errorf("%s: ids = %+v", tt.name, m)
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
		{2, 40 * time.Second, "it exited with status 2, the 2nd time in 40 seconds"},
		{3, 10 * time.Minute, "it exited with status 2, the 3rd time in 10 minutes"},
		{4, 61 * time.Second, "it exited with status 2, the 4th time in 2 minutes"},
		{11, 3 * time.Hour, "it exited with status 2, the 11th time in 3 hours"},
		{22, time.Hour, "it exited with status 2, the 22nd time in 1 hour"},
	}
	for _, tt := range tests {
		is := issues.Issue{ID: "x", Kind: issues.KindUnexpectedExit, Process: "flaky:flaky", Config: "/c/flaky.ini",
			Message: "Exited with status 2", OpenedAt: t0, LastAt: t0.Add(tt.after), Count: tt.count}
		m := Compose(is)
		if m.Body != "flaky.ini: "+tt.detail+"." || m.Detail != upperFirst(tt.detail)+"." {
			t.Errorf("count %d: detail %q body %q", tt.count, m.Detail, m.Body)
		}
		if m.ID == is.ID {
			t.Errorf("count %d: a repeat reuses the issue's notification ID", tt.count)
		}
	}
	once := Compose(issues.Issue{ID: "y", Kind: issues.KindUnexpectedExit, Process: "a:a", Message: "Exited with status 1", Count: 1})
	if once.Detail != "It exited with status 1." || once.ID != "y" || once.Kind != issues.KindUnexpectedExit {
		t.Errorf("once = %+v", once)
	}
	// A repeat with no reason is the count alone.
	bare := Compose(issues.Issue{ID: "z", Kind: issues.KindFatal, Process: "w:w", Config: "/c/w.ini", OpenedAt: t0, LastAt: t0.Add(time.Minute), Count: 2})
	if bare.Body != "w.ini: the 2nd time in 1 minute." || bare.Detail != "The 2nd time in 1 minute." {
		t.Errorf("bare repeat = %q / %q", bare.Body, bare.Detail)
	}
}

// The severity is the icon before the title, with no word and no line of
// its own.
func TestComposeSaysHowSerious(t *testing.T) {
	for kind, want := range map[issues.Kind]string{
		issues.KindFatal:          "🔴",
		issues.KindCrashLoop:      "🔴",
		issues.KindUnreachable:    "🔴",
		issues.KindUnexpectedExit: "🟠",
		"something_new":           "🔵",
	} {
		if got := SeverityOf(kind).Icon(); got != want {
			t.Errorf("%s: icon %q, want %q", kind, got, want)
		}
		m := Compose(issues.Issue{ID: "x", Kind: kind, Process: "a:a"})
		if m.Title != want+" "+m.Headline || m.Subtitle != "" {
			t.Errorf("%s: title %q subtitle %q", kind, m.Title, m.Subtitle)
		}
	}
}

func TestEntrySeverity(t *testing.T) {
	if s := (Entry{Kind: issues.KindUnexpectedExit}).Level(); s != Warning {
		t.Errorf("old entry by kind = %s", s)
	}
	if s := (Entry{Kind: issues.KindUnreachable, Severity: Info}).Level(); s != Info {
		t.Errorf("recovery note = %s, want info", s)
	}
}
