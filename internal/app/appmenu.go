package app

import (
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// EventMenu carries a menu-bar choice the frontend carries out itself,
// because it owns the confirm dialogs and result panels: "find",
// "reload" (check supervisord's config for changes) and "restart-all".
const EventMenu = "visor:menu"

func init() { application.RegisterEvent[string](EventMenu) }

// menuItem is one entry of the menu bar. A Role item is a standard macOS
// item that Wails builds; otherwise Action says what it does:
// "route:<hash>" shows the main window there, "menu:<name>" shows it and
// sends EventMenu, "open:<url>" opens the browser.
type menuItem struct {
	Label, Accel string
	Role         application.Role
	Action       string
	Separator    bool
}

type menuSection struct {
	Title string
	Items []menuItem
}

const (
	repoURL  = "https://github.com/touhidurabir/visor"
	routeNew = "#/new"
	routeDoc = "#/doctor"
)

var sep = menuItem{Separator: true}

func role(r application.Role) menuItem { return menuItem{Role: r} }

// appMenuSpec is Visor's menu bar. It replaces Wails' default, whose View
// menu reloads the web view (losing unsaved edits) and whose Help menu
// loads wails.io into the app window.
func appMenuSpec() []menuSection {
	return []menuSection{
		{"Visor", []menuItem{
			role(application.About), sep,
			{Label: "Preferences…", Accel: "CmdOrCtrl+,", Action: "route:" + RouteSettings},
			{Label: "Doctor", Action: "route:" + routeDoc}, sep,
			role(application.ServicesMenu), sep,
			role(application.Hide), role(application.HideOthers), role(application.UnHide), sep,
			role(application.Quit),
		}},
		{"File", []menuItem{
			{Label: "New Config…", Accel: "CmdOrCtrl+N", Action: "route:" + routeNew}, sep,
			// Closes a server's tab, or the window on this computer's tab
			// (the page decides: it knows which tab is on screen).
			{Label: "Close", Accel: "CmdOrCtrl+W", Action: "menu:close"},
		}},
		{"Edit", []menuItem{
			role(application.Undo), role(application.Redo), sep,
			role(application.Cut), role(application.Copy), role(application.Paste), role(application.SelectAll),
		}},
		{"View", []menuItem{
			{Label: "Dashboard", Accel: "CmdOrCtrl+1", Action: "route:" + RouteMain},
			{Label: "Issues", Accel: "CmdOrCtrl+2", Action: "route:" + RouteIssues}, sep,
			{Label: "Find", Accel: "CmdOrCtrl+F", Action: "menu:find"}, sep,
			role(application.ResetZoom), role(application.ZoomIn), role(application.ZoomOut), sep,
			role(application.ToggleFullscreen),
		}},
		{"Supervisor", []menuItem{
			{Label: "Reload Config", Accel: "CmdOrCtrl+R", Action: "menu:reload"},
			{Label: "Restart All…", Action: "menu:restart-all"},
		}},
		{"Window", []menuItem{
			role(application.Minimise), role(application.Zoom), sep, role(application.Front),
		}},
		{"Help", []menuItem{
			{Label: "Visor Help", Action: "open:" + repoURL + "#readme"},
			{Label: "Report an Issue", Action: "open:" + repoURL + "/issues/new/choose"}, sep,
			{Label: "Doctor", Action: "route:" + routeDoc},
		}},
	}
}

// dispatchMenu carries out a menu item's Action.
func dispatchMenu(action string, show func(route string), emit func(name string), open func(url string)) {
	kind, arg, _ := strings.Cut(action, ":")
	switch kind {
	case "route":
		show(arg)
	case "menu":
		if arg != "close" { // closing must not first bring a hidden window back
			show("")
		}
		emit(arg)
	case "open":
		open(arg)
	}
}

// BuildAppMenu turns appMenuSpec into the native menu bar. It needs the
// running app (roles read its name), so it is checked by hand, not tested.
func BuildAppMenu(a *application.App, s *Shell) *application.Menu {
	bar := a.NewMenu()
	for _, sec := range appMenuSpec() {
		m := bar.AddSubmenu(sec.Title)
		for _, it := range sec.Items {
			switch {
			case it.Separator:
				m.AddSeparator()
			case it.Role != application.NoRole:
				m.AddRole(it.Role)
			default:
				action := it.Action
				item := m.Add(it.Label).OnClick(func(*application.Context) {
					dispatchMenu(action, s.ShowMain,
						func(name string) { WailsEmit(EventMenu, name) },
						func(url string) { _ = a.Browser.OpenURL(url) })
				})
				if it.Accel != "" {
					item.SetAccelerator(it.Accel)
				}
			}
		}
	}
	return bar
}
