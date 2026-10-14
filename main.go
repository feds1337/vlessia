package main

import (
	"embed"
	"flag"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	xwin "golang.org/x/sys/windows"

	"github.com/feds1337/vlessia/internal/autostart"
	"github.com/feds1337/vlessia/internal/store"
	"github.com/feds1337/vlessia/internal/sysproxy"
)

//go:embed all:frontend/dist
var assets embed.FS

// version is set at build time (-ldflags "-X main.version=v1.2.3").
var version = "dev"

func main() {
	waitPID := flag.Int("wait-pid", 0, "wait for this process to exit before starting (used after elevation)")
	connect := flag.Bool("connect", false, "connect right after start")
	autostarted := flag.Bool(strings.TrimPrefix(autostart.Arg, "--"), false, "started by Windows logon: stay in the tray")
	cleanup := flag.Bool("cleanup", false, "uninstall: remove autostart, restore the system proxy and exit")
	flag.Parse()

	if *cleanup {
		runCleanup()
		return
	}
	if generatingBindings {
		// Only the bound method set matters here; skip the data dir, logs and
		// migration, which would otherwise run on every developer build.
		if err := wails.Run(&options.App{Bind: []any{&App{}}}); err != nil {
			fatal(err.Error())
		}
		return
	}

	if *waitPID != 0 {
		waitForExit(uint32(*waitPID), 10*time.Second)
	}

	st, err := store.Open()
	if err != nil {
		fatal("Не удалось открыть настройки: " + err.Error())
	}
	logger := openLog(st.Dir())

	app := NewApp(st, logger)
	app.connectOnStart = *connect
	app.autostarted = *autostarted

	err = wails.Run(&options.App{
		Title:            "Vlessia",
		Width:            960,
		Height:           640,
		MinWidth:         760,
		MinHeight:        520,
		BackgroundColour: &options.RGBA{R: 15, G: 17, B: 21, A: 255},
		AssetServer:      &assetserver.Options{Assets: assets},
		StartHidden:      *autostarted,
		OnStartup:        app.startup,
		OnBeforeClose:    app.beforeClose,
		OnShutdown:       app.shutdown,
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId:               "vlessia-3f1c2a9e",
			OnSecondInstanceLaunch: func(options.SecondInstanceData) { app.onSecondInstance() },
		},
		Bind: []any{app},
		Windows: &windows.Options{
			Theme: windows.Dark,
		},
	})
	if err != nil {
		fatal(err.Error())
	}
}

// openLog appends to app.log. It must not truncate: this runs before the
// single-instance check, so a second launch would wipe the running
// instance's log. The file is trimmed instead once it grows large.
// runCleanup undoes what the client changed in the system, for the
// uninstaller. Servers and settings are left in place.
func runCleanup() {
	autostart.Disable()
	st, err := store.Open()
	if err != nil {
		return
	}
	var saved *sysproxy.State
	st.Read(func(d *store.Data) { saved = d.SavedProxy })
	if saved != nil && sysproxy.Set(*saved) == nil {
		_ = st.Update(func(d *store.Data) { d.SavedProxy = nil })
	}
}

func openLog(dir string) *log.Logger {
	path := filepath.Join(dir, "app.log")
	if fi, err := os.Stat(path); err == nil && fi.Size() > 1<<20 {
		_ = os.Rename(path, path+".old")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return log.New(io.Discard, "", 0)
	}
	l := log.New(f, "", log.LstdFlags)
	l.Printf("---- запуск, pid %d", os.Getpid())
	return l
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
