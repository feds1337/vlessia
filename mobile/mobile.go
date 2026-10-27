// Package mobile is Vlessia for Android, built into an AAR with
// gomobile bind (see android/).
//
// The Android app shows the same web UI as the desktop client and forwards
// its calls to Call by method name, so client mirrors the desktop App's
// methods. The VPN interface comes from Android's VpnService through Host:
// Xray reads all traffic from it, and the app excludes itself from the VPN,
// so Xray's own connections to the servers go straight to the network.
package mobile

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/feds1337/vlessia/internal/core"
	"github.com/feds1337/vlessia/internal/profile"
	"github.com/feds1337/vlessia/internal/sound"
	"github.com/feds1337/vlessia/internal/store"
	"github.com/feds1337/vlessia/internal/xray"
)

// Host is implemented by the Android app.
type Host interface {
	// Emit delivers an event ("state", "traffic", "ping", "update", "notice")
	// with a JSON payload to the UI.
	Emit(name, payload string)
	// OpenTun starts the VPN service, establishes the VPN interface with
	// dns as its DNS server and returns the interface's file descriptor,
	// which then belongs to Go.
	OpenTun(dns string) (int32, error)
	// TunClosed tells the VPN service that the interface is gone.
	TunClosed()
	OpenURL(url string)
	SetClipboard(text string)
	// PlaySound plays a WAV file.
	PlaySound(path string)
	// InstallPackage opens the system installer for a downloaded APK.
	InstallPackage(path string)
	// OpenVpnSettings opens the system VPN settings (always-on VPN).
	OpenVpnSettings()
}

var (
	appMu sync.Mutex
	app   *client
)

// Start opens the data in dataDir and prepares the client. version is the
// app version ("0.3.0"), abi the device's preferred ABI ("arm64-v8a"),
// used to pick the update package. Calling it again is a no-op.
func Start(dataDir, version, abi string, host Host) error {
	appMu.Lock()
	defer appMu.Unlock()
	if app != nil {
		return nil
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	// The store reads its location from here; Android has no config dir.
	_ = os.Setenv("VLESSIA_DATA", dataDir)
	st, err := store.Open()
	if err != nil {
		return err
	}
	logFile := openLog(filepath.Join(dataDir, "app.log"))
	logger := log.New(logFile, "", log.LstdFlags)
	logger.Printf("Vlessia %s (Android, %s) запущена", version, abi)

	a := &client{
		store:   st,
		meter:   core.NewMeter(st),
		log:     logger,
		logFile: logFile,
		host:    host,
		version: version,
		abi:     abi,
		status:  connStatus{State: "disconnected"},
	}
	a.lib = &core.Library{Store: st, Log: logger, HTTP: a.httpClient, Changed: a.emitState}
	a.lib.UpgradeServers()
	go a.lib.AutoUpdateLoop()
	go a.updateLoop()
	app = a
	return nil
}

// openLog appends to path, starting afresh when it grew past 1 MB.
func openLog(path string) *os.File {
	if fi, err := os.Stat(path); err == nil && fi.Size() > 1<<20 {
		_ = os.Rename(path, path+".old")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return os.Stderr
	}
	return f
}

func get() (*client, error) {
	appMu.Lock()
	defer appMu.Unlock()
	if app == nil {
		return nil, errors.New("клиент не запущен")
	}
	return app, nil
}

// Call invokes the client method named method with args, a JSON array, and
// returns its result as JSON ("" for none). It blocks; call it off the
// main thread.
func Call(method, args string) (string, error) {
	a, err := get()
	if err != nil {
		return "", err
	}
	m := reflect.ValueOf(a).MethodByName(method)
	if !m.IsValid() || !isPublic(method) {
		return "", fmt.Errorf("неизвестный метод %s", method)
	}
	var raw []json.RawMessage
	if args != "" {
		if err := json.Unmarshal([]byte(args), &raw); err != nil {
			return "", fmt.Errorf("%s: аргументы: %w", method, err)
		}
	}
	t := m.Type()
	if len(raw) != t.NumIn() {
		return "", fmt.Errorf("%s: ожидается аргументов: %d", method, t.NumIn())
	}
	in := make([]reflect.Value, t.NumIn())
	for i := range in {
		v := reflect.New(t.In(i))
		if err := json.Unmarshal(raw[i], v.Interface()); err != nil {
			return "", fmt.Errorf("%s: аргумент %d: %w", method, i+1, err)
		}
		in[i] = v.Elem()
	}
	out := m.Call(in)
	errType := reflect.TypeOf((*error)(nil)).Elem()
	if n := len(out); n > 0 && t.Out(n-1) == errType {
		if e := out[n-1]; !e.IsNil() {
			return "", e.Interface().(error)
		}
		out = out[:n-1]
	}
	if len(out) == 0 {
		return "", nil
	}
	b, err := json.Marshal(out[0].Interface())
	return string(b), err
}

func isPublic(name string) bool { return name != "" && name[0] >= 'A' && name[0] <= 'Z' }

// SetVisible tells the client whether the UI is on screen; live traffic
// numbers are only sent then.
func SetVisible(visible bool) {
	if a, err := get(); err == nil {
		a.visible.Store(visible)
	}
}

// Connect connects to the selected server (used by the VPN service for
// always-on VPN and by the quick settings tile).
func Connect() error {
	a, err := get()
	if err != nil {
		return err
	}
	return a.Connect()
}

// Disconnect disconnects, e.g. when Android revokes the VPN.
func Disconnect() {
	if a, err := get(); err == nil {
		_ = a.Disconnect()
	}
}

// State returns "disconnected", "connecting", "connected" or
// "disconnecting".
func State() string {
	a, err := get()
	if err != nil {
		return "disconnected"
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.status.State
}

// ServerName returns the name of the connected (or selected) server.
func ServerName() string {
	a, err := get()
	if err != nil {
		return ""
	}
	a.mu.Lock()
	id := a.status.ServerID
	a.mu.Unlock()
	if s := a.store.Server(id); s != nil {
		return s.Name
	}
	if s := a.lib.Selected(); s != nil {
		return s.Name
	}
	return ""
}

// AutoConnect reports whether the user asked to connect when the app opens.
func AutoConnect() bool {
	a, err := get()
	if err != nil {
		return false
	}
	var on bool
	a.store.Read(func(d *store.Data) { on = d.Settings.AutoConnect })
	return on
}

// ---------- client ----------

type connStatus struct {
	State     string `json:"state"` // disconnected | connecting | connected | disconnecting
	Mode      string `json:"mode"`
	ServerID  string `json:"serverId"`
	Error     string `json:"error"`
	Admin     bool   `json:"admin"`
	Interface string `json:"interface"`
}

type appState struct {
	Servers       []*profile.Server     `json:"servers"`
	Subscriptions []*store.Subscription `json:"subscriptions"`
	Selected      string                `json:"selected"`
	Settings      store.Settings        `json:"settings"`
	Status        connStatus            `json:"status"`
}

// client holds the client state; its exported methods are called from the UI.
type client struct {
	store     *store.Store
	lib       *core.Library
	meter     *core.Meter
	log       *log.Logger
	logFile   *os.File
	host      Host
	version   string
	abi       string
	visible   atomic.Bool
	autoFails atomic.Int32
	upd       updater

	opMu sync.Mutex // serialises Connect/Disconnect

	mu        sync.Mutex // guards the fields below
	eng       *core.Engine
	status    connStatus
	stopStats chan struct{}
}

func (a *client) emit(name string, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		a.log.Printf("событие %s: %v", name, err)
		return
	}
	a.host.Emit(name, string(b))
}

func (a *client) emitState() { a.emit("state", a.GetState()) }

func (a *client) setStatus(fn func(s *connStatus)) {
	a.mu.Lock()
	fn(&a.status)
	a.mu.Unlock()
	a.emitState()
}

func (a *client) GetState() appState {
	var s appState
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

func (a *client) GetVersion() string          { return a.version }
func (a *client) GetPlatform() string         { return "android" }
func (a *client) GetTraffic() core.TrafficInfo { return a.meter.Info() }

// ---------- servers & subscriptions ----------

func (a *client) ImportText(text string) (int, error) { return a.lib.ImportText(text) }

// ImportFileData imports a file the user picked — a config, links or a
// picture of a QR code — given as base64; name is the file name without
// extension.
func (a *client) ImportFileData(data, name string) (int, error) {
	b, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return 0, err
	}
	return a.lib.ImportFile(b, name)
}

// WelcomePending reports whether the first-run offer should be shown.
func (a *client) WelcomePending() bool { return a.lib.WelcomePending() }

// WelcomeSeen records that the first-run offer was shown.
func (a *client) WelcomeSeen() error { return a.lib.SetWelcomeSeen() }

func (a *client) CopyLink(id string) error {
	s := a.store.Server(id)
	if s == nil {
		return errors.New("сервер не найден")
	}
	a.host.SetClipboard(s.Link)
	return nil
}

func (a *client) DeleteServer(id string) error { return a.lib.DeleteServer(id) }

// SelectServer makes the server current, reconnecting if already connected.
func (a *client) SelectServer(id string) error {
	if err := a.lib.Select(id); err != nil {
		return err
	}
	return a.reconnectIfActive()
}

func (a *client) SetOpenVPNCredentials(id, user, pass string) error {
	return a.lib.SetOpenVPNCredentials(id, user, pass)
}

func (a *client) AddSubscription(name, rawURL string) (int, error) {
	return a.lib.AddSubscription(name, rawURL)
}
func (a *client) DeleteSubscription(id string) error         { return a.lib.DeleteSubscription(id) }
func (a *client) UpdateSubscription(id string) (int, error)  { return a.lib.UpdateSubscription(id) }
func (a *client) RenameSubscription(id, name string) error   { return a.lib.RenameSubscription(id, name) }
func (a *client) UpdateAllSubscriptions() error              { return a.lib.UpdateAllSubscriptions() }
func (a *client) OpenURL(u string)                           { a.host.OpenURL(u) }
func (a *client) OpenVpnSettings()                           { a.host.OpenVpnSettings() }
func (a *client) GetLogs() string                            { return core.Logs(a.store.Dir()) }
func (a *client) OpenDataDir()                               {}
func (a *client) RestartAsAdmin() error                      { return errors.New("не нужно на Android") }
func (a *client) GetAutostart() map[string]bool              { return map[string]bool{} }
func (a *client) SetAutostart(bool) (map[string]bool, error) { return map[string]bool{}, nil }
func (a *client) TestConnection() (int64, error)             { return core.TestConnection(a.localProxyURL()) }
func (a *client) httpClient(timeout time.Duration) *http.Client {
	return core.HTTPClient(a.localProxyURL(), timeout)
}

// ---------- settings ----------

func (a *client) SaveSettings(s store.Settings) error {
	s.Mode = "tun" // Android has no system proxy for all apps
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
	s.DirectRules, s.ProxyRules = cleanRules(s.DirectRules), cleanRules(s.ProxyRules)
	for _, rules := range [][]string{s.DirectRules, s.ProxyRules} {
		if _, _, err := xray.ParseRules(rules); err != nil {
			return err
		}
	}
	var old store.Settings
	if err := a.store.Update(func(d *store.Data) { old, d.Settings = d.Settings, s }); err != nil {
		return err
	}
	a.emitState()
	if old.SocksPort == s.SocksPort && old.HTTPPort == s.HTTPPort && old.BypassRU == s.BypassRU &&
		old.LogLevel == s.LogLevel && strings.Join(old.DNS, ",") == strings.Join(s.DNS, ",") &&
		strings.Join(old.DirectRules, "|") == strings.Join(s.DirectRules, "|") &&
		strings.Join(old.ProxyRules, "|") == strings.Join(s.ProxyRules, "|") {
		return nil
	}
	return a.reconnectIfActive()
}

func cleanRules(lines []string) []string {
	var out []string
	for _, l := range lines {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

func (a *client) SetMode(string) error { return nil }

// SetAutoBest turns "connect to the fastest server" on or off. Turned on
// while connected, it reconnects to the fastest server right away.
func (a *client) SetAutoBest(on bool) error {
	var was bool
	err := a.store.Update(func(d *store.Data) { was, d.Settings.AutoBest = d.Settings.AutoBest, on })
	a.emitState()
	if err != nil || !on || was {
		return err
	}
	return a.reconnectIfActive()
}

// ---------- connection ----------

func (a *client) Toggle() error {
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

func (a *client) reconnectIfActive() error {
	if a.engine() == nil {
		return nil
	}
	if err := a.Disconnect(); err != nil {
		return err
	}
	return a.Connect()
}

func (a *client) engine() *core.Engine {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.eng
}

func (a *client) Connect() error {
	a.opMu.Lock()
	defer a.opMu.Unlock()
	if a.engine() != nil {
		return nil
	}
	var settings store.Settings
	a.store.Read(func(d *store.Data) { settings = d.Settings })
	settings.Mode = "tun"
	srv := a.lib.Selected()
	if srv == nil {
		return errors.New("выберите сервер")
	}
	a.setStatus(func(s *connStatus) { *s = connStatus{State: "connecting", Mode: "tun", ServerID: srv.ID} })
	if settings.AutoBest {
		if best := a.fastestServer(srv); best != nil && best.ID != srv.ID {
			a.log.Printf("самый быстрый сервер: %s", best.Name)
			_ = a.lib.Select(best.ID)
			srv = best
			a.setStatus(func(s *connStatus) { s.ServerID = srv.ID })
		}
	}

	eng, err := core.StartEngine(a.lib, srv, settings, a.store.Dir(), core.Hooks{
		PrepareTUN: func(serverIP string, opts *xray.Options) error {
			opts.TUN = true
			opts.TunName = "tun0"
			opts.ServerAddress = serverIP
			return nil
		},
		BeforeXray: func(*xray.Options) (func(), error) { return a.openTun(settings.DNS) },
	})
	if err != nil {
		a.log.Printf("подключение не удалось: %v", err)
		a.setStatus(func(s *connStatus) {
			s.State = "disconnected"
			s.Error = err.Error()
		})
		return err
	}
	stop := make(chan struct{})
	a.mu.Lock()
	a.eng = eng
	a.stopStats = stop
	a.mu.Unlock()
	a.meter.StartSession()
	go a.statsLoop(eng.Inst, stop)
	if !settings.NoAutoReconnect {
		go a.watch(eng, stop)
	}
	a.log.Printf("подключено: %s (%s)", srv.Name, srv.Address)
	if settings.Sound {
		a.playSound()
	}
	a.setStatus(func(s *connStatus) { s.State = "connected" })
	return nil
}

// openTun gets the VPN interface from Android and hands it to Xray, which
// reads its descriptor from the environment. The returned release closes
// the interface, which takes the VPN down.
func (a *client) openTun(dnsServers []string) (func(), error) {
	dns := "1.1.1.1"
	for _, d := range dnsServers {
		if net.ParseIP(d) != nil {
			dns = d
			break
		}
	}
	fd, err := a.host.OpenTun(dns)
	if err != nil {
		a.host.TunClosed()
		return nil, err
	}
	_ = os.Setenv("xray.tun.fd", strconv.Itoa(int(fd)))
	return func() {
		// Xray does not close the descriptor it was given.
		closeFD(int(fd))
		a.host.TunClosed()
	}, nil
}

func (a *client) Disconnect() error {
	a.opMu.Lock()
	defer a.opMu.Unlock()
	a.mu.Lock()
	eng, stop := a.eng, a.stopStats
	a.eng = nil
	a.mu.Unlock()
	if eng == nil {
		return nil
	}
	a.setStatus(func(s *connStatus) { s.State = "disconnecting" })
	started := time.Now()
	close(stop)
	err := eng.Close()
	a.log.Printf("отключено за %v", time.Since(started).Round(time.Millisecond))
	a.setStatus(func(s *connStatus) { *s = connStatus{State: "disconnected"} })
	return err
}

func (a *client) statsLoop(inst *xray.Instance, stop chan struct{}) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			a.meter.Flush()
			info := a.meter.Info()
			info.Up, info.Down = 0, 0
			a.emit("traffic", info)
			return
		case <-t.C:
			// Always read: the counters accumulate until reset.
			a.meter.Add(inst.Traffic())
			if a.visible.Load() {
				a.emit("traffic", a.meter.Info())
			}
		}
	}
}

func (a *client) localProxyURL() *url.URL {
	if a.engine() == nil {
		return nil
	}
	var port int
	a.store.Read(func(d *store.Data) { port = d.Settings.HTTPPort })
	return core.LocalProxy(port)
}

func (a *client) playSound() {
	path := filepath.Join(a.store.Dir(), "chime.wav")
	if _, err := os.Stat(path); err != nil {
		if err := os.WriteFile(path, sound.Connected(), 0o644); err != nil {
			return
		}
	}
	a.host.PlaySound(path)
}

// PingAll measures every server, emitting a "ping" event per result. The
// app is outside its own VPN, so this measures the direct path.
func (a *client) PingAll() {
	var servers []*profile.Server
	a.store.Read(func(d *store.Data) { servers = append(servers, d.Servers...) })
	core.PingAll(servers, &net.Dialer{Timeout: 3 * time.Second}, func(id string, ms int64) {
		a.emit("ping", map[string]any{"id": id, "ms": ms})
	})
}

func (a *client) fastestServer(srv *profile.Server, skip ...string) *profile.Server {
	var all []*profile.Server
	a.store.Read(func(d *store.Data) { all = append(all, d.Servers...) })
	group := core.Group(all, srv, skip...)
	if len(group) == 0 {
		return nil
	}
	best := core.Fastest(group, &net.Dialer{Timeout: 3 * time.Second})
	if best == nil {
		return nil
	}
	c := *best
	return &c
}

func (a *client) watch(eng *core.Engine, stop chan struct{}) {
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	core.Watchdog{
		Check:   func() error { return core.CheckThrough(a.localProxyURL()) },
		Online:  func() bool { return core.Online(dialer) },
		Healthy: func() { a.autoFails.Store(0) },
		Failed:  func() { go a.recoverConnection(eng) },
		Logf:    a.log.Printf,
	}.Run(stop)
}

func (a *client) recoverConnection(dead *core.Engine) {
	a.mu.Lock()
	current, id := a.eng == dead, a.status.ServerID
	a.mu.Unlock()
	var off bool
	a.store.Read(func(d *store.Data) { off = d.Settings.NoAutoReconnect })
	if !current || off {
		return
	}
	srv := a.store.Server(id)
	if srv == nil {
		return
	}
	var next *profile.Server
	if a.autoFails.Add(1) >= 2 {
		next = a.fastestServer(srv, srv.ID)
	}
	if next != nil {
		a.notice(fmt.Sprintf("Сервер «%s» не отвечает — переключаюсь на «%s»", srv.Name, next.Name))
		_ = a.lib.Select(next.ID)
	} else {
		a.notice("Соединение через VPN пропало — переподключаюсь")
	}
	_ = a.Disconnect()
	if err := a.Connect(); err != nil {
		a.log.Printf("автопереподключение не удалось: %v", err)
	}
}

func (a *client) notice(text string) {
	a.log.Printf("%s", text)
	a.emit("notice", text)
}
