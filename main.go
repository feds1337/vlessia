package main

import (
	"embed"
	"flag"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"github.com/feds1337/vlessia/internal/autostart"
	"github.com/feds1337/vlessia/internal/store"
	"github.com/feds1337/vlessia/internal/sysproxy"
	"github.com/feds1337/vlessia/internal/tun"
)

//go:embed all:frontend/dist
var assets embed.FS

// version is set at build time (-ldflags "-X main.version=v1.2.3").
var version = "dev"

// instanceID keys the single-instance lock. Development builds can override
// it (-X main.instanceID=vlessia-dev) to run next to an installed client.
var instanceID = "vlessia-3f1c2a9e"

func main() {
	// The privileged TUN helper (macOS/Linux) runs headless and must not
	// touch the user's data or open a window.
	if len(os.Args) > 1 && os.Args[1] == helperArg {
		os.Exit(runHelper(os.Args[2:]))
	}

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

	opts := &options.App{
		Title:            "Vlessia",
		Width:            960,
		Height:           640,
		MinWidth:         760,
		MinHeight:        520,
		BackgroundColour: &options.RGBA{R: 0xf5, G: 0xf6, B: 0xf8, A: 255},
		AssetServer:      &assetserver.Options{Assets: assets},
		StartHidden:      *autostarted,
		OnStartup:        app.startup,
		OnBeforeClose:    app.beforeClose,
		OnShutdown:       app.shutdown,
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId:               instanceID,
			OnSecondInstanceLaunch: func(options.SecondInstanceData) { app.onSecondInstance() },
		},
		Bind: []any{app},
	}
	platformOptions(opts, app)
	err = wails.Run(opts)
	if err != nil {
		fatal(err.Error())
	}
}

// runCleanup undoes what the client changed in the system, for the
// uninstaller. Servers and settings are left in place.
func runCleanup() {
	autostart.Disable()
	if tun.IsAdmin() {
		tun.Cleanup()
	}
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

// openLog appends to app.log. It must not truncate: this runs before the
// single-instance check, so a second launch would wipe the running
// instance's log. Instead the file is rotated to app.log.old when it grows
// past 1 MB, also while running (a long session must not fill the disk).
func openLog(dir string) *log.Logger {
	w := &rotatingFile{path: filepath.Join(dir, "app.log"), max: 1 << 20}
	if err := w.open(); err != nil {
		return log.New(io.Discard, "", 0)
	}
	l := log.New(w, "", log.LstdFlags)
	l.Printf("---- запуск, pid %d", os.Getpid())
	return l
}

type rotatingFile struct {
	path string
	max  int64
	mu   sync.Mutex
	f    *os.File
	size int64
}

func (r *rotatingFile) open() error {
	if fi, err := os.Stat(r.path); err == nil && fi.Size() > r.max {
		_ = os.Rename(r.path, r.path+".old")
	}
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	r.f, r.size = f, fi.Size()
	return nil
}

func (r *rotatingFile) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.size+int64(len(p)) > r.max {
		_ = r.f.Close()
		if err := r.open(); err != nil {
			return 0, err
		}
	}
	n, err := r.f.Write(p)
	r.size += int64(n)
	return n, err
}
