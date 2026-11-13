package main

import (
	"embed"
	"log"
	"os"
	"runtime"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/services/dock"

	"github.com/touhidurabir/visor/internal/app"
	"github.com/touhidurabir/visor/internal/platform"
	"github.com/touhidurabir/visor/internal/remote"
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
	guardWebKitSandbox(stateDir)
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
	// The hub owns the tabs: this computer's Runtime and one per open server.
	hub := app.NewHub(rt, app.HubOptions{
		StateDir: stateDir,
		Keychain: keychain,
		Emit:     shell.Emit(app.WailsEmit),
		LogEmit:  app.WailsEmit,
		Notifier: notifier,
		HostKeys: remote.DefaultHostKeys(stateDir),
		Closed:   shell.Forget,
	})
	remoteService := app.NewRemoteService(hub, stateDir, keychain, remote.DefaultHostKeys(stateDir), rt.Settings)
	dockService := dock.New()
	settingsService := app.NewSettingsService(rt, stateDir, envConfig, keychain, loadErr)
	settingsService.ApplyToServers(hub.ApplySettings)
	loginService := app.NewLoginItemService(loginItem, shell.ShowLoginStatus)
	// LogLevel stays at Wails' default (Info). Do not raise it to Debug:
	// Wails then logs every binding call with its arguments, which would
	// put SaveConnection's supervisord password in the log.
	wails := application.New(application.Options{
		Name:        "Visor",
		Description: "A menu-bar and tray app for managing supervisord",
		Services: []application.Service{
			// Before ServerService, so notifications are ready by the first poll.
			application.NewService(notifier),
			// The hub starts rt with the app and stops every tab at quit.
			application.NewService(app.NewHubService(hub)),
			// Per-tab services: each method takes the tab's id first.
			application.NewService(app.NewServerAPI(hub)),
			application.NewService(app.NewIssueAPI(hub)),
			application.NewService(app.NewInboxAPI(hub)),
			application.NewService(app.NewConfigAPI(hub)),
			application.NewService(app.NewLogAPI(hub)),
			// Saved servers: the form, Settings → Servers and the tab bar.
			application.NewService(remoteService),
			application.NewService(settingsService),
			application.NewService(app.NewDoctorService(rt, settingsService, loginService)),
			application.NewService(app.NewShellService(shell)),
			application.NewService(loginService),
			application.NewService(dockService),
			application.NewService(app.NewSystemService()),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		// One Visor per user: a second launch hands over to this one.
		SingleInstance: app.SingleInstance(platform.Installed(), shell),
		Mac: application.MacOptions{
			// Closing the main window hides it; Visor lives on in the menu
			// bar until Quit.
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
		Linux: application.LinuxOptions{
			// The same on Linux: Visor lives on in the tray.
			DisableQuitOnLastWindowClosed: true,
			// The GTK application id, which is also the Wayland app_id and
			// the window class, so the desktop matches windows to
			// dev.getvisor.visor.desktop and its icon.
			ApplicationID: "dev.getvisor.visor",
		},
	})

	// The app starts with the main window shown, so the Dock icon shows;
	// started by the login item, in the menu bar or tray only.
	atLogin := app.StartedAtLogin(os.Args[1:])
	if atLogin {
		shell.StartInTray()
	}
	mainWindow := wails.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:   "main",
		Title:  "Visor",
		Width:  app.MainWidth,
		Height: app.MainHeight,
		Hidden: atLogin,
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 50,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		URL: "/",
		// Files dropped on the window go to Go, which reads them for Add new
		// → Import files (app.FilesDropped). Left to the web view, WebKitGTK
		// would load a dropped file in place of Visor's page.
		EnableFileDrop: true,
	})
	mainWindow.OnWindowEvent(events.Common.WindowFilesDropped, func(e *application.WindowEvent) {
		app.FilesDropped(e.Context().DroppedFiles())
	})
	shell.Setup(wails, mainWindow, dockService)
	// Visor's own menu bar, in place of Wails' default (whose Reload loses
	// unsaved edits and whose Help loads wails.io into the window). macOS
	// only: on Linux Wails would draw it inside every window, the popover
	// too, so there the page carries out the shortcuts (lib/shortcuts.ts).
	if runtime.GOOS == "darwin" {
		wails.Menu.SetApplicationMenu(app.BuildAppMenu(wails, shell))
	}
	// The window chrome follows the saved appearance from launch on.
	wails.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		app.ApplyStartupAppearance(prefs.Appearance)
	})

	if err := wails.Run(); err != nil {
		log.Fatal(err)
	}
}
