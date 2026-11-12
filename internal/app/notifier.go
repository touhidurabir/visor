package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/touhidurabir/visor/internal/notify"
	"github.com/touhidurabir/visor/internal/platform"
)

// Notification permission, as Settings shows it.
const (
	// NotifyAllowed: macOS shows Visor's notifications, or on Linux a
	// notification server answers.
	NotifyAllowed = "allowed"
	// NotifyDenied: notifications are off for Visor in System Settings
	// (or the person has not answered the permission prompt yet).
	NotifyDenied = "denied"
	// NotifyUnavailable: this build cannot show notifications: the server
	// build, a binary outside Visor.app (`task dev`), which macOS gives no
	// bundle identifier, or on Linux a session with no notification server.
	NotifyUnavailable = "unavailable"
)

// testThread is the banner thread of Settings' sample notification, apart
// from every config's.
const testThread = "visor-test"

// nativeNotifier is the platform's notification centre.
type nativeNotifier interface {
	// start prepares notifications and asks for permission once. When it
	// cannot, it logs why and leaves notifications off; it never fails.
	start(ctx context.Context, onClick func())
	send(m notify.Message) error
	status() string
}

// NotifyService shows Visor's native notifications: it is the Runtime's
// notify.Sender, and tells Settings whether macOS allows them. Clicking a
// notification calls onClick.
//
// Visor registers this service instead of Wails' notifications service,
// because Wails stops the whole app when that service fails to start, and
// it does in any binary outside Visor.app.
type NotifyService struct {
	native  nativeNotifier
	onClick func()
}

// NewNotifyService returns a NotifyService that calls onClick (may be nil)
// when a notification is clicked.
func NewNotifyService(onClick func()) *NotifyService {
	return &NotifyService{native: newNativeNotifier(), onClick: onClick}
}

// ServiceStartup prepares notifications when the app starts. It never
// fails: without notifications Visor runs on, and Settings says why.
func (s *NotifyService) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	s.native.start(ctx, s.onClick)
	return nil
}

// Send shows m. It returns notify.ErrUnavailable when this build cannot
// show notifications.
//
//wails:ignore
func (s *NotifyService) Send(m notify.Message) error { return s.native.send(m) }

// NotificationStatus says whether macOS shows Visor's notifications:
// NotifyAllowed, NotifyDenied or NotifyUnavailable.
func (s *NotifyService) NotificationStatus() string { return s.native.status() }

// OpenNotificationSettings opens System Settings at Notifications, where
// Visor's banners are turned on.
func (s *NotifyService) OpenNotificationSettings() error { return platform.OpenNotificationSettings() }

// SendTestNotification shows a sample notification, or says why it cannot.
func (s *NotifyService) SendTestNotification() error {
	switch s.native.status() {
	case NotifyUnavailable:
		return unavailableReason(application.System.IsServer(), platform.Current())
	case NotifyDenied:
		return errors.New("notifications are off for Visor. Turn them on in System Settings → Notifications → Visor")
	}
	return s.native.send(notify.Message{
		ID:     fmt.Sprintf("visor-test-%d", time.Now().UnixNano()),
		Title:  notify.Info.Icon() + " Visor notifications are on",
		Body:   "You'll get one like this when a process fails, marked " + notify.Error.Icon() + " for an error or " + notify.Warning.Icon() + " for a warning.",
		Thread: testThread,
	})
}

// unavailableReason says why Settings' test notification can't show.
func unavailableReason(server bool, os platform.OS) error {
	switch {
	case server:
		return errors.New("notifications aren't available in this build")
	case os == platform.Darwin:
		return errors.New("notifications aren't available in this build. Run the packaged Visor.app to get them")
	}
	return errors.New("notifications aren't available: no desktop notification service is running in this session")
}
