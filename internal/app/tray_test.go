package app

import (
	"testing"

	"github.com/touhidurabir/visor/internal/monitor"
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
