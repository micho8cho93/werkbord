package main

import (
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"

	"devboard/desktop/internal/shell"
)

const projectURL = "https://github.com/micho8cho93/werkbord"

// buildMenu is the menu bar: the standard application, Edit and Window menus (which is what makes
// copy and paste, hiding, minimising and Cmd-Q work as on any Mac), and what is particular to Werkbord.
// Menu actions that wait for anything run off the menu's own thread.
func buildMenu(sh *shell.Shell, ui *wailsUI, withShell bool) *menu.Menu {
	m := menu.NewMenu()
	m.Append(menu.AppMenu())
	m.Append(menu.EditMenu())

	// Workspaces: where the person is going, which the shell page decides what to do about. The menu only says where.
	if withShell {
		ws := m.AddSubmenu("Workspace")
		go2 := func(where string) func(*menu.CallbackData) {
			return func(*menu.CallbackData) { ui.Emit("shell:go", where) }
		}
		ws.AddText("Individual", keys.CmdOrCtrl("1"), go2("personal"))
		ws.AddText("My Work", keys.CmdOrCtrl("2"), go2("mywork"))
		ws.AddText("Calendar", keys.CmdOrCtrl("3"), go2("calendar"))
		ws.AddText("Needs You", keys.CmdOrCtrl("4"), go2("inbox"))
		ws.AddSeparator()
		ws.AddText("Switch Workspace…", keys.Combo("k", keys.CmdOrCtrlKey, keys.ShiftKey), go2("switch"))
		ws.AddText("Workspaces and Devices", keys.CmdOrCtrl("0"), go2("workspaces"))
		ws.AddText("Add a Team…", nil, go2("add-team"))
	}

	view := m.AddSubmenu("View")
	view.AddText("Reload", keys.CmdOrCtrl("r"), func(*menu.CallbackData) { sh.Reload() })
	view.AddText("Open in Browser", keys.Combo("o", keys.CmdOrCtrlKey, keys.ShiftKey), func(*menu.CallbackData) { go sh.OpenInBrowser() })

	m.Append(menu.WindowMenu())

	// "Check for Updates…" is not here: it is in the application menu, where a Mac has it, added by updater_darwin.m
	// (Wails's application menu is native and cannot be added to from Go). There is one, and it asks the shell, which
	// knows whether the app, the program, or neither can be updated now.
	help := m.AddSubmenu("Help")
	help.AddText("Show Diagnostics…", nil, func(*menu.CallbackData) { go sh.ShowDiagnostics() })
	help.AddText("Show Controller Log", nil, func(*menu.CallbackData) { go sh.OpenLogs() })
	help.AddSeparator()
	help.AddText("Werkbord on GitHub", nil, func(*menu.CallbackData) { ui.OpenURL(projectURL) })
	return m
}
