package app

import (
	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/touhidurabir/visor/internal/platform"
)

// EventLoginItem carries a LoginItemView when launch at login changes from
// the tray menu, so an open Settings page shows it.
const EventLoginItem = "visor:login-item"

func init() { application.RegisterEvent[LoginItemView](EventLoginItem) }

// LoginItemView is whether Visor opens at login. Status is "enabled",
// "disabled", "requires_approval" (macOS wants the user's OK in Login
// Items), "not_moved" (running from the disk image or Downloads) or
// "unsupported" (not the packaged app).
type LoginItemView struct {
	Status string
	On     bool
}

// LoginItemService exposes launch at login to Settings, Doctor and the
// tray menu.
type LoginItemService struct {
	li      platform.LoginItem
	changed func(platform.LoginItemStatus)
}

// NewLoginItemService wraps li; changed (may be nil) hears every new status,
// so the tray menu's check mark follows.
func NewLoginItemService(li platform.LoginItem, changed func(platform.LoginItemStatus)) *LoginItemService {
	return &LoginItemService{li: li, changed: changed}
}

func loginView(st platform.LoginItemStatus) LoginItemView {
	return LoginItemView{Status: string(st), On: st == platform.LoginEnabled || st == platform.LoginRequiresApproval}
}

// LaunchAtLogin is the current status.
func (s *LoginItemService) LaunchAtLogin() LoginItemView { return loginView(s.li.Status()) }

// SetLaunchAtLogin turns it on or off and returns the status that results,
// with the error when macOS or the app's location refused.
func (s *LoginItemService) SetLaunchAtLogin(on bool) (LoginItemView, error) {
	err := s.li.Set(on)
	st := s.li.Status()
	if err == nil && s.changed != nil {
		s.changed(st)
	}
	return loginView(st), err
}

// OpenLoginItemsSettings opens System Settings → General → Login Items.
func (s *LoginItemService) OpenLoginItemsSettings() error { return platform.OpenLoginItemsSettings() }
