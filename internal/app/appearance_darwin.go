//go:build darwin && !server

package app

/*
#cgo darwin CFLAGS: -x objective-c
#cgo darwin LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>

// visorSetAppearance sets the app-wide appearance: 1 light (Aqua), 2 dark
// (DarkAqua), anything else follows the system. Call it on the main thread.
static void visorSetAppearance(int mode) {
	NSAppearance *a = nil;
	if (mode == 1) {
		a = [NSAppearance appearanceNamed:NSAppearanceNameAqua];
	} else if (mode == 2) {
		a = [NSAppearance appearanceNamed:NSAppearanceNameDarkAqua];
	}
	[NSApp setAppearance:a];
}
*/
import "C"

import (
	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/touhidurabir/visor/internal/settings"
)

// setNativeAppearance makes the window chrome (title bar, traffic lights,
// the tray popover frame, scroll bars) follow the chosen theme; "system"
// goes back to following macOS. It queues the change on the main thread, so
// it is safe from any goroutine, and does nothing before the app exists.
func setNativeAppearance(appearance string) {
	if application.Get() == nil {
		return
	}
	mode := C.int(0)
	switch appearance {
	case settings.AppearanceLight:
		mode = 1
	case settings.AppearanceDark:
		mode = 2
	}
	application.InvokeAsync(func() { C.visorSetAppearance(mode) })
}
