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

// trayLabels are the menu strings; the tray follows the UI language.
type trayLabels struct {
	tooltipOff        string
	tooltipOn         string
	tooltipConnecting string
	tooltipOffing     string
	statusOff         string
	statusOn          string
	statusConnecting  string
	statusOffing      string
	installUpdate     string // plus the version
	open              string
	connect           string
	disconnect        string
	proxyMode         string
	tunMode           string
	quit              string
}

var (
	trayRu = trayLabels{
		tooltipOff:        "Vlessia — отключено",
		tooltipOn:         "Vlessia — подключено",
		tooltipConnecting: "Vlessia — подключение…",
		tooltipOffing:     "Vlessia — отключение…",
		statusOff:         "Отключено",
		statusOn:          "Подключено",
		statusConnecting:  "Подключение…",
		statusOffing:      "Отключение…",
		installUpdate:     "Установить обновление",
		open:              "Открыть",
		connect:           "Подключить",
		disconnect:        "Отключить",
		proxyMode:         "Системный прокси",
		tunMode:           "TUN (весь трафик)",
		quit:              "Выход",
	}
	trayEn = trayLabels{
		tooltipOff:        "Vlessia — disconnected",
		tooltipOn:         "Vlessia — connected",
		tooltipConnecting: "Vlessia — connecting…",
		tooltipOffing:     "Vlessia — disconnecting…",
		statusOff:         "Disconnected",
		statusOn:          "Connected",
		statusConnecting:  "Connecting…",
		statusOffing:      "Disconnecting…",
		installUpdate:     "Install update",
		open:              "Open",
		connect:           "Connect",
		disconnect:        "Disconnect",
		proxyMode:         "System proxy",
		tunMode:           "TUN (all traffic)",
		quit:              "Quit",
	}
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

	mu           sync.Mutex
	labels       trayLabels
	lastState    string
	lastMode     string
	lastUpdState string
	lastUpdVer   string
}

func StartTray(app *App) *Tray {
	t := &Tray{
		app:      app,
		ready:    make(chan struct{}),
		iconOn:   makeIcon(color.RGBA{0x0e, 0x7c, 0x86, 0xff}),
		iconOff:  makeIcon(color.RGBA{0x6b, 0x72, 0x80, 0xff}),
		iconWait: makeIcon(color.RGBA{0xc2, 0x7c, 0x0e, 0xff}),
		labels:   trayRu,
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
	l := t.labels
	systray.SetIcon(t.iconOff)
	systray.SetTooltip(l.tooltipOff)
	systray.SetOnTapped(func() { go t.app.ShowWindow() })

	t.mStatus = systray.AddMenuItem(l.statusOff, "")
	t.mStatus.Disable()
	t.mUpdate = systray.AddMenuItem(l.installUpdate, "")
	t.mUpdate.Hide()
	systray.AddSeparator()
	mShow := systray.AddMenuItem(l.open, "")
	t.mToggle = systray.AddMenuItem(l.connect, "")
	systray.AddSeparator()
	t.mProxy = systray.AddMenuItemCheckbox(l.proxyMode, "", true)
	t.mTun = systray.AddMenuItemCheckbox(l.tunMode, "", false)
	systray.AddSeparator()
	mQuit := systray.AddMenuItem(l.quit, "")

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

// stateTitles applies the current state to the icon, tooltip and the
// status / toggle items. t.mu must be held.
func (t *Tray) stateTitles() {
	l := t.labels
	switch t.lastState {
	case "connected":
		systray.SetIcon(t.iconOn)
		systray.SetTooltip(l.tooltipOn)
		t.mStatus.SetTitle(l.statusOn)
		t.mToggle.SetTitle(l.disconnect)
	case "disconnecting":
		systray.SetIcon(t.iconWait)
		systray.SetTooltip(l.tooltipOffing)
		t.mStatus.SetTitle(l.statusOffing)
		t.mToggle.SetTitle(l.connect)
	case "connecting":
		systray.SetIcon(t.iconWait)
		systray.SetTooltip(l.tooltipConnecting)
		t.mStatus.SetTitle(l.statusConnecting)
		t.mToggle.SetTitle(l.disconnect)
	default:
		systray.SetIcon(t.iconOff)
		systray.SetTooltip(l.tooltipOff)
		t.mStatus.SetTitle(l.statusOff)
		t.mToggle.SetTitle(l.connect)
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
	t.stateTitles()
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
		t.mUpdate.SetTitle(t.labels.installUpdate + " " + info.Latest)
		t.mUpdate.Show()
	} else {
		t.mUpdate.Hide()
	}
}

// SetLang switches the menu language and re-applies the current titles.
// The ready channel is waited on because the items exist only after the
// systray loop starts.
func (t *Tray) SetLang(lang string) {
	t.mu.Lock()
	labels := trayRu
	if lang == "en" {
		labels = trayEn
	}
	t.labels = labels
	if !t.readyClosed() {
		t.mu.Unlock()
		<-t.ready
		t.mu.Lock()
		defer t.mu.Unlock()
	}
	t.mProxy.SetTitle(labels.proxyMode)
	t.mTun.SetTitle(labels.tunMode)
	if t.lastUpdState == "available" {
		t.mUpdate.SetTitle(labels.installUpdate + " " + t.lastUpdVer)
	}
	t.stateTitles()
}

func (t *Tray) readyClosed() bool {
	select {
	case <-t.ready:
		return true
	default:
		return false
	}
}

func (t *Tray) Stop() {
	systray.Quit()
}

func makeIcon(c color.RGBA) []byte {
	return icon.ICO(icon.Shield(32, c), 32)
}
