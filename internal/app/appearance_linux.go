//go:build linux && !server

package app

/*
#cgo pkg-config: gtk4
#include <gtk/gtk.h>

// visorPreferDark sets GTK's dark preference, which the title bar and
// WebKit's prefers-color-scheme follow. Call it on the main thread.
static void visorPreferDark(int dark) {
	GtkSettings *s = gtk_settings_get_default();
	if (s != NULL) {
		g_object_set(s, "gtk-application-prefer-dark-theme", dark ? TRUE : FALSE, NULL);
	}
}
*/
import "C"

import (
	"sync"

	"github.com/godbus/dbus/v5"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// GTK 4 without libadwaita doesn't follow GNOME's or Plasma's dark
// setting, so Visor sets GTK's preference itself: from Settings, or for
// "system" from the desktop portal (org.freedesktop.appearance
// color-scheme), which it keeps watching.
var linuxAppearance struct {
	mu     sync.Mutex
	choice string
	watch  sync.Once
}

func setNativeAppearance(appearance string) {
	if application.Get() == nil {
		return
	}
	linuxAppearance.mu.Lock()
	linuxAppearance.choice = appearance
	linuxAppearance.mu.Unlock()
	linuxAppearance.watch.Do(func() { go watchColorScheme() })
	applyLinuxAppearance()
}

func applyLinuxAppearance() {
	linuxAppearance.mu.Lock()
	choice := linuxAppearance.choice
	linuxAppearance.mu.Unlock()
	dark := C.int(0)
	if preferDark(choice, systemPrefersDark()) {
		dark = 1
	}
	application.InvokeAsync(func() { C.visorPreferDark(dark) })
}

const (
	portalName = "org.freedesktop.portal.Desktop"
	portalPath = "/org/freedesktop/portal/desktop"
	portalIfc  = "org.freedesktop.portal.Settings"
)

// systemPrefersDark asks the settings portal; without one, light.
func systemPrefersDark() bool {
	conn, err := dbus.SessionBus()
	if err != nil {
		return false
	}
	obj := conn.Object(portalName, portalPath)
	var v dbus.Variant
	if obj.Call(portalIfc+".ReadOne", 0, "org.freedesktop.appearance", "color-scheme").Store(&v) == nil {
		return portalDark(v)
	}
	if obj.Call(portalIfc+".Read", 0, "org.freedesktop.appearance", "color-scheme").Store(&v) == nil {
		return portalDark(v)
	}
	return false
}

// watchColorScheme re-applies "system" when the desktop switches.
func watchColorScheme() {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return
	}
	if err := conn.AddMatchSignal(dbus.WithMatchInterface(portalIfc), dbus.WithMatchMember("SettingChanged")); err != nil {
		return
	}
	ch := make(chan *dbus.Signal, 8)
	conn.Signal(ch)
	for sig := range ch {
		if len(sig.Body) >= 2 && sig.Body[0] == "org.freedesktop.appearance" && sig.Body[1] == "color-scheme" {
			applyLinuxAppearance()
		}
	}
}
