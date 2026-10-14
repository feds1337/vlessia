package main

import (
	"os"
	"time"

	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	xwin "golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// helperArg is only used on macOS and Linux.
const helperArg = "--tun-helper"

func runHelper([]string) int { return 2 }

func platformOptions(o *options.App, _ *App) {
	o.BackgroundColour = windowBackground()
	o.Windows = &windows.Options{Theme: windows.SystemDefault}
}

// windowBackground matches the UI's paper colour for the Windows app theme,
// so the window does not flash white before the page loads in dark mode.
func windowBackground() *options.RGBA {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err == nil {
		defer k.Close()
		if v, _, err := k.GetIntegerValue("AppsUseLightTheme"); err == nil && v == 0 {
			return &options.RGBA{R: 0x15, G: 0x18, B: 0x1e, A: 255}
		}
	}
	return &options.RGBA{R: 0xf5, G: 0xf6, B: 0xf8, A: 255}
}

func waitForExit(pid uint32, timeout time.Duration) {
	h, err := xwin.OpenProcess(xwin.SYNCHRONIZE, false, pid)
	if err != nil {
		return
	}
	defer xwin.CloseHandle(h)
	_, _ = xwin.WaitForSingleObject(h, uint32(timeout.Milliseconds()))
}

func fatal(msg string) {
	title, _ := xwin.UTF16PtrFromString("Vlessia")
	text, _ := xwin.UTF16PtrFromString(msg)
	_, _ = xwin.MessageBox(0, text, title, xwin.MB_ICONERROR)
	os.Exit(1)
}
