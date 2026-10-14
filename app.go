package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/feds1337/vlessia/internal/autostart"
	"github.com/feds1337/vlessia/internal/core"
	"github.com/feds1337/vlessia/internal/openvpn"
	"github.com/feds1337/vlessia/internal/profile"
	"github.com/feds1337/vlessia/internal/qr"
	"github.com/feds1337/vlessia/internal/sound"
	"github.com/feds1337/vlessia/internal/store"
	"github.com/feds1337/vlessia/internal/sysproxy"
	"github.com/feds1337/vlessia/internal/tun"
	"github.com/feds1337/vlessia/internal/xray"
)

// tunSession is the system side of TUN mode (routes, DNS, the helper on
// macOS/Linux). Done, if non-nil, is closed when it stops on its own.
type tunSession interface {
	Close()
	Done() <-chan struct{}
}

// watchTUN disconnects when TUN mode stops behind the client's back (the
// helper was killed), instead of showing a connection that no longer works.
func (a *App) watchTUN(sess tunSession) {
	<-sess.Done()
	a.mu.Lock()
	current := a.tunSess == sess
	a.mu.Unlock()
	if !current {
		return
	}
	a.log.Printf("режим «Весь трафик» остановлен системой")
	_ = a.Disconnect()
	a.setStatus(func(s *Status) {
		s.Error = "Режим «Весь трафик» остановлен: служебный процесс завершился"
	})
}

// ErrNeedAdmin is returned when TUN mode is requested without elevation;
// the UI recognises the message and offers to restart as administrator.
var ErrNeedAdmin = errors.New("need_admin")

type Status struct {
	State     string `json:"state"` // disconnected | connecting | connected
	Mode      string `json:"mode"`
	ServerID  string `json:"serverId"`
	Error     string `json:"error"`
	Admin     bool   `json:"admin"`
	Interface string `json:"interface"`
}

type AppState struct {
	Servers       []*profile.Server     `json:"servers"`
	Subscriptions []*store.Subscription `json:"subscriptions"`
	Selected      string                `json:"selected"`
	Settings      store.Settings        `json:"settings"`
	Status        Status                `json:"status"`
}

// App is bound to the frontend; its exported methods are callable from JS.
type App struct {
	ctx   context.Context
	store *store.Store
	lib   *core.Library
	log   *log.Logger
	tray  *Tray

	connectOnStart bool
	autostarted    bool // launched by Windows logon
	quitting       bool
	// visible: the window is shown. Live traffic numbers are only sent to
	// the UI then, so a VPN running all day in the tray costs no UI work.
	visible atomic.Bool

	upd updater

	opMu sync.Mutex // serialises Connect/Disconnect

	mu        sync.Mutex   // guards the fields below
	eng       *core.Engine // the running connection, if any
	tunSess   tunSession   // system side of TUN mode, if any
	status    Status
	stopStats chan struct{}
}

func NewApp(st *store.Store, logger *log.Logger) *App {
	a := &App{
		store:  st,
		log:    logger,
		status: Status{State: "disconnected", Admin: tun.IsAdmin()},
	}
	a.lib = &core.Library{Store: st, Log: logger, HTTP: a.httpClient, Changed: a.emitState}
	return a
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.visible.Store(!a.autostarted)
	a.restoreLeftoverProxy()
	a.lib.UpgradeServers()
	a.tray = StartTray(a)
	go a.lib.AutoUpdateLoop()
	go a.updateLoop()
	a.refreshAutostart()
	var autoConnect bool
	a.store.Read(func(d *store.Data) { autoConnect = d.Settings.AutoConnect })
	if a.connectOnStart || autoConnect {
		go a.autoConnect()
	}
}

// autoConnect connects at startup. Right after Windows logon the network
// may not be up yet, so transient failures are retried for about a minute.
func (a *App) autoConnect() {
	var err error
	for attempt := 0; attempt < 12; attempt++ {
		if err = a.Connect(); err == nil {
			return
		}
		msg := err.Error()
		if errors.Is(err, ErrNeedAdmin) || errors.Is(err, openvpn.ErrNeedAuth) || strings.Contains(msg, "выберите сервер") {
			break
		}
		a.log.Printf("автоподключение, попытка %d: %v", attempt+1, err)
		time.Sleep(5 * time.Second)
	}
	a.log.Printf("автоподключение не удалось: %v", err)
	// Started hidden in the tray: show why it did not connect.
	a.ShowWindow()
}

// AutostartState is shown in the settings.
type AutostartState struct {
	Enabled bool `json:"enabled"`
	// Elevated means the task starts with administrator rights (TUN works);
	// otherwise only system-proxy mode can start unattended.
	Elevated bool `json:"elevated"`
	Admin    bool `json:"admin"`
}

func (a *App) GetAutostart() AutostartState {
	st := autostart.Get()
	return AutostartState{Enabled: st.Mode != autostart.ModeOff, Elevated: st.Mode == autostart.ModeTask, Admin: tun.IsAdmin()}
}

func (a *App) SetAutostart(on bool) (AutostartState, error) {
	if !on {
		autostart.Disable()
		return a.GetAutostart(), nil
	}
	exe, err := os.Executable()
	if err != nil {
		return a.GetAutostart(), err
	}
	err = autostart.Enable(exe)
	return a.GetAutostart(), err
}

// refreshAutostart re-registers autostart when the executable moved (e.g.
// reinstalled elsewhere). An elevated task is only rewritten when running
// elevated, so it is never downgraded.
func (a *App) refreshAutostart() {
	st := autostart.Get()
	exe, err := os.Executable()
	if err != nil || st.Mode == autostart.ModeOff || strings.EqualFold(st.Command, exe) {
		return
	}
	if st.Mode == autostart.ModeTask && !tun.IsAdmin() {
		return
	}
	if err := autostart.Enable(exe); err != nil {
		a.log.Printf("автозапуск: %v", err)
	}
}

// GetVersion returns the build version.
func (a *App) GetVersion() string { return version }

func (a *App) beforeClose(ctx context.Context) bool {
	if a.quitting {
		return false
	}
	hideOnClose(ctx)
	a.visible.Store(false)
	return true
}

func (a *App) shutdown(ctx context.Context) {
	_ = a.Disconnect()
	a.tray.Stop()
}

func (a *App) onSecondInstance() {
	a.ShowWindow()
}

// ShowWindow brings the main window to front.
func (a *App) ShowWindow() {
	a.visible.Store(true)
	runtime.WindowShow(a.ctx)
	runtime.WindowUnminimise(a.ctx)
}

// Quit exits the application, disconnecting first.
func (a *App) Quit() {
	a.quitting = true
	runtime.Quit(a.ctx)
}

// ---------- state ----------

func (a *App) GetState() AppState {
	var s AppState
	a.store.Read(func(d *store.Data) {
		s.Servers = append([]*profile.Server{}, d.Servers...)
		s.Subscriptions = append([]*store.Subscription{}, d.Subscriptions...)
		s.Selected = d.Selected
		s.Settings = d.Settings
	})
	a.mu.Lock()
	s.Status = a.status
	a.mu.Unlock()
	return s
}

func (a *App) emitState() {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "state", a.GetState())
	}
	a.mu.Lock()
	st := a.status
	a.mu.Unlock()
	if a.tray != nil {
		a.tray.Update(st)
	}
}

func (a *App) setStatus(fn func(s *Status)) {
	a.mu.Lock()
	fn(&a.status)
	a.mu.Unlock()
	a.emitState()
}

// ---------- servers & subscriptions ----------

// ImportText adds servers from share links, or a subscription if the text
// is an http(s) URL. Returns how many servers were added.
func (a *App) ImportText(text string) (int, error) { return a.lib.ImportText(text) }

// ImportFile asks for a file (.conf from AmneziaVPN / WireGuard, .ovpn, a
// text file with links, or a picture of a QR code) and imports it.
func (a *App) ImportFile() (int, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Импорт конфигурации",
		Filters: []runtime.FileFilter{
			{DisplayName: "Конфигурации и QR-коды", Pattern: "*.conf;*.ovpn;*.txt;*.vpn;*.png;*.jpg;*.jpeg;*.gif"},
			{DisplayName: "Все файлы", Pattern: "*.*"},
		},
	})
	if err != nil || path == "" {
		return 0, err
	}
	fi, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	if fi.Size() > 4<<20 {
		return 0, errors.New("файл слишком большой")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	return a.lib.ImportFile(b, name)
}

// ScanQR finds a QR code with a share link or subscription on the screen
// (e.g. in a messenger) and imports it. The window steps aside meanwhile.
func (a *App) ScanQR() (int, error) {
	runtime.WindowMinimise(a.ctx)
	time.Sleep(400 * time.Millisecond) // let the minimise animation finish
	text, err := qr.FromScreen()
	a.ShowWindow()
	if errors.Is(err, qr.ErrNotFound) {
		msg := "QR-код на экране не найден. Откройте его так, чтобы он был виден целиком, или выберите картинку с кодом через «Добавить → Открыть файл…»"
		if goruntime.GOOS == "darwin" {
			msg += ". На macOS разрешите Vlessia запись экрана: Системные настройки → Конфиденциальность и безопасность → Запись экрана"
		}
		return 0, errors.New(msg)
	}
	if err != nil {
		return 0, err
	}
	return a.ImportText(text)
}

// WelcomePending reports whether the first-run offer should be shown.
func (a *App) WelcomePending() bool { return a.lib.WelcomePending() }

// WelcomeSeen records that the first-run offer was shown.
func (a *App) WelcomeSeen() error { return a.lib.SetWelcomeSeen() }

// SetOpenVPNCredentials stores the login for an OpenVPN profile that uses
// auth-user-pass.
func (a *App) SetOpenVPNCredentials(id, user, pass string) error {
	return a.lib.SetOpenVPNCredentials(id, user, pass)
}

// ImportClipboard imports whatever is in the clipboard.
func (a *App) ImportClipboard() (int, error) {
	text, err := runtime.ClipboardGetText(a.ctx)
	if err != nil {
		return 0, err
	}
	return a.ImportText(text)
}

func (a *App) CopyLink(id string) error {
	s := a.store.Server(id)
	if s == nil {
		return errors.New("сервер не найден")
	}
	return runtime.ClipboardSetText(a.ctx, s.Link)
}

func (a *App) DeleteServer(id string) error { return a.lib.DeleteServer(id) }

// SelectServer makes the server current, reconnecting if already connected.
func (a *App) SelectServer(id string) error {
	if err := a.lib.Select(id); err != nil {
		return err
	}
	return a.reconnectIfActive()
}

func (a *App) AddSubscription(name, rawURL string) (int, error) {
	return a.lib.AddSubscription(name, rawURL)
}

func (a *App) DeleteSubscription(id string) error { return a.lib.DeleteSubscription(id) }

// UpdateSubscription downloads the subscription, refreshes its metadata and
// replaces its servers.
func (a *App) UpdateSubscription(id string) (int, error) { return a.lib.UpdateSubscription(id) }

// RenameSubscription sets a custom name; an empty name restores the title
// sent by the server.
func (a *App) RenameSubscription(id, name string) error { return a.lib.RenameSubscription(id, name) }

// OpenURL opens a support / web page link in the default browser.
func (a *App) OpenURL(u string) {
	runtime.BrowserOpenURL(a.ctx, u)
}

func (a *App) UpdateAllSubscriptions() error { return a.lib.UpdateAllSubscriptions() }

// ---------- settings ----------

func (a *App) SaveSettings(s store.Settings) error {
	if s.Mode != "proxy" && s.Mode != "tun" {
		return errors.New("неизвестный режим")
	}
	if s.SocksPort <= 0 || s.SocksPort > 65535 || s.HTTPPort <= 0 || s.HTTPPort > 65535 || s.SocksPort == s.HTTPPort {
		return errors.New("неверные порты")
	}
	var dns []string
	for _, d := range s.DNS {
		if d = strings.TrimSpace(d); d != "" {
			dns = append(dns, d)
		}
	}
	s.DNS = dns
	var old store.Settings
	if err := a.store.Update(func(d *store.Data) { old, d.Settings = d.Settings, s }); err != nil {
		return err
	}
	a.emitState()
	if !affectsConnection(old, s) {
		return nil
	}
	return a.reconnectIfActive()
}

// affectsConnection reports whether a settings change requires
// reconnecting; sound, autostart or update options do not.
func affectsConnection(a, b store.Settings) bool {
	return a.Mode != b.Mode || a.SocksPort != b.SocksPort || a.HTTPPort != b.HTTPPort ||
		a.BypassRU != b.BypassRU || a.LogLevel != b.LogLevel || strings.Join(a.DNS, ",") != strings.Join(b.DNS, ",")
}

// SetMode switches between system proxy and TUN.
func (a *App) SetMode(mode string) error {
	var s store.Settings
	a.store.Read(func(d *store.Data) { s = d.Settings })
	s.Mode = mode
	return a.SaveSettings(s)
}

// RestartAsAdmin relaunches the app elevated and connects there.
func (a *App) RestartAsAdmin() error {
	args := fmt.Sprintf("--wait-pid %d --connect", os.Getpid())
	if err := tun.RelaunchElevated(args); err != nil {
		return fmt.Errorf("перезапуск отменён: %w", err)
	}
	a.Quit()
	return nil
}

// ---------- connection ----------

func (a *App) Toggle() error {
	a.mu.Lock()
	state := a.status.State
	a.mu.Unlock()
	switch state {
	case "disconnected":
		return a.Connect()
	case "disconnecting":
		return nil
	}
	return a.Disconnect()
}

func (a *App) reconnectIfActive() error {
	if a.instance() == nil {
		return nil
	}
	if err := a.Disconnect(); err != nil {
		return err
	}
	return a.Connect()
}

func (a *App) Connect() error {
	a.opMu.Lock()
	defer a.opMu.Unlock()
	if a.instance() != nil {
		return nil
	}

	var settings store.Settings
	a.store.Read(func(d *store.Data) { settings = d.Settings })
	srv := a.lib.Selected()
	if srv == nil {
		return errors.New("выберите сервер")
	}
	settings.Mode = core.EffectiveMode(srv, settings.Mode)

	a.setStatus(func(s *Status) {
		*s = Status{State: "connecting", Mode: settings.Mode, ServerID: srv.ID, Admin: tun.IsAdmin()}
	})

	eng, sess, err := a.start(srv, settings)
	if err != nil {
		a.log.Printf("подключение не удалось: %v", err)
		a.setStatus(func(s *Status) {
			s.State = "disconnected"
			s.Error = err.Error()
		})
		return err
	}
	stop := make(chan struct{})
	a.mu.Lock()
	a.eng = eng
	a.tunSess = sess
	a.stopStats = stop
	a.mu.Unlock()
	if sess != nil && sess.Done() != nil {
		go a.watchTUN(sess)
	}
	go a.statsLoop(eng.Inst, stop)
	a.log.Printf("подключено: %s (%s), режим %s", srv.Name, srv.Address, settings.Mode)
	if settings.Sound {
		sound.PlayConnected()
	}
	a.setStatus(func(s *Status) {
		s.State = "connected"
		s.Interface = eng.Interface
	})
	return nil
}

func (a *App) instance() *xray.Instance {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.eng == nil {
		return nil
	}
	return a.eng.Inst
}

// start launches the engine for srv, then TUN mode or the system proxy.
func (a *App) start(srv *profile.Server, settings store.Settings) (*core.Engine, tunSession, error) {
	eng, err := core.StartEngine(a.lib, srv, settings, a.store.Dir(), core.Hooks{
		PrepareTUN: func(serverIP string, opts *xray.Options) error { return a.prepareTUN(srv, serverIP, opts) },
	})
	if err != nil {
		return nil, nil, err
	}
	if settings.Mode == "tun" {
		sess, err := a.activateTUN(eng.ServerIP, settings)
		if err != nil {
			_ = eng.Close()
			return nil, nil, err
		}
		return eng, sess, nil
	}
	if err := a.enableSystemProxy(settings.HTTPPort); err != nil {
		_ = eng.Close()
		return nil, nil, fmt.Errorf("не удалось включить системный прокси: %w", err)
	}
	return eng, nil, nil
}

func (a *App) Disconnect() error {
	a.opMu.Lock()
	defer a.opMu.Unlock()
	a.mu.Lock()
	eng, sess, stop, mode := a.eng, a.tunSess, a.stopStats, a.status.Mode
	a.eng, a.tunSess = nil, nil
	a.mu.Unlock()
	if eng == nil {
		return nil
	}
	a.setStatus(func(s *Status) { s.State = "disconnecting" })
	started := time.Now()
	step := func(name string, fn func()) {
		t := time.Now()
		fn()
		if d := time.Since(t); d > 300*time.Millisecond {
			a.log.Printf("отключение: %s занял %v", name, d.Round(time.Millisecond))
		}
	}

	close(stop)
	if mode == "proxy" {
		step("возврат системного прокси", a.restoreSystemProxy)
	}
	if sess != nil {
		step("остановка режима «Весь трафик»", sess.Close)
	}
	var err error
	step("остановка xray", func() { err = eng.CloseXray() })
	step("остановка туннеля", eng.CloseBridge)
	if mode == "tun" {
		step("сброс DNS-кэша", tun.FlushDNS)
	}
	a.log.Printf("отключено за %v", time.Since(started).Round(time.Millisecond))
	a.setStatus(func(s *Status) { *s = Status{State: "disconnected", Admin: tun.IsAdmin()} })
	return err
}

func (a *App) enableSystemProxy(port int) error {
	cur, err := sysproxy.Get()
	if err != nil {
		return err
	}
	var saved *sysproxy.State
	a.store.Read(func(d *store.Data) { saved = d.SavedProxy })
	if saved == nil {
		if err := a.store.Update(func(d *store.Data) { d.SavedProxy = &cur }); err != nil {
			return err
		}
	}
	return sysproxy.Enable(net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
}

func (a *App) restoreSystemProxy() {
	var saved *sysproxy.State
	a.store.Read(func(d *store.Data) { saved = d.SavedProxy })
	if saved == nil {
		saved = &sysproxy.State{}
	}
	if err := sysproxy.Set(*saved); err != nil {
		a.log.Printf("не удалось вернуть настройки прокси: %v", err)
		return
	}
	_ = a.store.Update(func(d *store.Data) { d.SavedProxy = nil })
}

// restoreLeftoverProxy undoes our system proxy if the app was killed while
// connected.
func (a *App) restoreLeftoverProxy() {
	var saved *sysproxy.State
	a.store.Read(func(d *store.Data) { saved = d.SavedProxy })
	if saved != nil {
		a.log.Printf("восстанавливаю системный прокси после аварийного завершения")
		a.restoreSystemProxy()
	}
}

func (a *App) statsLoop(inst *xray.Instance, stop chan struct{}) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			runtime.EventsEmit(a.ctx, "traffic", map[string]int64{"up": 0, "down": 0})
			return
		case <-t.C:
			// Always read: the counters accumulate until reset.
			up, down := inst.Traffic()
			if a.visible.Load() {
				runtime.EventsEmit(a.ctx, "traffic", map[string]int64{"up": up, "down": down})
			}
		}
	}
}

func (a *App) localProxyURL() *url.URL {
	if a.instance() == nil {
		return nil
	}
	var port int
	a.store.Read(func(d *store.Data) { port = d.Settings.HTTPPort })
	return core.LocalProxy(port)
}

// ---------- latency ----------

// PingAll measures TCP connect time to every server, emitting a "ping"
// event per server as results arrive. -1 means unreachable.
func (a *App) PingAll() {
	var servers []*profile.Server
	a.store.Read(func(d *store.Data) { servers = append(servers, d.Servers...) })

	a.mu.Lock()
	iface := a.status.Interface
	a.mu.Unlock()
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	if iface != "" {
		// In TUN mode bypass the tunnel, otherwise every port "answers".
		dialer.Control = tun.BindToInterface(iface)
	}

	core.PingAll(servers, dialer, func(id string, ms int64) {
		runtime.EventsEmit(a.ctx, "ping", map[string]any{"id": id, "ms": ms})
	})
}

// TestConnection measures an HTTPS request through the active tunnel.
func (a *App) TestConnection() (int64, error) { return core.TestConnection(a.localProxyURL()) }

// ---------- logs ----------

func (a *App) GetLogs() string { return core.Logs(a.store.Dir()) }

func (a *App) OpenDataDir() {
	_ = openFolder(a.store.Dir())
}

// GetPlatform returns the operating system ("windows", "darwin", "linux"),
// for platform-specific wording in the UI.
func (a *App) GetPlatform() string { return goruntime.GOOS }
