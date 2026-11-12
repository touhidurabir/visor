//go:build linux && !server

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/godbus/dbus/v5"

	"github.com/touhidurabir/visor/internal/platform"
)

// guardWebKitSandbox stops before any window when Ubuntu's AppArmor would
// crash it (platform.WebKitSandboxBlocked): it says why and how to fix it,
// in the terminal, in a file in the state folder and in a notification,
// instead of crashing with nothing on screen. The .deb's profile prevents
// this; an AppImage or a binary built by hand needs one added.
func guardWebKitSandbox(stateDir string) {
	if !platform.WebKitSandboxBlocked() {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		exe = "/path/to/visor"
	} else if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	text := "Visor can't open its window: this system's AppArmor stops WebKit's sandbox\n" +
		"(kernel.apparmor_restrict_unprivileged_userns=1), and no AppArmor profile lets Visor use it.\n" +
		"The .deb package installs one. For this copy, add it by running:\n\n" + platform.AppArmorFix(exe) +
		"\nThen open Visor again.\n"
	fmt.Fprint(os.Stderr, text)
	file := filepath.Join(stateDir, "apparmor-fix.txt")
	if os.MkdirAll(stateDir, 0o700) == nil && os.WriteFile(file, []byte(text), 0o600) == nil {
		notifyBlocked("The steps to fix it are in " + file + ".")
	} else {
		notifyBlocked("Run Visor from a terminal to see how to fix it.")
	}
	os.Exit(1)
}

// notifyBlocked shows a desktop notification without Wails (no app runs yet).
func notifyBlocked(how string) {
	conn, err := dbus.SessionBus()
	if err != nil {
		return
	}
	conn.Object("org.freedesktop.Notifications", "/org/freedesktop/Notifications").Call(
		"org.freedesktop.Notifications.Notify", 0, "Visor", uint32(0), "dialog-warning",
		"Visor can't open its window",
		"AppArmor blocks the sandbox Visor's window needs. "+how,
		// Transient: the message is about this launch only; it shouldn't
		// stay in the notification list and come back with later launches.
		[]string{}, map[string]dbus.Variant{"transient": dbus.MakeVariant(true)}, int32(-1))
}
