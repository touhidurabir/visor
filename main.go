package main

import (
	"embed"
	"log"
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/services/dock"

	"github.com/touhidurabir/visor/internal/app"
	"github.com/touhidurabir/visor/internal/platform"
	"github.com/touhidurabir/visor/internal/settings"
)

// The built frontend (frontend/dist) is embedded into the binary.
//
//go:embed all:frontend/dist
var assets embed.FS

func main() {
	guardEnvironment()
	stateDir, err := app.StateDir()
	if err != nil {
		log.Fatal(err)
	}
	// A settings file that cannot be read leaves the defaults in use; the
	// Settings page says so.
	prefs, loadErr := settings.Load(stateDir)
	if loadErr != nil {
		log.Print(loadErr)
	}
	// VISOR_CONFIG and VISOR_STATE_DIR are set by `task dev` to point at
	// the sandbox. VISOR_CONFIG overrides the config path in Settings.
	envConfig := os.Getenv("VISOR_CONFIG")
	keychain := newKeychain()
	// The shell (menu-bar icon, popover, Dock) watches every overview to
	// keep the menu-bar icon's health current.
	shell := app.NewShell()
	loginItem := platform.DefaultLoginItem()
	shell.UseLoginItem(loginItem)
	// A login item written by an older Visor gets today's arguments.
	if r, ok := loginItem.(interface{ Refresh() error }); ok {
		if err := r.Refresh(); err != nil {
			log.Printf("visor: updating the login item: %v", err)
		}
	}
	shell.ShowMenuBarCount(prefs.MenuBarCount)
	// Native notifications for new issues; clicking one opens Issues. They
	// work only in the packaged Visor.app (and never in the server build).
	notifier := app.NewNotifyService(func() { shell.ShowMain(app.RouteIssues) })
	rt := app.NewRuntime(app.RuntimeOptions{
		ConfigPath: envConfig,
		StateDir:   stateDir,
		Settings:   prefs,
		Keychain:   keychain,
		Emit:       shell.Emit(app.WailsEmit),
		Notifier:   notifier,
	})
	dockService := dock.New()
	settingsService := app.NewSettingsService(rt, stateDir, envConfig, keychain, loadErr)
	loginService := app.NewLoginItemService(loginItem, shell.ShowLoginStatus)
	// LogLevel stays at Wails' default (Info). Do not raise it to Debug:
	// Wails then logs every binding call with its arguments, which would
	// put SaveConnection's supervisord password in the log.
	wails := application.New(application.Options{
		Name:        "Visor",
		Description: "A macOS menu-bar app for managing supervisord",
		Services: []application.Service{
			// Before ServerService, so notifications are ready by the first poll.
			application.NewService(notifier),
			// ServerService starts and stops rt with the app.
			application.NewService(app.NewServerService(rt)),
			application.NewService(app.NewIssueService(rt)),
			application.NewService(app.NewInboxService(rt)),
			application.NewService(app.NewConfigService(rt)),
			application.NewService(settingsService),
			application.NewService(app.NewDoctorService(rt, settingsService, loginService)),
			// Log events go to the frontend only; the shell has no use for them.
			application.NewService(app.NewLogService(rt, app.WailsEmit)),
			application.NewService(app.NewShellService(shell)),
			application.NewService(loginService),
			application.NewService(dockService),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		// One Visor per user: a second launch hands over to this one.
		SingleInstance: app.SingleInstance(platform.BundlePath(), shell),
		Mac: application.MacOptions{
			// Closing the main window hides it; Visor lives on in the menu
			// bar until Quit.
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
	})

	// The app starts with the main window shown, so the Dock icon shows.
	mainWindow := wails.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:   "main",
		Title:  "Visor",
		Width:  1000,
		Height: 640,
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 50,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		URL: "/",
	})
	shell.Setup(wails, mainWindow, dockService)
	// Visor's own menu bar, in place of Wails' default (whose Reload loses
	// unsaved edits and whose Help loads wails.io into the window).
	wails.Menu.SetApplicationMenu(app.BuildAppMenu(wails, shell))
	// The window chrome follows the saved appearance from launch on.
	wails.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		app.ApplyStartupAppearance(prefs.Appearance)
	})

	if err := wails.Run(); err != nil {
		log.Fatal(err)
	}
}
