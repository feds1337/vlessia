package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/feds1337/vlessia/internal/autostart"
	"github.com/feds1337/vlessia/internal/awg"
	"github.com/feds1337/vlessia/internal/hysteria"
	"github.com/feds1337/vlessia/internal/openvpn"
	"github.com/feds1337/vlessia/internal/profile"
	"github.com/feds1337/vlessia/internal/sound"
	"github.com/feds1337/vlessia/internal/store"
	"github.com/feds1337/vlessia/internal/subscription"
	"github.com/feds1337/vlessia/internal/sysproxy"
	"github.com/feds1337/vlessia/internal/tun"
	"github.com/feds1337/vlessia/internal/xray"
)

// bridge is a full-tunnel protocol running in userspace and exposed to
// Xray as a local SOCKS5 server.
type bridge interface {
	Port() int
	Close()
}

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

	mu        sync.Mutex // guards the fields below
	inst      *xray.Instance
	bridge    bridge     // full-tunnel protocol (WireGuard/AmneziaWG/OpenVPN), if any
	tunSess   tunSession // system side of TUN mode, if any
	status    Status
	stopStats chan struct{}
}

func NewApp(st *store.Store, logger *log.Logger) *App {
	return &App{
		store:  st,
		log:    logger,
		status: Status{State: "disconnected", Admin: tun.IsAdmin()},
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.visible.Store(!a.autostarted)
	a.restoreLeftoverProxy()
	a.upgradeServers()
	a.tray = StartTray(a)
	go a.autoUpdateLoop()
	go a.updateLoop()
	a.refreshAutostart()
	var autoConnect bool
	a.store.Read(func(d *store.Data) { autoConnect = d.Settings.AutoConnect })
	if a.connectOnStart || autoConnect {
		go a.autoConnect()
	}
}

// upgradeServers re-parses stored WireGuard/AmneziaWG servers so that
// parameters the parser has learned since (e.g. AmneziaWG 3.x) take effect
// without re-importing.
func (a *App) upgradeServers() {
	_ = a.store.Update(func(d *store.Data) {
		for i, s := range d.Servers {
			if s.WG == nil {
				continue
			}
			if fresh, err := profile.Reparse(s); err == nil {
				d.Servers[i] = fresh
			} else {
				a.log.Printf("не удалось обновить сервер %s: %v", s.Name, err)
			}
		}
	})
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
func (a *App) ImportText(text string) (int, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, errors.New("пусто")
	}
	if (strings.HasPrefix(text, "http://") || strings.HasPrefix(text, "https://")) && !strings.ContainsAny(text, "\n ") &&
		!profile.IsProxyURL(text) {
		return a.AddSubscription("", text)
	}
	return a.importServers(text, "")
}

// importServers parses text and adds the servers. name, if set, names a
// single imported config (used for .conf files).
func (a *App) importServers(text, name string) (int, error) {
	// Outline dynamic keys have to be downloaded first.
	var outline []*profile.Server
	var outlineErrs []error
	if strings.Contains(text, "ssconf://") && !profile.IsOpenVPNConf(text) && !profile.IsWireGuardConf(text) {
		var rest []string
		for _, line := range strings.Split(text, "\n") {
			if l := strings.TrimSpace(line); strings.HasPrefix(strings.ToLower(l), "ssconf://") {
				s, err := a.loadOutlineKey(l)
				if err != nil {
					outlineErrs = append(outlineErrs, err)
				} else {
					outline = append(outline, s)
				}
				continue
			}
			rest = append(rest, line)
		}
		text = strings.Join(rest, "\n")
	}
	servers, errs := profile.ParseMany(text)
	servers = append(outline, servers...)
	errs = append(outlineErrs, errs...)
	if len(servers) == 0 {
		if len(errs) > 0 {
			return 0, errs[0]
		}
		return 0, errors.New("ссылки не найдены")
	}
	if name != "" && len(servers) == 1 && servers[0].IsFullTunnel() {
		servers[0].Name = name
	}
	err := a.store.Update(func(d *store.Data) {
		d.Servers = append(d.Servers, servers...)
		if d.Selected == "" {
			d.Selected = servers[0].ID
		}
	})
	a.emitState()
	return len(servers), err
}

// ImportFile asks for a file (.conf from AmneziaVPN / WireGuard, .ovpn, or
// a text file with links) and imports it.
func (a *App) ImportFile() (int, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Импорт конфигурации",
		Filters: []runtime.FileFilter{
			{DisplayName: "Конфигурации (*.conf, *.ovpn, *.txt, *.vpn)", Pattern: "*.conf;*.ovpn;*.txt;*.vpn"},
			{DisplayName: "Все файлы", Pattern: "*.*"},
		},
	})
	if err != nil || path == "" {
		return 0, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	if len(b) > 4<<20 {
		return 0, errors.New("файл слишком большой")
	}
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	return a.importServers(string(b), name)
}

// loadOutlineKey downloads an Outline dynamic access key (ssconf://).
func (a *App) loadOutlineKey(link string) (*profile.Server, error) {
	keyURL, name, err := profile.OutlineKeyURL(link)
	if err != nil {
		return nil, err
	}
	body, _, err := a.fetch(keyURL)
	if err != nil {
		return nil, fmt.Errorf("Outline: не удалось загрузить ключ: %w", err)
	}
	s, err := profile.ParseOutlineConfig(body, name)
	if err != nil {
		return nil, err
	}
	s.AccessKeyURL, s.Link = link, link
	return s, nil
}

// refreshOutlineKey re-downloads a dynamic key before connecting: the
// provider may have moved the server. On failure the cached copy is used.
func (a *App) refreshOutlineKey(srv *profile.Server) {
	fresh, err := a.loadOutlineKey(srv.AccessKeyURL)
	if err != nil {
		a.log.Printf("%v — использую сохранённые данные", err)
		return
	}
	srv.Address, srv.Port, srv.Cipher, srv.Password, srv.Prefix = fresh.Address, fresh.Port, fresh.Cipher, fresh.Password, fresh.Prefix
	_ = a.store.Update(func(d *store.Data) {
		for _, s := range d.Servers {
			if s.ID == srv.ID {
				s.Address, s.Port, s.Cipher, s.Password, s.Prefix = fresh.Address, fresh.Port, fresh.Cipher, fresh.Password, fresh.Prefix
			}
		}
	})
}

// SetOpenVPNCredentials stores the login for an OpenVPN profile that uses
// auth-user-pass.
func (a *App) SetOpenVPNCredentials(id, user, pass string) error {
	err := a.store.Update(func(d *store.Data) {
		for _, s := range d.Servers {
			if s.ID == id && s.OVPN != nil {
				s.OVPN.Username, s.OVPN.Password = strings.TrimSpace(user), pass
			}
		}
	})
	a.emitState()
	return err
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

func (a *App) DeleteServer(id string) error {
	err := a.store.Update(func(d *store.Data) {
		out := d.Servers[:0]
		for _, s := range d.Servers {
			if s.ID != id {
				out = append(out, s)
			}
		}
		d.Servers = out
		if d.Selected == id {
			d.Selected = ""
		}
	})
	a.emitState()
	return err
}

// SelectServer makes the server current, reconnecting if already connected.
func (a *App) SelectServer(id string) error {
	if err := a.store.Update(func(d *store.Data) { d.Selected = id }); err != nil {
		return err
	}
	a.emitState()
	return a.reconnectIfActive()
}

func (a *App) AddSubscription(name, rawURL string) (int, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return 0, errors.New("неверный URL подписки")
	}
	sub := &store.Subscription{ID: profile.NewID(), Name: name, URL: u.String()}
	if err := a.store.Update(func(d *store.Data) { d.Subscriptions = append(d.Subscriptions, sub) }); err != nil {
		return 0, err
	}
	return a.UpdateSubscription(sub.ID)
}

func (a *App) DeleteSubscription(id string) error {
	err := a.store.Update(func(d *store.Data) {
		subs := d.Subscriptions[:0]
		for _, s := range d.Subscriptions {
			if s.ID != id {
				subs = append(subs, s)
			}
		}
		d.Subscriptions = subs
		servers := d.Servers[:0]
		for _, s := range d.Servers {
			if s.SubID != id {
				servers = append(servers, s)
			} else if d.Selected == s.ID {
				d.Selected = ""
			}
		}
		d.Servers = servers
	})
	a.emitState()
	return err
}

// UpdateSubscription downloads the subscription, refreshes its metadata and
// replaces its servers.
func (a *App) UpdateSubscription(id string) (int, error) {
	var sub store.Subscription
	a.store.Read(func(d *store.Data) {
		for _, s := range d.Subscriptions {
			if s.ID == id {
				sub = *s
			}
		}
	})
	if sub.ID == "" {
		return 0, errors.New("подписка не найдена")
	}

	n, err := a.updateSubscription(sub)
	if err != nil {
		a.log.Printf("подписка %s: %v", sub.URL, err)
	}
	_ = a.store.Update(func(d *store.Data) {
		for _, s := range d.Subscriptions {
			if s.ID == id {
				s.LastError = ""
				if err != nil {
					s.LastError = err.Error()
				}
			}
		}
	})
	a.emitState()
	return n, err
}

func (a *App) updateSubscription(sub store.Subscription) (int, error) {
	body, header, err := a.fetch(sub.URL)
	if err != nil {
		return 0, fmt.Errorf("загрузка подписки: %w", err)
	}
	text := profile.Decode(body)
	info := subscription.Parse(header, text)
	servers, errs := profile.ParseMany(text)
	for _, s := range servers {
		s.SubID = sub.ID
	}

	err = a.store.Update(func(d *store.Data) {
		for _, s := range d.Subscriptions {
			if s.ID != sub.ID {
				continue
			}
			s.Title = info.Title
			s.Announce = info.Announce
			s.SupportURL = info.SupportURL
			s.WebPageURL = info.WebPageURL
			s.UpdateInterval = info.UpdateInterval
			s.HasUserInfo = info.HasUserInfo
			s.Upload, s.Download, s.Total, s.Expire = info.Upload, info.Download, info.Total, info.Expire
			if s.Title == "" {
				if u, err := url.Parse(s.URL); err == nil {
					s.Title = u.Host
				}
			}
			// Metadata counts as a successful update even without servers,
			// so expired subscriptions are not re-fetched on every tick.
			s.Updated = time.Now().Unix()
		}
		if len(servers) > 0 {
			replaceSubscriptionServers(d, sub.ID, servers)
		}
	})
	if err != nil {
		return 0, err
	}
	if len(servers) == 0 {
		if len(errs) > 0 {
			return 0, fmt.Errorf("в подписке нет рабочих ссылок: %v", errs[0])
		}
		return 0, errors.New("подписка не вернула ни одного сервера")
	}
	return len(servers), nil
}

func replaceSubscriptionServers(d *store.Data, subID string, servers []*profile.Server) {
	// Keep the selection if the same server (by link) is still present.
	var selectedLink string
	for _, s := range d.Servers {
		if s.ID == d.Selected && s.SubID == subID {
			selectedLink = s.Link
		}
	}
	// Replace in place to keep the subscription's position in the list.
	var out []*profile.Server
	inserted := false
	for _, s := range d.Servers {
		if s.SubID == subID {
			if !inserted {
				out = append(out, servers...)
				inserted = true
			}
			continue
		}
		out = append(out, s)
	}
	if !inserted {
		out = append(out, servers...)
	}
	d.Servers = out
	if selectedLink != "" {
		d.Selected = ""
		for _, s := range servers {
			if s.Link == selectedLink {
				d.Selected = s.ID
			}
		}
	}
	if d.Selected == "" {
		d.Selected = servers[0].ID
	}
}

// RenameSubscription sets a custom name; an empty name restores the title
// sent by the server.
func (a *App) RenameSubscription(id, name string) error {
	err := a.store.Update(func(d *store.Data) {
		for _, s := range d.Subscriptions {
			if s.ID == id {
				s.Name = strings.TrimSpace(name)
			}
		}
	})
	a.emitState()
	return err
}

// OpenURL opens a support / web page link in the default browser.
func (a *App) OpenURL(u string) {
	runtime.BrowserOpenURL(a.ctx, u)
}

// autoUpdateLoop refreshes subscriptions according to the interval the
// server requested (profile-update-interval), or every 12 hours.
func (a *App) autoUpdateLoop() {
	time.Sleep(20 * time.Second)
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for {
		var due []string
		now := time.Now().Unix()
		a.store.Read(func(d *store.Data) {
			for _, s := range d.Subscriptions {
				hours := s.UpdateInterval
				if hours <= 0 {
					hours = 12
				}
				if now-s.Updated >= int64(hours)*3600 {
					due = append(due, s.ID)
				}
			}
		})
		for _, id := range due {
			_, _ = a.UpdateSubscription(id)
		}
		<-t.C
	}
}

func (a *App) UpdateAllSubscriptions() error {
	var ids []string
	a.store.Read(func(d *store.Data) {
		for _, s := range d.Subscriptions {
			ids = append(ids, s.ID)
		}
	})
	var errs []error
	for _, id := range ids {
		if _, err := a.UpdateSubscription(id); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// fetch downloads a URL, going through the local proxy while connected so
// that blocked subscription hosts remain reachable.
func (a *App) fetch(rawURL string) (string, http.Header, error) {
	client := a.httpClient(30 * time.Second)
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return "", nil, err
	}
	// Most panels return the base64 link list for v2rayN-like clients.
	req.Header.Set("User-Agent", "v2rayN/7.0")
	resp, err := client.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	return string(b), resp.Header, err
}

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

	var (
		srv      *profile.Server
		settings store.Settings
	)
	a.store.Read(func(d *store.Data) {
		settings = d.Settings
		for _, s := range d.Servers {
			if s.ID == d.Selected {
				c := *s
				srv = &c
			}
		}
	})
	if srv == nil {
		return errors.New("выберите сервер")
	}
	settings.Mode = effectiveMode(srv, settings.Mode)

	a.setStatus(func(s *Status) {
		*s = Status{State: "connecting", Mode: settings.Mode, ServerID: srv.ID, Admin: tun.IsAdmin()}
	})

	inst, bridge, sess, iface, err := a.start(srv, settings)
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
	a.inst = inst
	a.bridge = bridge
	a.tunSess = sess
	a.stopStats = stop
	a.mu.Unlock()
	if sess != nil && sess.Done() != nil {
		go a.watchTUN(sess)
	}
	go a.statsLoop(inst, stop)
	a.log.Printf("подключено: %s (%s), режим %s", srv.Name, srv.Address, settings.Mode)
	if settings.Sound {
		sound.PlayConnected()
	}
	a.setStatus(func(s *Status) {
		s.State = "connected"
		s.Interface = iface
	})
	return nil
}

// effectiveMode returns the mode a server actually runs in. WireGuard,
// AmneziaWG and OpenVPN are full tunnels by nature, so they always use TUN.
func effectiveMode(srv *profile.Server, mode string) string {
	if srv.IsFullTunnel() {
		return "tun"
	}
	return mode
}

func (a *App) instance() *xray.Instance {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.inst
}

// start launches everything needed for srv: the WireGuard/AmneziaWG bridge
// if any, Xray, and then TUN or system proxy.
func (a *App) start(srv *profile.Server, settings store.Settings) (*xray.Instance, bridge, tunSession, string, error) {
	opts := xray.Options{
		SocksPort: settings.SocksPort,
		HTTPPort:  settings.HTTPPort,
		BypassRU:  settings.BypassRU,
		DNS:       settings.DNS,
		LogFile:   filepath.Join(a.store.Dir(), "xray.log"),
		LogLevel:  settings.LogLevel,
	}

	// Ask for the OpenVPN login before touching the network.
	if srv.OVPN != nil && srv.OVPN.NeedAuth && srv.OVPN.Username == "" {
		return nil, nil, nil, "", openvpn.ErrNeedAuth
	}
	if srv.AccessKeyURL != "" {
		a.refreshOutlineKey(srv)
	}
	if srv.Prefix != "" {
		a.log.Printf("Outline: маскировка prefix не поддерживается Xray, подключение без неё")
	}

	// Resolve the server now: once TUN is up, system DNS would go into the
	// tunnel that needs this very address to work. WireGuard and the
	// Hysteria probe need an IP anyway.
	var serverIP string
	if settings.Mode == "tun" || srv.IsFullTunnel() || srv.Protocol == "hysteria2" {
		ip, err := resolve(srv.Address)
		if err != nil {
			return nil, nil, nil, "", fmt.Errorf("не удалось найти IP сервера %s: %w", srv.Address, err)
		}
		serverIP = ip
	}

	if settings.Mode == "tun" {
		if err := a.prepareTUN(srv, serverIP, &opts); err != nil {
			return nil, nil, nil, "", err
		}
	}

	if srv.Protocol == "hysteria2" && srv.Insecure && srv.PinnedSHA == "" {
		// Runs before TUN is up, so no adapter binding is needed.
		res, err := hysteria.Probe(context.Background(), srv, serverIP, nil)
		if err != nil {
			return nil, nil, nil, "", fmt.Errorf("Hysteria: сервер не отвечает: %w", err)
		}
		srv.PinnedSHA = res.CertSHA256
		a.log.Printf("Hysteria: ссылка с insecure=1, закреплён текущий сертификат сервера %s", res.CertSHA256)
	}

	var br bridge
	if srv.OVPN != nil {
		var control func(string, string, syscall.RawConn) error
		if opts.Interface != "" {
			control = tun.BindToInterface(opts.Interface)
		}
		t, err := openvpn.Start(srv.OVPN, serverIP, control, a.log.Printf)
		if err != nil {
			return nil, nil, nil, "", err
		}
		br = t
		opts.BridgePort = t.Port()
		a.log.Printf("openvpn: туннель к %s:%d, локальный мост 127.0.0.1:%d", serverIP, srv.Port, t.Port())
	}
	if srv.WG != nil {
		awg.Verbose = settings.LogLevel == "debug"
		endpoint, err := netip.ParseAddrPort(net.JoinHostPort(serverIP, strconv.Itoa(srv.Port)))
		if err != nil {
			return nil, nil, nil, "", err
		}
		bindIdx := 0
		if opts.Interface != "" {
			if inf, err := net.InterfaceByName(opts.Interface); err == nil {
				bindIdx = inf.Index
			}
		}
		t, err := awg.Start(srv.WG, endpoint, bindIdx, a.log.Printf)
		if err != nil {
			return nil, nil, nil, "", fmt.Errorf("AmneziaWG: %w", err)
		}
		br = t
		opts.BridgePort = t.Port()
		a.log.Printf("%s: туннель к %s, локальный мост 127.0.0.1:%d", srv.Protocol, endpoint, t.Port())
	}
	fail := func(inst *xray.Instance, err error) (*xray.Instance, bridge, tunSession, string, error) {
		if inst != nil {
			_ = inst.Close()
		}
		if br != nil {
			br.Close()
		}
		return nil, nil, nil, "", err
	}

	cfg, err := xray.BuildConfig(srv, opts)
	if err != nil {
		return fail(nil, err)
	}
	// Xray appends to its log; keep only the current session.
	_ = os.Truncate(opts.LogFile, 0)
	_ = os.WriteFile(filepath.Join(a.store.Dir(), "xray-config.json"), cfg, 0o600)

	inst, err := xray.Start(cfg)
	if err != nil {
		return fail(nil, err)
	}

	if settings.Mode == "tun" {
		sess, err := a.activateTUN(serverIP, settings)
		if err != nil {
			return fail(inst, err)
		}
		return inst, br, sess, opts.Interface, nil
	}

	if err := a.enableSystemProxy(settings.HTTPPort); err != nil {
		return fail(inst, fmt.Errorf("не удалось включить системный прокси: %w", err))
	}
	return inst, br, nil, "", nil
}

func (a *App) Disconnect() error {
	a.opMu.Lock()
	defer a.opMu.Unlock()
	a.mu.Lock()
	inst, bridge, sess, stop, mode := a.inst, a.bridge, a.tunSess, a.stopStats, a.status.Mode
	a.inst, a.bridge, a.tunSess = nil, nil, nil
	a.mu.Unlock()
	if inst == nil {
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
	step("остановка xray", func() { err = inst.Close() })
	if bridge != nil {
		step("остановка AmneziaWG", bridge.Close)
	}
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
	return &url.URL{Scheme: "http", Host: net.JoinHostPort("127.0.0.1", strconv.Itoa(port))}
}

func resolve(host string) (string, error) {
	if ip := net.ParseIP(host); ip != nil {
		return host, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return "", err
	}
	for _, ip := range ips {
		if ip.To4() != nil {
			return ip.String(), nil
		}
	}
	return ips[0].String(), nil
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

	sem := make(chan struct{}, 16)
	var wg sync.WaitGroup
	for _, s := range servers {
		wg.Add(1)
		go func(s *profile.Server) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			runtime.EventsEmit(a.ctx, "ping", map[string]any{"id": s.ID, "ms": pingServer(s, dialer)})
		}(s)
	}
	wg.Wait()
}

// pingServer returns the connect time in ms, -1 if unreachable and -2 if
// the protocol cannot be pinged: WireGuard deliberately ignores packets it
// cannot authenticate.
func pingServer(s *profile.Server, dialer *net.Dialer) int64 {
	switch {
	case s.WG != nil, s.OVPN != nil && s.Network == "udp":
		return -2
	case s.Protocol == "hysteria2":
		ip, err := resolve(s.Address)
		if err != nil {
			return -1
		}
		res, err := hysteria.Probe(context.Background(), s, ip, dialer.Control)
		if err != nil {
			return -1
		}
		return res.RTT.Milliseconds()
	}
	start := time.Now()
	c, err := dialer.Dial("tcp", net.JoinHostPort(s.Address, strconv.Itoa(s.Port)))
	if err != nil {
		return -1
	}
	_ = c.Close()
	return time.Since(start).Milliseconds()
}

// TestConnection measures an HTTPS request through the active tunnel.
func (a *App) TestConnection() (int64, error) {
	p := a.localProxyURL()
	if p == nil {
		return 0, errors.New("не подключено")
	}
	tr := &http.Transport{Proxy: http.ProxyURL(p), DisableKeepAlives: true}
	client := &http.Client{Transport: tr, Timeout: 10 * time.Second}
	start := time.Now()
	resp, err := client.Get("https://www.gstatic.com/generate_204")
	if err != nil {
		return 0, err
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return time.Since(start).Milliseconds(), nil
}

// ---------- logs ----------

func (a *App) GetLogs() string {
	var b strings.Builder
	for _, name := range []string{"app.log", "xray.log"} {
		fmt.Fprintf(&b, "===== %s =====\n", name)
		b.WriteString(tail(filepath.Join(a.store.Dir(), name), 200))
		b.WriteString("\n")
	}
	return b.String()
}

func (a *App) OpenDataDir() {
	_ = openFolder(a.store.Dir())
}

// GetPlatform returns the operating system ("windows", "darwin", "linux"),
// for platform-specific wording in the UI.
func (a *App) GetPlatform() string { return goruntime.GOOS }

func tail(path string, n int) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return "(пусто)\n"
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
