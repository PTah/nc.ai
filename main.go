package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

const (
	defaultWidth  = 1440
	defaultHeight = 920
	minWidth      = 1100
	minHeight     = 700
)

func main() {
	app := NewApp()
	_ = app.cfg.Load()
	s := app.cfg.Get()

	width := defaultWidth
	height := defaultHeight
	if s.WindowWidth >= minWidth {
		width = s.WindowWidth
	}
	if s.WindowHeight >= minHeight {
		height = s.WindowHeight
	}

	err := wails.Run(&options.App{
		Title:            "NotCursor.ai",
		Width:            width,
		Height:           height,
		MinWidth:         minWidth,
		MinHeight:        minHeight,
		WindowStartState: startState(s.WindowMaximised),
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 18, G: 18, B: 18, A: 1},
		OnStartup:        app.startup,
		OnDomReady:       app.domReady,
		OnShutdown:       app.shutdown,
		OnBeforeClose:    app.beforeClose,
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}

func startState(maximised bool) options.WindowStartState {
	if maximised {
		return options.Maximised
	}
	return options.Normal
}
