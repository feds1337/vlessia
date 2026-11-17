// Package store persists servers, subscriptions and settings as JSON in the
// user's application data directory.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/feds1337/vlessia/internal/profile"
	"github.com/feds1337/vlessia/internal/sysproxy"
)

type Subscription struct {
	ID          string `json:"id"`
	Name        string `json:"name"` // set by the user; overrides Title
	URL         string `json:"url"`
	Updated     int64  `json:"updated"` // unix seconds, last successful update
	LastAttempt int64  `json:"lastAttempt,omitempty"`

	// Metadata reported by the subscription server.
	Title          string `json:"title"`
	Announce       string `json:"announce"`
	SupportURL     string `json:"supportUrl"`
	WebPageURL     string `json:"webPageUrl"`
	UpdateInterval int    `json:"updateInterval"` // hours
	HasUserInfo    bool   `json:"hasUserInfo"`
	Upload         int64  `json:"upload"`
	Download       int64  `json:"download"`
	Total          int64  `json:"total"`  // bytes, 0 = unlimited
	Expire         int64  `json:"expire"` // unix seconds, 0 = never

	LastError string `json:"lastError"`

	// Notified: which alerts have already been sent for this subscription
	// (a bitmask of core.Remind* bits), so a warning is shown once and
	// again only after its condition clears.
	Notified int `json:"notified,omitempty"`
}

type Settings struct {
	Mode      string   `json:"mode"` // "proxy" | "tun"
	SocksPort int      `json:"socksPort"`
	HTTPPort  int      `json:"httpPort"`
	BypassRU  bool     `json:"bypassRu"`
	DNS       []string `json:"dns"`
	LogLevel  string   `json:"logLevel"`
	// Sound plays the connection sound. Off by default; it replaced the old
	// "soundOff" key, so existing configs start with the sound off too.
	Sound bool `json:"sound"`
	// NoUpdateCheck disables the automatic check for new releases (stored
	// inverted so that checking is on by default).
	NoUpdateCheck bool `json:"noUpdateCheck"`
	// AutoConnect connects to the selected server when the client starts.
	AutoConnect bool `json:"autoConnect"`

	// NoAnimations turns interface animations off (stored inverted so that
	// they are on by default).
	NoAnimations bool `json:"noAnimations"`
	// Theme is the colour scheme: "" follows the system, "light" or "dark".
	Theme string `json:"theme"`
	// Accent is the accent colour of the interface; "" is the default
	// lagoon (see frontend/dist/style.css for the others).
	Accent string `json:"accent"`
	// NoAutoReconnect disables the connection watchdog, which reconnects
	// (and then switches servers) when the VPN stops passing traffic.
	NoAutoReconnect bool `json:"noAutoReconnect"`
	// AutoBest picks the fastest server of the selected server's
	// subscription before connecting.
	AutoBest bool `json:"autoBest"`
	// DirectRules and ProxyRules are the user's own routing rules, one per
	// entry: a domain ("example.com" covers its subdomains), an Xray domain
	// rule ("full:", "keyword:", "regexp:", "domain:"), an IP or a CIDR.
	// ProxyRules win over DirectRules and over BypassRU.
	DirectRules []string `json:"directRules"`
	ProxyRules  []string `json:"proxyRules"`

	// KillSwitch keeps the TUN adapter up while the VPN is disconnected and
	// sends all traffic into a black hole, so nothing leaks. It only works
	// where an adapter exists: TUN mode on desktop, always on Android.
	KillSwitch bool `json:"killSwitch"`

	// Balance spreads the new connections across all the servers of the
	// selected server's subscription instead of using a single one.
	// Full-tunnel protocols cannot be balanced.
	Balance bool `json:"balance"`
	// BalanceStrategy is "roundrobin" ("" means the same: spread evenly) or
	// "leastping": always the healthy member with the lowest delay.
	// Dead members are dropped before connecting in both cases.
	BalanceStrategy string `json:"balanceStrategy"`

	// ProxyApps and DirectApps route whole applications through the VPN or
	// around it, by process name ("chrome.exe"), absolute path or folder
	// ("C:/Games/"). Xray resolves the owner of a connection through the
	// system's TCP table: it works on Windows, Linux, macOS and Android.
	// ProxyApps win over DirectApps and over the site rules.
	ProxyApps  []string `json:"proxyApps"`
	DirectApps []string `json:"directApps"`

	// AndroidApps and AndroidAppsMode split the tunnel per application on
	// Android: mode "allow" sends only AndroidApps through the VPN, "block"
	// sends everything except them, "" (or "off") sends everything. The
	// client itself is always kept out of the tunnel.
	AndroidApps     []string `json:"androidApps"`
	AndroidAppsMode string   `json:"androidAppsMode"`
}

// Traffic counts the bytes passed through the VPN on one day.
type Traffic struct {
	Day  string `json:"day"` // local date, YYYY-MM-DD
	Up   int64  `json:"up"`
	Down int64  `json:"down"`
}

type Data struct {
	Servers       []*profile.Server `json:"servers"`
	Subscriptions []*Subscription   `json:"subscriptions"`
	Selected      string            `json:"selected"`
	Settings      Settings          `json:"settings"`

	// SavedProxy holds the user's proxy settings while ours are active, so
	// they can be restored even after a crash.
	SavedProxy *sysproxy.State `json:"savedProxy,omitempty"`

	// Today is the traffic of the current day.
	Today        Traffic      `json:"today"`
	DailyTraffic []Traffic    `json:"dailyTraffic,omitempty"`
	Connections  []Connection `json:"connections,omitempty"`

	// WelcomeSeen: the first-run offer (a free VPN from the Telegram bot)
	// has been shown.
	WelcomeSeen bool `json:"welcomeSeen,omitempty"`
}

type Store struct {
	mu        sync.Mutex
	dir       string
	data      Data
	recovered bool
}

func DefaultSettings() Settings {
	return Settings{
		Mode:      "proxy",
		SocksPort: 10808,
		HTTPPort:  10809,
		DNS:       []string{"1.1.1.1", "8.8.8.8"},
		LogLevel:  "warning",
	}
}

// Open loads the store from %APPDATA%\Vlessia (or $VLESSIA_DATA),
// creating it if needed.
func Open() (*Store, error) {
	dir := os.Getenv("VLESSIA_DATA")
	if dir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return nil, err
		}
		dir = filepath.Join(base, "Vlessia")
		migrateLegacyDir(filepath.Join(base, "VPNClient"), dir)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, data: Data{Settings: DefaultSettings()}}
	b, err := os.ReadFile(s.file())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	var invalid error
	if err == nil {
		invalid = decodeData(b, &s.data)
	}
	if invalid != nil || errors.Is(err, os.ErrNotExist) {
		saved, backupErr := os.ReadFile(s.file() + ".bak")
		if backupErr == nil {
			recovered := Data{Settings: DefaultSettings()}
			if backupErr = decodeData(saved, &recovered); backupErr == nil {
				if invalid != nil {
					damaged := s.file() + ".corrupt-" + time.Now().UTC().Format("20060102T150405.000000000")
					if err := atomicWrite(damaged, b); err != nil {
						return nil, err
					}
				}
				if err := atomicWrite(s.file(), saved); err != nil {
					return nil, err
				}
				s.data, s.recovered = recovered, true
			}
		}
		if invalid != nil && !s.recovered {
			return nil, fmt.Errorf("config: %w; backup: %v", invalid, backupErr)
		}
		if invalid == nil && backupErr != nil && !errors.Is(backupErr, os.ErrNotExist) {
			return nil, backupErr
		}
	}
	s.data.Servers = slices.DeleteFunc(s.data.Servers, func(p *profile.Server) bool { return p == nil })
	s.data.Subscriptions = slices.DeleteFunc(s.data.Subscriptions, func(p *Subscription) bool { return p == nil })
	d := DefaultSettings()
	st := &s.data.Settings
	if st.Mode == "" {
		st.Mode = d.Mode
	}
	if st.SocksPort == 0 {
		st.SocksPort = d.SocksPort
	}
	if st.HTTPPort == 0 {
		st.HTTPPort = d.HTTPPort
	}
	if len(st.DNS) == 0 {
		st.DNS = d.DNS
	}
	if st.LogLevel == "" {
		st.LogLevel = d.LogLevel
	}
	return s, nil
}

func (s *Store) Dir() string  { return s.dir }
func (s *Store) file() string { return filepath.Join(s.dir, "config.json") }

// Read calls fn with the data under lock.
func (s *Store) Read(fn func(d *Data)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.data)
}

// Update calls fn with the data under lock and saves it to disk.
func (s *Store) Update(fn func(d *Data)) error {
	return s.Edit(func(d *Data) error { fn(d); return nil })
}

// Edit commits a change in memory only after it was saved successfully.
func (s *Store) Edit(fn func(d *Data) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.data.clone()
	if err := fn(&next); err != nil {
		return err
	}
	b, err := json.MarshalIndent(&next, "", "  ")
	if err != nil {
		return err
	}
	// Keep the previous valid configuration before replacing the current one.
	previous, readErr := os.ReadFile(s.file())
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	if readErr == nil {
		var check Data
		if err := decodeData(previous, &check); err != nil {
			return err
		}
		if err := atomicWrite(s.file()+".bak", previous); err != nil {
			return err
		}
	} else {
		if err := atomicWrite(s.file()+".bak", b); err != nil {
			return err
		}
	}
	if err := atomicWrite(s.file(), b); err != nil {
		return err
	}
	s.data = next
	return nil
}

func (d Data) clone() Data {
	d.Servers = slices.Clone(d.Servers)
	for i, s := range d.Servers {
		d.Servers[i] = s.Clone()
	}
	d.Subscriptions = slices.Clone(d.Subscriptions)
	for i, sub := range d.Subscriptions {
		c := *sub
		d.Subscriptions[i] = &c
	}
	d.Settings.DNS = slices.Clone(d.Settings.DNS)
	d.Settings.ProxyRules = slices.Clone(d.Settings.ProxyRules)
	d.Settings.DirectRules = slices.Clone(d.Settings.DirectRules)
	d.Settings.ProxyApps = slices.Clone(d.Settings.ProxyApps)
	d.Settings.DirectApps = slices.Clone(d.Settings.DirectApps)
	d.Settings.AndroidApps = slices.Clone(d.Settings.AndroidApps)
	d.DailyTraffic = slices.Clone(d.DailyTraffic)
	d.Connections = slices.Clone(d.Connections)
	return d
}

type Connection struct {
	Started int64  `json:"started"`
	Server  string `json:"server"`
	Mode    string `json:"mode"`
}

// Server returns a copy of the server with the given id.
func (s *Store) Server(id string) *profile.Server {
	var out *profile.Server
	s.Read(func(d *Data) {
		for _, srv := range d.Servers {
			if srv.ID == id {
				out = srv.Clone()
			}
		}
	})
	return out
}

// migrateLegacyDir moves data from the pre-rename location (the client was
// called "VPN Client"). It keys on config.json, not on the folder: the new
// folder may already exist (e.g. holding only a log). If the folder cannot
// be moved (old version still running), the config is copied.
func migrateLegacyDir(old, dir string) {
	if _, err := os.Stat(filepath.Join(dir, "config.json")); err == nil {
		return
	}
	b, err := os.ReadFile(filepath.Join(old, "config.json"))
	if err != nil {
		return
	}
	if _, err := os.Stat(dir); err != nil && os.Rename(old, dir) == nil {
		return
	}
	if os.MkdirAll(dir, 0o755) == nil {
		_ = os.WriteFile(filepath.Join(dir, "config.json"), b, 0o600)
	}
}

// Recovered reports automatic restoration at startup. It is immutable after Open.
func (s *Store) Recovered() bool { return s.recovered }

func decodeData(b []byte, d *Data) error {
	if len(b) == 0 || string(b) == "null" {
		return errors.New("empty configuration")
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(b, &object); err != nil {
		return err
	}
	if object == nil {
		return errors.New("empty configuration")
	}
	return json.Unmarshal(b, d)
}

// atomicWrite never exposes a partially written configuration or backup.
func atomicWrite(path string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), "config-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
