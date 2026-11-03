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
// app again, or macOS starting the login item when it's switched on in
// System Settings) exits and hands over to the running one. Only the
// packaged app (bundle != ""): dev and server builds may run beside it.
func SingleInstance(bundle string, shell *Shell) *application.SingleInstanceOptions {
	if bundle == "" {
		return nil
	}
	return &application.SingleInstanceOptions{
		UniqueID: "dev.getvisor.visor",
		OnSecondInstanceLaunch: func(d application.SecondInstanceData) {
			if secondLaunchShowsWindow(d.Args) {
				application.InvokeAsync(func() { shell.ShowMain("") })
			}
		},
	}
}

func secondLaunchShowsWindow(args []string) bool { return !slices.Contains(args, LoginArg) }
