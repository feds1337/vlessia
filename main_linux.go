package main

import (
	_ "embed"

	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
)

//go:embed build/appicon.png
var appIcon []byte

func platformOptions(o *options.App, _ *App) {
	o.Linux = &linux.Options{
		Icon:        appIcon,
		ProgramName: "vlessia",
	}
}
