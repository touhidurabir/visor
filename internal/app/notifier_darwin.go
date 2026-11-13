//go:build darwin && !server

package app

import (
	"context"
	"log"
	"sync/atomic"
	"time"

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
	// asked is closed once the launch permission request has its answer.
	// Issues often open in the first second after launch, while the
	// prompt is still on screen: their sends wait for it instead of being
	// dropped as not allowed.
	asked chan struct{}
}

// permissionWait bounds how long a send waits for the permission prompt.
const permissionWait = 2 * time.Minute

func newNativeNotifier() nativeNotifier { return &macNotifier{asked: make(chan struct{})} }

func (n *macNotifier) start(ctx context.Context, onClick func(server string)) {
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
			go onClick(serverOf(r.Response.UserInfo)) // off the notification centre's callback
		}
	})
	n.ns = ns
	n.ready.Store(true)
	// Ask once at launch. macOS prompts only the first time; afterwards
	// this returns the person's choice. It may wait minutes for an answer.
	go func() {
		defer close(n.asked)
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
	ok, err := n.ns.CheckNotificationAuthorization()
	if err == nil && !ok {
		select { // the person may still be answering the launch prompt
		case <-n.asked:
		case <-time.After(permissionWait):
		}
		ok, err = n.ns.CheckNotificationAuthorization()
	}
	if err != nil {
		return err
	} else if !ok {
		return notify.ErrNotAllowed
	}
	data := map[string]any{}
	if m.IssueID != "" {
		data["issueId"] = m.IssueID
	}
	if m.Server != "" {
		data["server"] = m.Server
	}
	return n.ns.SendNotification(notifications.NotificationOptions{
		ID:       m.ID,
		Title:    m.Title,
		Subtitle: m.Subtitle,
		Body:     m.Body,
		Data:     data,
		// macOS stacks a thread's banners: a burst from one config is one
		// stack. It sums them up as "N more notifications"; Wails has no
		// summaryArgument to say "N more from flaky.ini".
		ThreadID: m.Thread,
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
