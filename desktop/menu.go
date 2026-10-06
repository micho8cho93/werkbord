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
func buildMenu(sh *shell.Shell, ui *wailsUI) *menu.Menu {
	m := menu.NewMenu()
	m.Append(menu.AppMenu())
	m.Append(menu.EditMenu())

	view := m.AddSubmenu("View")
	view.AddText("Reload", keys.CmdOrCtrl("r"), func(*menu.CallbackData) { sh.Reload() })
	view.AddText("Open in Browser", keys.Combo("o", keys.CmdOrCtrlKey, keys.ShiftKey), func(*menu.CallbackData) { go sh.OpenInBrowser() })

	m.Append(menu.WindowMenu())

	help := m.AddSubmenu("Help")
	help.AddText("Check for Updates…", nil, func(*menu.CallbackData) { go sh.CheckForUpdates() })
	help.AddSeparator()
	help.AddText("Show Diagnostics…", nil, func(*menu.CallbackData) { go sh.ShowDiagnostics() })
	help.AddText("Show Controller Log", nil, func(*menu.CallbackData) { go sh.OpenLogs() })
	help.AddSeparator()
	help.AddText("Werkbord on GitHub", nil, func(*menu.CallbackData) { ui.OpenURL(projectURL) })
	return m
}
