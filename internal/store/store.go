// Package store persists servers, subscriptions and settings as JSON in the
// user's application data directory.
package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/feds1337/vlessia/internal/profile"
	"github.com/feds1337/vlessia/internal/sysproxy"
)

type Subscription struct {
	ID      string `json:"id"`
	Name    string `json:"name"` // set by the user; overrides Title
	URL     string `json:"url"`
	Updated int64  `json:"updated"` // unix seconds, last successful update

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
}

type Data struct {
	Servers       []*profile.Server `json:"servers"`
	Subscriptions []*Subscription   `json:"subscriptions"`
	Selected      string            `json:"selected"`
	Settings      Settings          `json:"settings"`

	// SavedProxy holds the user's proxy settings while ours are active, so
	// they can be restored even after a crash.
	SavedProxy *sysproxy.State `json:"savedProxy,omitempty"`

	// WelcomeSeen: the first-run offer (a free VPN from the Telegram bot)
	// has been shown.
	WelcomeSeen bool `json:"welcomeSeen,omitempty"`
}

type Store struct {
	mu   sync.Mutex
	dir  string
	data Data
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
	if err == nil {
		if err := json.Unmarshal(b, &s.data); err != nil {
			return nil, err
		}
	}
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
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.data)
	b, err := json.MarshalIndent(&s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.file() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.file())
}

// Server returns a copy of the server with the given id.
func (s *Store) Server(id string) *profile.Server {
	var out *profile.Server
	s.Read(func(d *Data) {
		for _, srv := range d.Servers {
			if srv.ID == id {
				c := *srv
				out = &c
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
