package main

import (
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

// platformOptions: macOS has no tray here, so closing the window minimises
// it to the Dock (see minimiseOnClose) and the app menu provides Quit. The
// Edit menu is required for copy and paste in text fields.
func platformOptions(o *options.App, app *App) {
	o.Mac = &mac.Options{
		Appearance: mac.DefaultAppearance,
		About: &mac.AboutInfo{
			Title:   "Vlessia",
			Message: "VPN-клиент на базе Xray-core.\nhttps://github.com/feds1337/vlessia",
		},
	}
	m := menu.NewMenu()
	appMenu := m.AddSubmenu("Vlessia")
	appMenu.AddText("Показать окно", keys.CmdOrCtrl("0"), func(*menu.CallbackData) { app.ShowWindow() })
	appMenu.AddSeparator()
	appMenu.AddText("Выйти из Vlessia", keys.CmdOrCtrl("q"), func(*menu.CallbackData) { app.Quit() })
	m.Append(menu.EditMenu())
	o.Menu = m
}
