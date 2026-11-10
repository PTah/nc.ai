// Edited by nc.ai 0.8.8.5 on new-admin-pc 2026-10-19 11:40:05
// Edited by nc.ai 1.1.11 on macbook 2026-11-08 21:41:13
package main

import (
	"embed"
	"os"
	stdruntime "runtime"
	"strings"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"notcursor.ai/app/internal/update"
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
	// Режим самоприменения обновления: тот же бинарь вызван с --apply-update,
	// подменяет exe и перезапускает приложение — без PowerShell/Bypass.
	if update.ApplyUpdate(os.Args[1:]) {
		return
	}
	hardenWebView2Occlusion()
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
		// Один экземпляр приложения: второй запуск (например, превью-сборка или
		// двойной клик по exe) не должен открывать второе окно и делить каталог
		// данных %APPDATA%\NotCursor — иначе параллельные записи в settings.json,
		// drafts и running.json затирают друг друга и приводят к «вылетам».
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "notcursor.ai-app-single-instance",
		},
		// Production builds hide the browser context menu by default; enable it so
		// selected chat text can be copied via right-click → Copy (Ctrl+C still works).
		EnableDefaultContextMenu: true,
		// Папку можно перетащить в окно — она станет корнем рабочей области
		// (как Add folder to workspace, но без диалога и сразу несколько за раз).
		DragAndDrop:   &options.DragAndDrop{EnableFileDrop: true},
		OnStartup:     app.startup,
		OnDomReady:    app.domReady,
		OnShutdown:    app.shutdown,
		OnBeforeClose: app.beforeClose,
		Windows: &windows.Options{
			// После сна Windows WebView2 часто поднимает пустую поверхность:
			// мягко перезагружаем только фронт, Go-процесс и черновики остаются.
			OnResume: func() {
				if app.ctx == nil {
					return
				}
				runtime.WindowReloadApp(app.ctx)
			},
		},
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}

// hardenWebView2Occlusion снижает шанс «белого окна» после долгого minimize:
// WebView2 иначе считает окно occluded и подвешивает GPU/render.
func hardenWebView2Occlusion() {
	if stdruntime.GOOS != "windows" {
		return
	}
	const key = "WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS"
	const flag = "--disable-features=CalculateNativeWinOcclusion"
	cur := strings.TrimSpace(os.Getenv(key))
	if cur == "" {
		_ = os.Setenv(key, flag)
		return
	}
	if strings.Contains(cur, "CalculateNativeWinOcclusion") {
		return
	}
	_ = os.Setenv(key, cur+" "+flag)
}

func startState(maximised bool) options.WindowStartState {
	if maximised {
		return options.Maximised
	}
	return options.Normal
}
