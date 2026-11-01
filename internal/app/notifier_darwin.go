//go:build darwin && !server

package app

import (
	"context"
	"log"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"

	"github.com/touhidurabir/visor/internal/notify"
)

// macNotifier shows notifications through macOS' notification centre, by
// way of Wails' notifications package. It works only inside Visor.app:
// macOS gives a bare binary no bundle identifier, and the notification
// centre refuses (or crashes) without one, so start leaves it off there.
type macNotifier struct {
	ns    *notifications.NotificationService
	ready atomic.Bool // ns started; set once, after ns
}

func newNativeNotifier() nativeNotifier { return &macNotifier{} }

func (n *macNotifier) start(ctx context.Context, onClick func()) {
	ns := notifications.New()
	// Wails' own startup checks: macOS 11+ and a bundle identifier. Its
	// error would stop the app if Wails ran it as a service.
	if err := ns.ServiceStartup(ctx, application.ServiceOptions{}); err != nil {
		log.Printf("visor: notifications are off: %v", err)
		return
	}
	ns.OnNotificationResponse(func(r notifications.NotificationResult) {
		if r.Error != nil {
			log.Printf("visor: notification click: %v", r.Error)
			return
		}
		if r.Response.ActionIdentifier == notifications.DefaultActionIdentifier && onClick != nil {
			go onClick() // off the notification centre's callback
		}
	})
	n.ns = ns
	n.ready.Store(true)
	// Ask once at launch. macOS prompts only the first time; afterwards
	// this returns the person's choice. It may wait minutes for an answer.
	go func() {
		granted, err := ns.RequestNotificationAuthorization()
		if err != nil {
			log.Printf("visor: notification permission: %v", err)
			return
		}
		log.Printf("visor: notification permission: allowed=%v", granted)
	}()
}

func (n *macNotifier) send(m notify.Message) error {
	if !n.ready.Load() {
		return notify.ErrUnavailable
	}
	// macOS accepts a notification while permission is undecided or
	// denied and then drops it, so check first: only a notification that
	// can show marks its issue Notified.
	if ok, err := n.ns.CheckNotificationAuthorization(); err != nil {
		return err
	} else if !ok {
		return notify.ErrNotAllowed
	}
	data := map[string]any{}
	if m.IssueID != "" {
		data["issueId"] = m.IssueID
	}
	return n.ns.SendNotification(notifications.NotificationOptions{
		ID:       m.ID,
		Title:    m.Title,
		Body:     m.Body,
		Data:     data,
		ThreadID: "visor-issues",
	})
}

func (n *macNotifier) status() string {
	if !n.ready.Load() {
		return NotifyUnavailable
	}
	ok, err := n.ns.CheckNotificationAuthorization()
	switch {
	case err != nil:
		log.Printf("visor: notification permission: %v", err)
		return NotifyUnavailable
	case ok:
		return NotifyAllowed
	}
	return NotifyDenied
}
