package app

import (
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/services/dock"

	"github.com/touhidurabir/visor/internal/monitor"
	"github.com/touhidurabir/visor/internal/platform"
	"github.com/touhidurabir/visor/internal/trayicon"
)

// Frontend routes the native shell opens.
const (
	RouteMain     = "#/"
	RouteSettings = "#/settings"
	RouteIssues   = "#/issues"
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

	unread     int                   // unread notifications, shown beside the icon
	countOff   bool                  // Settings: no count beside the icon
	drawnLabel string                // what drawUnread last put beside the icon
	notifItem  *application.MenuItem // the tray menu's "Notifications (N)"

	login     platform.LoginItem    // nil: the tray's "Launch at login" stays disabled
	loginItem *application.MenuItem // that check item
	trayMenu  *application.Menu

	quitting atomic.Bool
}

// NewShell returns a Shell that tracks health until Setup.
func NewShell() *Shell { return &Shell{} }

// UseLoginItem gives the tray menu's "Launch at login" its switch. Call it
// before Setup.
func (s *Shell) UseLoginItem(li platform.LoginItem) { s.login = li }

// ShowLoginStatus updates the tray's check mark (after Settings or Doctor
// changed launch at login).
func (s *Shell) ShowLoginStatus(st platform.LoginItemStatus) {
	s.mu.Lock()
	item, menu := s.loginItem, s.trayMenu
	s.mu.Unlock()
	if item == nil {
		return
	}
	item.SetChecked(loginView(st).On)
	item.SetEnabled(st != platform.LoginUnsupported)
	if menu != nil {
		menu.Update()
	}
}

// Emit wraps next (the function that sends events to the frontend) so that
// every overview also updates the menu-bar icon. A nil next only updates
// the icon.
func (s *Shell) Emit(next func(name string, data any)) func(name string, data any) {
	return func(name string, data any) {
		if next != nil {
			next(name, data)
		}
		if name == EventSettings {
			if v, ok := data.(SettingsView); ok {
				s.ShowMenuBarCount(v.Settings.MenuBarCount)
			}
			return
		}
		if name == EventNotifications {
			if v, ok := data.(NotificationsView); ok {
				s.mu.Lock()
				s.unread = v.Unread
				started := s.started
				s.mu.Unlock()
				if started {
					application.InvokeAsync(s.drawUnread)
				}
			}
			return
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

// trayLabel is the text beside the menu-bar icon: the unread count.
func trayLabel(unread int) string {
	switch {
	case unread <= 0:
		return ""
	case unread > 99:
		return "99+"
	}
	return strconv.Itoa(unread)
}

func (s *Shell) unreadLabel() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return trayLabel(s.unread)
}

// iconLabel is the text beside the menu-bar icon: the unread count,
// unless Settings turned it off.
func (s *Shell) iconLabel() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.countOff {
		return ""
	}
	return trayLabel(s.unread)
}

// ShowMenuBarCount turns the count beside the menu-bar icon on or off (the
// right-click menu's "Notifications (N)" keeps it either way).
func (s *Shell) ShowMenuBarCount(on bool) {
	s.mu.Lock()
	changed := s.countOff == on
	s.countOff = !on
	started := s.started
	s.mu.Unlock()
	if changed && started {
		application.InvokeAsync(s.drawUnread)
	}
}

// drawUnread shows the unread count beside the icon and in the tray menu.
// Main thread only.
func (s *Shell) drawUnread() {
	label, icon := s.unreadLabel(), s.iconLabel()
	s.mu.Lock()
	tray, item, menu := s.tray, s.notifItem, s.trayMenu
	s.drawnLabel = icon
	s.mu.Unlock()
	if tray != nil {
		tray.SetLabel(icon)
	}
	if item != nil {
		text := "Notifications"
		if label != "" {
			text += " (" + label + ")"
		}
		item.SetLabel(text)
		if menu != nil {
			menu.Update()
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
		// A non-activating floating panel that joins every Space and may sit
		// over full-screen apps, so the popover opens where the menu bar is.
		Mac: application.MacWindow{
			WindowClass:      application.MacWindowClassPanel,
			PanelPreferences: application.MacPanelPreferences{FloatingPanel: true, NonActivating: true},
			CollectionBehavior: application.MacWindowCollectionBehaviorCanJoinAllSpaces |
				application.MacWindowCollectionBehaviorFullScreenAuxiliary,
		},
	})

	menu := a.NewMenu()
	menu.Add("Open Visor").OnClick(func(*application.Context) { s.ShowMain("") })
	menu.Add("Preferences…").OnClick(func(*application.Context) { s.ShowMain(RouteSettings) })
	notifItem := menu.Add("Notifications").OnClick(func(*application.Context) {
		s.ShowMain("")
		WailsEmit(EventMenu, "notifications") // the bell opens its panel
	})
	menu.AddSeparator()
	loginItem := menu.AddCheckbox("Launch at login", false)
	if li := s.login; li != nil {
		loginItem.OnClick(func(ctx *application.Context) {
			err := li.Set(ctx.ClickedMenuItem().Checked())
			st := li.Status()
			s.ShowLoginStatus(st)
			WailsEmit(EventLoginItem, loginView(st)) // an open Settings page follows
			if err != nil {
				// Settings says why (e.g. move Visor to Applications first).
				s.ShowMain(RouteSettings)
			}
		})
	} else {
		loginItem.SetEnabled(false)
	}
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
	s.loginItem, s.trayMenu, s.notifItem = loginItem, menu, notifItem
	s.mu.Unlock()
	s.drawUnread()
	if s.login != nil {
		s.ShowLoginStatus(s.login.Status())
	}

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
		s.appStarted(application.InvokeAsync)
	})
}

// appStarted lets events draw from now on and draws what arrived while the
// app was starting (services start before ApplicationStarted, so the
// health and the unread count sent at launch weren't drawn yet).
func (s *Shell) appStarted(onMain func(func())) {
	s.mu.Lock()
	s.started = true
	s.mu.Unlock()
	onMain(func() {
		s.drawIcon()
		s.drawUnread()
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
	main.UnMinimise() // Show alone leaves a minimised window in the Dock
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

// OpenNotifications shows the main window with its notification list open.
func (s *ShellService) OpenNotifications() {
	s.shell.ShowMain("")
	WailsEmit(EventMenu, "notifications")
}

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
