package main

import (
	"embed"
	"log"
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/touhidurabir/visor/internal/app"
)

// The built frontend (frontend/dist) is embedded into the binary.
//
//go:embed all:frontend/dist
var assets embed.FS

func main() {
	stateDir, err := app.StateDir()
	if err != nil {
		log.Fatal(err)
	}
	rt := app.NewRuntime(app.RuntimeOptions{
		// VISOR_CONFIG and VISOR_STATE_DIR are set by `task dev` to point
		// at the sandbox.
		ConfigPath: os.Getenv("VISOR_CONFIG"),
		StateDir:   stateDir,
		Emit:       app.WailsEmit,
	})
	wails := application.New(application.Options{
		Name:        "Visor",
		Description: "A macOS menu-bar app for managing supervisord",
		Services: []application.Service{
			// ServerService starts and stops rt with the app.
			application.NewService(app.NewServerService(rt)),
			application.NewService(app.NewIssueService(rt)),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	wails.Window.NewWithOptions(application.WebviewWindowOptions{
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

	if err := wails.Run(); err != nil {
		log.Fatal(err)
	}
}
