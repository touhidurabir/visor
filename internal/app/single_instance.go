package app

import (
	"slices"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/touhidurabir/visor/internal/platform"
)

// LoginArg marks a launch by the login item: when Visor already runs, it
// stays quiet instead of bringing the window forward.
const LoginArg = platform.LoginArg

// SingleInstance keeps one Visor per user: a second launch (opening the
// app again, or the login item starting it) exits and hands over to the
// running one. Only an installed copy (platform.Installed: the .app, the
// AppImage or a packaged binary): dev and server builds may run beside it.
// On Linux Wails claims the D-Bus name dev.getvisor.visor.SingleInstance.
func SingleInstance(installed bool, shell *Shell) *application.SingleInstanceOptions {
	if !installed {
		return nil
	}
	return &application.SingleInstanceOptions{
		UniqueID: "dev.getvisor.visor",
		OnSecondInstanceLaunch: func(d application.SecondInstanceData) {
			// Wails calls this on a goroutine. Not InvokeAsync: ShowMain's
			// Dock call is a dispatch_sync to the main queue, which traps
			// (SIGILL) when run on the main thread.
			if secondLaunchShowsWindow(d.Args) {
				shell.ShowMain("")
			}
		},
	}
}

func secondLaunchShowsWindow(args []string) bool { return !StartedAtLogin(args) }

// StartedAtLogin reports whether the login item launched Visor (args are
// the command-line arguments): it then starts in the menu bar or tray
// only, with no window and (on macOS) no Dock icon.
func StartedAtLogin(args []string) bool { return slices.Contains(args, LoginArg) }
