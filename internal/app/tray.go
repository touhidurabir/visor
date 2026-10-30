package app

import (
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/services/dock"

	"github.com/touhidurabir/visor/internal/monitor"
	"github.com/touhidurabir/visor/internal/trayicon"
)

// Frontend routes the native shell opens.
const (
	RouteMain     = "#/"
	RouteSettings = "#/settings"
	routeTray     = "#/tray"
)

// Popover size, in points.
const (
	popoverWidth  = 360
	popoverHeight = 560
	popoverOffset = 5 // gap below the menu bar
)

// Shell is Visor's native presence: the menu-bar icon with its popover and
// right-click menu, the main window, and the Dock icon, which shows only
// while the main window does.
//
// Create it before the Runtime so the Runtime's events can go through
// Emit, then call Setup once the Wails app and main window exist.
type Shell struct {
	icon healthIcon

	mu      sync.Mutex
	app     *application.App
	tray    *application.SystemTray
	popover *application.WebviewWindow
	main    *application.WebviewWindow
	dock    *dock.DockService
	started bool // the app is running, so the tray may be changed

	quitting atomic.Bool
}

// NewShell returns a Shell that tracks health until Setup.
func NewShell() *Shell { return &Shell{} }

// Emit wraps next (the function that sends events to the frontend) so that
// every overview also updates the menu-bar icon. A nil next only updates
// the icon.
func (s *Shell) Emit(next func(name string, data any)) func(name string, data any) {
	return func(name string, data any) {
		if next != nil {
			next(name, data)
		}
		if name != EventOverview {
			return
		}
		ov, ok := data.(OverviewView)
		if !ok {
			return
		}
		s.icon.set(parseHealth(ov.Server.Health))
		s.mu.Lock()
		started := s.started
		s.mu.Unlock()
		if started {
			// Never block the Runtime on the main thread: it may be
			// waiting for the Runtime to stop.
			application.InvokeAsync(s.drawIcon)
		}
	}
}

// Setup creates the menu-bar icon, its popover and menu, and makes closing
// the main window hide it (and the Dock icon) instead of quitting. In
// server mode (no native GUI) it does nothing.
func (s *Shell) Setup(a *application.App, main *application.WebviewWindow, d *dock.DockService) {
	if application.System.IsServer() {
		return
	}
	popover := a.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:            "tray",
		Title:           "Visor",
		Width:           popoverWidth,
		Height:          popoverHeight,
		Frameless:       true,
		AlwaysOnTop:     true,
		Hidden:          true,
		DisableResize:   true,
		HideOnEscape:    true,
		HideOnFocusLost: true,
		URL:             "/" + routeTray,
	})

	menu := a.NewMenu()
	menu.Add("Open Visor").OnClick(func(*application.Context) { s.ShowMain("") })
	menu.Add("Preferences…").OnClick(func(*application.Context) { s.ShowMain(RouteSettings) })
	menu.AddSeparator()
	// Wired up in Task 27 (launch at login).
	menu.AddCheckbox("Launch at login", false).SetEnabled(false)
	menu.AddSeparator()
	menu.Add("Quit").OnClick(func(*application.Context) { a.Quit() })

	tray := a.SystemTray.New()
	tray.SetTooltip("Visor")
	h, _ := s.icon.take()
	tray.SetTemplateIcon(trayicon.Template(h)) // before Run: no race with the tray starting
	tray.SetMenu(menu)
	tray.AttachWindow(popover).WindowOffset(popoverOffset)

	s.mu.Lock()
	s.app, s.tray, s.popover, s.main, s.dock = a, tray, popover, main, d
	s.mu.Unlock()

	a.OnShutdown(func() { s.quitting.Store(true) })
	hideOnClose := func(w *application.WebviewWindow, hideDock bool) {
		w.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
			if s.quitting.Load() {
				return // let Quit close every window
			}
			e.Cancel()
			w.Hide()
			if hideDock && d != nil {
				d.HideAppIcon()
			}
		})
	}
	hideOnClose(main, true)
	hideOnClose(popover, false)

	// Opening Visor again (Finder, Spotlight) or clicking its Dock icon
	// shows the main window. Wails' default would show every hidden
	// window, popover included, without the Dock icon.
	a.Event.RegisterApplicationEventHook(events.Mac.ApplicationShouldHandleReopen, func(e *application.ApplicationEvent) {
		e.Cancel()
		s.ShowMain("")
	})

	a.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		s.mu.Lock()
		s.started = true
		s.mu.Unlock()
		application.InvokeAsync(s.drawIcon) // health may have changed while starting
	})
}

// drawIcon redraws the menu-bar icon if the health changed since the last
// draw. It runs on the main thread.
func (s *Shell) drawIcon() {
	s.mu.Lock()
	tray := s.tray
	s.mu.Unlock()
	if tray == nil {
		return
	}
	if h, redraw := s.icon.take(); redraw {
		tray.SetTemplateIcon(trayicon.Template(h))
	}
}

// ShowMain shows the main window with the Dock icon and brings Visor to
// the front. A non-empty route (RouteMain, RouteSettings) navigates the
// main window there.
func (s *Shell) ShowMain(route string) {
	if application.System.IsServer() {
		return
	}
	s.mu.Lock()
	main, popover, d := s.main, s.popover, s.dock
	s.mu.Unlock()
	if main == nil {
		return
	}
	if popover != nil {
		popover.Hide()
	}
	if d != nil {
		d.ShowAppIcon()
	}
	main.Show().Focus()
	if route != "" {
		main.ExecJS("window.location.hash = " + strconv.Quote(route))
	}
}

// ShellService lets the frontend drive the native shell.
type ShellService struct {
	shell *Shell
}

// NewShellService returns a service backed by s.
func NewShellService(s *Shell) *ShellService { return &ShellService{shell: s} }

// OpenVisor shows the main window (and the Dock icon) and hides the
// menu-bar popover.
func (s *ShellService) OpenVisor() { s.shell.ShowMain("") }

// healthIcon remembers the latest health and the one last drawn, because
// trayicon.Template renders a fresh image on every call. The zero value
// holds HealthUnknown with nothing drawn.
type healthIcon struct {
	mu       sync.Mutex
	current  monitor.Health
	seen     bool // current was set
	drawn    monitor.Health
	hasDrawn bool
}

func (h *healthIcon) set(health monitor.Health) {
	h.mu.Lock()
	h.current, h.seen = health, true
	h.mu.Unlock()
}

// take returns the health to draw and true if it differs from the last
// drawn one (or nothing has been drawn), and records it as drawn.
func (h *healthIcon) take() (monitor.Health, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	cur := monitor.HealthUnknown
	if h.seen {
		cur = h.current
	}
	if h.hasDrawn && h.drawn == cur {
		return cur, false
	}
	h.drawn, h.hasDrawn = cur, true
	return cur, true
}

// parseHealth turns ServerView.Health back into a monitor.Health; anything
// unrecognised is HealthUnknown.
func parseHealth(s string) monitor.Health {
	for _, h := range []monitor.Health{monitor.HealthOK, monitor.HealthWarning, monitor.HealthError} {
		if h.String() == s {
			return h
		}
	}
	return monitor.HealthUnknown
}
