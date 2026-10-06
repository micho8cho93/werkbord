// Command werkbord-desktop is the Werkbord desktop app: a native window around the web app that
// the controller already serves. See docs/DESKTOP.md.
//
// It is not a second program that does Werkbord's work. The controller (Go, SQLite, the agents) is a
// background service exactly as `werkbord setup` makes it, and keeps running when this window is
// closed or this app quits; the browser and the phone reach the same one. What this app adds is a
// way to open it without a terminal: it finds the controller or starts it (internal/launcher),
// then loads the controller's own web app into a window.
package main

import (
	"context"
	"embed"
	"fmt"
	"os"
	goruntime "runtime"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"

	"devboard/desktop/internal/shell"
	"devboard/internal/launcher"
)

// The loading and error screen: the only page of the app that is not the controller's web app.
//
//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var icon []byte

// version is the version of Werkbord this app was built with (-ldflags "-X main.version=…"), which
// is also the version of the controller program inside it.
var version = "dev"

// App is what the windows's pages can call: exactly the exported methods of shell.Shell, which a test
// pins. It has no methods of its own, so nothing is callable from a page that the shell did not decide on.
type App struct{ *shell.Shell }

func main() {
	log, appLog, closeLog := openLog()
	defer closeLog()
	log.Info("starting", "version", version, "os", goruntime.GOOS, "arch", goruntime.GOARCH)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	l := launcher.New(launcherOptions())
	ui := &wailsUI{}
	sh := shell.New(shell.Options{Launcher: l, UI: ui, Version: version, Platform: goruntime.GOOS, AppLog: appLog, Log: log, Ctx: ctx})

	err := wails.Run(&options.App{
		Title:            "Werkbord",
		Width:            1280,
		Height:           840,
		MinWidth:         480, // the web app has a phone layout below 900px wide, and works down to this
		MinHeight:        560,
		BackgroundColour: &options.RGBA{R: 245, G: 245, B: 242, A: 255}, // the web app's paper
		AssetServer:      &assetserver.Options{Assets: assets},
		Bind:             []any{&App{sh}},
		// Pages the controller serves may call what App offers, whichever port the controller ended up on.
		// Wails refuses calls from any other page, and from a page the window was sent to by a link.
		BindingsAllowedOrigins: "http://127.0.0.1:*,http://localhost:*",
		Menu:                   buildMenu(sh, ui),
		// Closing the window hides the app, as on any Mac: it is still in the Dock, and clicking it brings the window
		// back. Cmd-Q quits the app. Neither touches the controller, which is not this app's child.
		HideWindowOnClose:        true,
		EnableDefaultContextMenu: true,
		OnStartup:                ui.startup,
		OnShutdown:               func(context.Context) { cancel(); log.Info("quit") },
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId:               "dev.werkbord.desktop",
			OnSecondInstanceLaunch: func(options.SecondInstanceData) { ui.show() },
		},
		Mac: &mac.Options{
			TitleBar:   mac.TitleBarDefault(),
			Appearance: mac.DefaultAppearance,
			About: &mac.AboutInfo{
				Title:   "Werkbord " + version,
				Message: "Werkbord runs your coding agents on your own computer.\n\nThe controller keeps working when this window is closed.",
				Icon:    icon,
			},
		},
	})
	if err != nil {
		log.Error("the window could not be opened", "err", err)
		fmt.Fprintln(os.Stderr, "werkbord-desktop:", err)
		os.Exit(1)
	}
}
