// Command werkbord-desktop is the unified Personal and Team desktop shell. See docs/DESKTOP.md.
//
// It is not a second program that does Werkbord's work. The controller (Go, SQLite, the agents) is a
// background service exactly as `werkbord setup` makes it, and keeps running when this window is
// closed or this app quits; the browser and the phone reach the same one. What this app adds is a
// way to open it without a terminal: it finds the controller or starts it (internal/launcher),
// then embeds each isolated service's own interface in one workspace window.
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

// The loading/error page and bundled neutral workspace shell.
//
//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var icon []byte

// instanceID names the single-instance lock: a second launch brings the first to the front instead of making another window.
// (The test build, which must never meet a real app, has its own: updater_testenv_darwin.go.)
var instanceID = "dev.werkbord.desktop"

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
	parts, err := newWorkspaceParts(l, log)
	if err != nil {
		log.Error("the workspaces could not be set up", "err", err)
		fmt.Fprintln(os.Stderr, "werkbord-desktop:", err)
		os.Exit(1)
	}
	// An invitation the app was opened with (a werkbord://join/ link) waits for the shell to use it.
	for _, arg := range os.Args[1:] {
		parts.invites.Receive(arg)
	}
	if !hasShell(assets) {
		log.Error("workspace shell assets missing; run make web-shell before building")
		os.Exit(1)
	}
	page := shellPage
	// Sparkle, if this build has it: it may look at its feed only when the person has not turned looking for updates off.
	nu := newNativeUpdater(log, func() bool { return l.UpdatesAllowed(ctx) })
	var up shell.Updater
	if nu != nil {
		up = nu
	}
	sh := shell.New(shell.Options{Launcher: l, UI: ui, Version: version, Platform: goruntime.GOOS, AppLog: appLog, Log: log, Ctx: ctx, Updater: up,
		Workspaces: parts.registry, Team: parts.team, TeamInstaller: parts.installer, Grants: parts.personal, Invites: parts.invites, ShellPage: page})

	// The window's own page is the only page that may call the app: every workspace's page is shown in a
	// frame of it, WebKit gives a frame no way to call the app, and a workspace's page asks the shell page, which asks
	// Relay. Workspace pages never receive the full shell binding.
	bindingsFrom := ""

	err = wails.Run(&options.App{
		Title:            "Werkbord",
		Width:            1280,
		Height:           840,
		MinWidth:         480, // the web app has a phone layout below 900px wide, and works down to this
		MinHeight:        560,
		BackgroundColour: &options.RGBA{R: 245, G: 245, B: 242, A: 255}, // the web app's paper
		AssetServer:      &assetserver.Options{Assets: assets},
		Bind:             []any{&App{sh}},
		// Only the bundled shell origin may call App. Wails also rejects subframe calls.
		BindingsAllowedOrigins: bindingsFrom,
		Menu:                   buildMenu(sh, ui, page != ""),
		// Closing the window hides the app, as on any Mac: it is still in the Dock, and clicking it brings the window
		// back. Cmd-Q quits the app. Neither touches the controller, which is not this app's child.
		HideWindowOnClose:        true,
		EnableDefaultContextMenu: true,
		OnStartup:                func(c context.Context) { ui.startup(c); startNative(nu, sh) },
		OnShutdown:               func(context.Context) { cancel(); log.Info("quit") },
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: instanceID,
			OnSecondInstanceLaunch: func(d options.SecondInstanceData) {
				for _, arg := range d.Args {
					if parts.invites.Receive(arg) {
						ui.Emit("shell:go", "invitation")
					}
				}
				ui.show()
			},
		},
		Mac: &mac.Options{
			TitleBar:   mac.TitleBarDefault(),
			Appearance: mac.DefaultAppearance,
			// A Team invitation (werkbord://join/…) opens in this app, which holds it for the shell.
			OnUrlOpen: func(u string) {
				if parts.invites.Receive(u) {
					ui.Emit("shell:go", "invitation")
					ui.show()
				}
			},
			About: &mac.AboutInfo{
				Title:   "Werkbord " + version,
				Message: "Werkbord runs your coding agents on your own computer, and your teams' work beside it.\n\nYour controller, your agents, your schedules and your Team services keep working when this window is closed.",
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
