//go:build linux && !server

package app

import (
	"context"
	"log"
	"slices"
	"sync/atomic"

	"github.com/godbus/dbus/v5"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"

	"github.com/touhidurabir/visor/internal/notify"
)

// linuxNotifier shows notifications through the desktop's notification
// server (org.freedesktop.Notifications: GNOME Shell, Plasma, …), by way of
// Wails' notifications package. Linux asks no permission; the server is
// either there or not.
type linuxNotifier struct {
	ns    *notifications.NotificationService
	ready atomic.Bool // ns started; set once, after ns
	// hasServer says a notification server answers on the session bus.
	hasServer func() bool
	// markup: the server reads bodies as markup (GetCapabilities lists
	// body-markup), so they are escaped.
	markup atomic.Bool
}

func newNativeNotifier() nativeNotifier { return &linuxNotifier{hasServer: notificationServerRunning} }

func (n *linuxNotifier) start(ctx context.Context, onClick func(route string)) {
	// Wails' Linux notifier reads the app's name at startup; without a
	// running app (tests) notifications stay off.
	if application.Get() == nil {
		log.Print("visor: notifications are off: no app is running")
		return
	}
	ns := notifications.New()
	if err := ns.ServiceStartup(ctx, application.ServiceOptions{}); err != nil {
		log.Printf("visor: notifications are off: %v", err)
		return
	}
	ns.OnNotificationResponse(func(r notifications.NotificationResult) {
		if r.Error != nil {
			log.Printf("visor: notification click: %v", r.Error)
			return
		}
		// GNOME and Plasma send the "default" action for a click on the
		// banner; Wails maps it to DefaultActionIdentifier.
		if r.Response.ActionIdentifier == notifications.DefaultActionIdentifier && onClick != nil {
			go onClick(routeOf(r.Response.UserInfo))
		}
	})
	n.markup.Store(serverHasCapability("body-markup"))
	n.ns = ns
	n.ready.Store(true)
}

// serverHasCapability asks the notification server (GetCapabilities).
func serverHasCapability(c string) bool {
	conn, err := dbus.SessionBus()
	if err != nil {
		return false
	}
	var caps []string
	if conn.Object("org.freedesktop.Notifications", "/org/freedesktop/Notifications").
		Call("org.freedesktop.Notifications.GetCapabilities", 0).Store(&caps) != nil {
		return false
	}
	return slices.Contains(caps, c)
}

func (n *linuxNotifier) send(m notify.Message) error {
	if !n.ready.Load() || !n.hasServer() {
		return notify.ErrUnavailable
	}
	return n.ns.SendNotification(linuxOptions(m, n.markup.Load()))
}

func (n *linuxNotifier) status() string {
	if n.ready.Load() && n.hasServer() {
		return NotifyAllowed
	}
	return NotifyUnavailable
}

// notificationServerRunning asks the session bus whether anything owns
// org.freedesktop.Notifications, or would start for it.
func notificationServerRunning() bool {
	conn, err := dbus.SessionBus()
	if err != nil {
		return false
	}
	const name = "org.freedesktop.Notifications"
	var owned bool
	if conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, name).Store(&owned) == nil && owned {
		return true
	}
	var activatable []string
	if conn.BusObject().Call("org.freedesktop.DBus.ListActivatableNames", 0).Store(&activatable) != nil {
		return false
	}
	for _, a := range activatable {
		if a == name {
			return true
		}
	}
	return false
}
