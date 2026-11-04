package app

import (
	"testing"

	"github.com/touhidurabir/visor/internal/monitor"
	"github.com/touhidurabir/visor/internal/settings"
)

func TestParseHealth(t *testing.T) {
	for _, h := range []monitor.Health{monitor.HealthOK, monitor.HealthWarning, monitor.HealthError, monitor.HealthUnknown} {
		if got := parseHealth(h.String()); got != h {
			t.Errorf("parseHealth(%q) = %v, want %v", h.String(), got, h)
		}
	}
	if got := parseHealth("bogus"); got != monitor.HealthUnknown {
		t.Errorf("parseHealth(bogus) = %v, want unknown", got)
	}
}

// The tray icon is re-rendered on every Template call, so it must be
// redrawn only when the health it shows changes.
func TestHealthIconRedrawsOnlyOnChange(t *testing.T) {
	var hi healthIcon
	step := func(want monitor.Health, wantRedraw bool) {
		t.Helper()
		got, redraw := hi.take()
		if redraw != wantRedraw || (redraw && got != want) {
			t.Fatalf("take() = %v, %v; want %v, %v", got, redraw, want, wantRedraw)
		}
	}
	step(monitor.HealthUnknown, true) // nothing drawn yet
	step(0, false)
	hi.set(monitor.HealthOK)
	step(monitor.HealthOK, true)
	hi.set(monitor.HealthOK)
	step(0, false)
	hi.set(monitor.HealthError)
	hi.set(monitor.HealthWarning) // only the latest counts
	step(monitor.HealthWarning, true)
	hi.set(monitor.HealthWarning)
	step(0, false)
}

func TestShellEmitForwardsAndTracksHealth(t *testing.T) {
	s := NewShell()
	var names []string
	emit := s.Emit(func(name string, _ any) { names = append(names, name) })

	if _, redraw := s.icon.take(); !redraw { // initial unknown icon
		t.Fatal("first take should draw")
	}
	emit(EventIssueOpened, IssueView{ID: "x"})
	if _, redraw := s.icon.take(); redraw {
		t.Error("a non-overview event changed the icon")
	}
	emit(EventOverview, OverviewView{Server: ServerView{Health: "error"}})
	if h, redraw := s.icon.take(); !redraw || h != monitor.HealthError {
		t.Errorf("after an error overview take() = %v, %v", h, redraw)
	}
	emit(EventOverview, OverviewView{Server: ServerView{Health: "error"}})
	if _, redraw := s.icon.take(); redraw {
		t.Error("same health redrew the icon")
	}
	if len(names) != 3 || names[0] != EventIssueOpened || names[1] != EventOverview {
		t.Errorf("forwarded %v", names)
	}
}

func TestShellEmitNilNext(t *testing.T) {
	s := NewShell()
	s.Emit(nil)(EventOverview, OverviewView{Server: ServerView{Health: "ok"}})
	s.icon.take() // must not panic
}

func TestTrayLabelShowsUnread(t *testing.T) {
	for n, want := range map[int]string{0: "", 3: "3", 99: "99", 120: "99+"} {
		if got := trayLabel(n); got != want {
			t.Errorf("trayLabel(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestTrayLabelFollowsInbox(t *testing.T) {
	s := NewShell()
	emit := s.Emit(nil)
	emit(EventNotifications, NotificationsView{Unread: 3})
	if got := s.unreadLabel(); got != "3" {
		t.Fatalf("after 3 unread: %q", got)
	}
	emit(EventNotifications, NotificationsView{Unread: 0}) // Mark all as read
	if got := s.unreadLabel(); got != "" {
		t.Fatalf("after mark all read: %q", got)
	}
}

func TestMenuBarCountFollowsSettings(t *testing.T) {
	s := NewShell()
	emit := s.Emit(nil)
	emit(EventNotifications, NotificationsView{Unread: 3})
	if got := s.iconLabel(); got != "3" {
		t.Fatalf("count on: %q", got)
	}
	off := settings.Defaults()
	off.MenuBarCount = false
	emit(EventSettings, SettingsView{Settings: off})
	if got := s.iconLabel(); got != "" {
		t.Errorf("count off: icon label %q, want none", got)
	}
	if got := s.unreadLabel(); got != "3" {
		t.Errorf("the menu item keeps the count: %q", got)
	}
	s.ShowMenuBarCount(true)
	if got := s.iconLabel(); got != "3" {
		t.Errorf("count back on: %q", got)
	}
}

// The count sent while services start (before ApplicationStarted) must be
// drawn once the app has started, not wait for the next inbox change.
func TestUnreadSentAtLaunchIsDrawnWhenStarted(t *testing.T) {
	s := NewShell()
	s.Emit(nil)(EventNotifications, NotificationsView{Unread: 3}) // not started: saved only
	if s.drawnLabel != "" {
		t.Fatalf("drawn before start: %q", s.drawnLabel)
	}
	s.appStarted(func(f func()) { f() })
	if s.drawnLabel != "3" {
		t.Errorf("after start the icon label is %q, want 3", s.drawnLabel)
	}
}

func TestOpenDoctorShowsTheMainWindowThere(t *testing.T) {
	var shown, emitted []string
	show := func(r string) { shown = append(shown, r) }
	emit := func(n string) { emitted = append(emitted, n) }

	openDoctor(false, show, emit)
	if len(shown) != 1 || shown[0] != "#/doctor" || len(emitted) != 0 {
		t.Fatalf("app: shown %q, emitted %q; want shown [#/doctor], nothing emitted", shown, emitted)
	}

	// The server build has no window, so the open pages go there themselves.
	shown, emitted = nil, nil
	openDoctor(true, show, emit)
	if len(shown) != 0 || len(emitted) != 1 || emitted[0] != "doctor" {
		t.Fatalf("server: shown %q, emitted %q; want nothing shown, emitted [doctor]", shown, emitted)
	}
}
