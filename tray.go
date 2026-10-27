//go:build !darwin

package main

import (
	"image/color"
	"runtime"
	"sync"

	"fyne.io/systray"

	"github.com/feds1337/vlessia/internal/icon"
	"github.com/feds1337/vlessia/internal/store"
)

// Tray is the notification-area icon with a quick-access menu.
type Tray struct {
	app      *App
	ready    chan struct{}
	iconOn   []byte
	iconOff  []byte
	iconWait []byte
	mToggle  *systray.MenuItem
	mProxy   *systray.MenuItem
	mTun     *systray.MenuItem
	mStatus  *systray.MenuItem
	mUpdate  *systray.MenuItem

	mu          sync.Mutex
	lastState   string
	lastMode    string
	lastUpdState string
	lastUpdVer  string
}

func StartTray(app *App) *Tray {
	t := &Tray{
		app:      app,
		ready:    make(chan struct{}),
		iconOn:   makeIcon(color.RGBA{0x0e, 0x7c, 0x86, 0xff}),
		iconOff:  makeIcon(color.RGBA{0x6b, 0x72, 0x80, 0xff}),
		iconWait: makeIcon(color.RGBA{0xc2, 0x7c, 0x0e, 0xff}),
	}
	// systray creates a Win32 window and runs GetMessageW on the calling
	// thread, but only calls LockOSThread in init() (the main goroutine).
	// Lock this goroutine to its OS thread so the Go scheduler never
	// migrates the message loop away from the thread that owns the window.
	go func() {
		runtime.LockOSThread()
		systray.Run(t.onReady, func() {})
	}()
	return t
}

func (t *Tray) onReady() {
	systray.SetIcon(t.iconOff)
	systray.SetTooltip("Vlessia — отключено")
	systray.SetOnTapped(func() { go t.app.ShowWindow() })

	t.mStatus = systray.AddMenuItem("Отключено", "")
	t.mStatus.Disable()
	t.mUpdate = systray.AddMenuItem("Установить обновление", "")
	t.mUpdate.Hide()
	systray.AddSeparator()
	mShow := systray.AddMenuItem("Открыть", "")
	t.mToggle = systray.AddMenuItem("Подключить", "")
	systray.AddSeparator()
	t.mProxy = systray.AddMenuItemCheckbox("Системный прокси", "", true)
	t.mTun = systray.AddMenuItemCheckbox("TUN (весь трафик)", "", false)
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Выход", "")

	var mode string
	t.app.store.Read(func(d *store.Data) { mode = d.Settings.Mode })
	t.mu.Lock()
	t.lastState = "disconnected"
	t.lastMode = mode
	t.setModeChecks(mode)
	t.mu.Unlock()
	close(t.ready)

	go func() {
		for {
			select {
			case <-mShow.ClickedCh:
				go t.app.ShowWindow()
			case <-t.mUpdate.ClickedCh:
				go func() {
					t.app.ShowWindow()
					_ = t.app.ApplyUpdate()
				}()
			case <-t.mToggle.ClickedCh:
				go t.toggle()
			case <-t.mProxy.ClickedCh:
				go t.setMode("proxy")
			case <-t.mTun.ClickedCh:
				go t.setMode("tun")
			case <-mQuit.ClickedCh:
				go t.app.Quit()
				return
			}
		}
	}()
}

func (t *Tray) toggle() {
	if err := t.app.Toggle(); err != nil {
		t.app.ShowWindow()
	}
}

func (t *Tray) setMode(mode string) {
	t.mu.Lock()
	t.lastMode = mode
	t.setModeChecks(mode)
	t.mu.Unlock()
	if err := t.app.SetMode(mode); err != nil {
		t.app.ShowWindow()
	}
}

func (t *Tray) setModeChecks(mode string) {
	if mode == "tun" {
		t.mTun.Check()
		t.mProxy.Uncheck()
	} else {
		t.mProxy.Check()
		t.mTun.Uncheck()
	}
}

// Update reflects the connection status in the tray.
func (t *Tray) Update(st Status) {
	select {
	case <-t.ready:
	default:
		return
	}
	var mode string
	t.app.store.Read(func(d *store.Data) { mode = d.Settings.Mode })

	t.mu.Lock()
	defer t.mu.Unlock()
	if mode != t.lastMode {
		t.lastMode = mode
		t.setModeChecks(mode)
	}
	if st.State == t.lastState {
		return
	}
	t.lastState = st.State

	switch st.State {
	case "connected":
		systray.SetIcon(t.iconOn)
		systray.SetTooltip("Vlessia — подключено")
		t.mStatus.SetTitle("Подключено")
		t.mToggle.SetTitle("Отключить")
	case "disconnecting":
		systray.SetIcon(t.iconWait)
		systray.SetTooltip("Vlessia — отключение…")
		t.mStatus.SetTitle("Отключение…")
		t.mToggle.SetTitle("Подключить")
	case "connecting":
		systray.SetIcon(t.iconWait)
		systray.SetTooltip("Vlessia — подключение…")
		t.mStatus.SetTitle("Подключение…")
		t.mToggle.SetTitle("Отключить")
	default:
		systray.SetIcon(t.iconOff)
		systray.SetTooltip("Vlessia — отключено")
		t.mStatus.SetTitle("Отключено")
		t.mToggle.SetTitle("Подключить")
	}
}

// SetUpdate shows the update item while a new version is available.
func (t *Tray) SetUpdate(info UpdateInfo) {
	select {
	case <-t.ready:
	default:
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if info.State == t.lastUpdState && info.Latest == t.lastUpdVer {
		return
	}
	t.lastUpdState = info.State
	t.lastUpdVer = info.Latest
	if info.State == "available" {
		t.mUpdate.SetTitle("Установить обновление " + info.Latest)
		t.mUpdate.Show()
	} else {
		t.mUpdate.Hide()
	}
}

func (t *Tray) Stop() {
	systray.Quit()
}

func makeIcon(c color.RGBA) []byte {
	return icon.ICO(icon.Shield(32, c), 32)
}
