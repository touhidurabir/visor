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
	wails := application.New(application.Options{
		Name:        "Visor",
		Description: "A macOS menu-bar app for managing supervisord",
		Services: []application.Service{
			// VISOR_CONFIG is set by `task dev` to point at the sandbox.
			application.NewService(app.NewServerService(os.Getenv("VISOR_CONFIG"))),
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
