// Package core holds the client logic apart from the window and the
// operating system: servers and subscriptions, starting a connection, pings.
package core

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/feds1337/vlessia/internal/profile"
	"github.com/feds1337/vlessia/internal/qr"
	"github.com/feds1337/vlessia/internal/store"
	"github.com/feds1337/vlessia/internal/subscription"
)

// Library manages the servers and subscriptions kept in the store.
type Library struct {
	Store *store.Store
	Log   *log.Logger
	// HTTP returns the client for downloads; it should go through the VPN
	// while connected, so that blocked subscription hosts stay reachable.
	HTTP func(timeout time.Duration) *http.Client
	// Changed is called after the stored data changed.
	Changed func()
}

func (l *Library) changed() {
	if l.Changed != nil {
		l.Changed()
	}
}

// ImportText adds servers from share links, or a subscription if the text
// is an http(s) URL. Returns how many servers were added.
func (l *Library) ImportText(text string) (int, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, errors.New("пусто")
	}
	if (strings.HasPrefix(text, "http://") || strings.HasPrefix(text, "https://")) && !strings.ContainsAny(text, "\n ") &&
		!profile.IsProxyURL(text) {
		return l.AddSubscription("", text)
	}
	return l.Import(text, "")
}

// Import parses text and adds the servers. name, if set, names a single
// imported config (used for .conf files).
func (l *Library) Import(text, name string) (int, error) {
	// Outline dynamic keys have to be downloaded first.
	var outline []*profile.Server
	var outlineErrs []error
	if strings.Contains(text, "ssconf://") && !profile.IsOpenVPNConf(text) && !profile.IsWireGuardConf(text) {
		var rest []string
		for _, line := range strings.Split(text, "\n") {
			if t := strings.TrimSpace(line); strings.HasPrefix(strings.ToLower(t), "ssconf://") {
				s, err := l.loadOutlineKey(t)
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
	err := l.Store.Update(func(d *store.Data) {
		d.Servers = append(d.Servers, servers...)
		if d.Selected == "" {
			d.Selected = servers[0].ID
		}
	})
	l.changed()
	return len(servers), err
}

// ImportFile imports a file the user picked: a config or a list of links,
// or a picture of a QR code. name is the file name without extension.
func (l *Library) ImportFile(data []byte, name string) (int, error) {
	if len(data) > 4<<20 {
		return 0, errors.New("файл слишком большой")
	}
	if qr.IsImage(data) {
		text, err := qr.DecodeBytes(data)
		if err != nil {
			return 0, err
		}
		return l.ImportText(text)
	}
	return l.Import(string(data), name)
}

// WelcomePending reports whether the first-run offer has yet to be shown.
func (l *Library) WelcomePending() bool {
	var seen bool
	l.Store.Read(func(d *store.Data) { seen = d.WelcomeSeen })
	return !seen
}

// SetWelcomeSeen records that the first-run offer was shown.
func (l *Library) SetWelcomeSeen() error {
	return l.Store.Update(func(d *store.Data) { d.WelcomeSeen = true })
}

// loadOutlineKey downloads an Outline dynamic access key (ssconf://).
func (l *Library) loadOutlineKey(link string) (*profile.Server, error) {
	keyURL, name, err := profile.OutlineKeyURL(link)
	if err != nil {
		return nil, err
	}
	body, _, err := l.Fetch(keyURL)
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

// RefreshOutlineKey re-downloads a dynamic key before connecting: the
// provider may have moved the server. On failure the cached copy is used.
func (l *Library) RefreshOutlineKey(srv *profile.Server) {
	fresh, err := l.loadOutlineKey(srv.AccessKeyURL)
	if err != nil {
		l.Log.Printf("%v — использую сохранённые данные", err)
		return
	}
	srv.Address, srv.Port, srv.Cipher, srv.Password, srv.Prefix = fresh.Address, fresh.Port, fresh.Cipher, fresh.Password, fresh.Prefix
	_ = l.Store.Update(func(d *store.Data) {
		for _, s := range d.Servers {
			if s.ID == srv.ID {
				s.Address, s.Port, s.Cipher, s.Password, s.Prefix = fresh.Address, fresh.Port, fresh.Cipher, fresh.Password, fresh.Prefix
			}
		}
	})
}

// UpgradeServers re-parses stored WireGuard/AmneziaWG servers so that
// parameters the parser has learned since (e.g. AmneziaWG 3.x) take effect
// without re-importing.
func (l *Library) UpgradeServers() {
	_ = l.Store.Update(func(d *store.Data) {
		for i, s := range d.Servers {
			if s.WG == nil {
				continue
			}
			if fresh, err := profile.Reparse(s); err == nil {
				d.Servers[i] = fresh
			} else {
				l.Log.Printf("не удалось обновить сервер %s: %v", s.Name, err)
			}
		}
	})
}

// SetOpenVPNCredentials stores the login for an OpenVPN profile that uses
// auth-user-pass.
func (l *Library) SetOpenVPNCredentials(id, user, pass string) error {
	err := l.Store.Update(func(d *store.Data) {
		for _, s := range d.Servers {
			if s.ID == id && s.OVPN != nil {
				s.OVPN.Username, s.OVPN.Password = strings.TrimSpace(user), pass
			}
		}
	})
	l.changed()
	return err
}

func (l *Library) DeleteServer(id string) error {
	err := l.Store.Update(func(d *store.Data) {
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
	l.changed()
	return err
}

// Select makes the server current.
func (l *Library) Select(id string) error {
	err := l.Store.Update(func(d *store.Data) { d.Selected = id })
	l.changed()
	return err
}

// Selected returns a copy of the current server, or nil.
func (l *Library) Selected() *profile.Server {
	var srv *profile.Server
	l.Store.Read(func(d *store.Data) {
		for _, s := range d.Servers {
			if s.ID == d.Selected {
				c := *s
				srv = &c
			}
		}
	})
	return srv
}

func (l *Library) AddSubscription(name, rawURL string) (int, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return 0, errors.New("неверный URL подписки")
	}
	sub := &store.Subscription{ID: profile.NewID(), Name: name, URL: u.String()}
	if err := l.Store.Update(func(d *store.Data) { d.Subscriptions = append(d.Subscriptions, sub) }); err != nil {
		return 0, err
	}
	return l.UpdateSubscription(sub.ID)
}

func (l *Library) DeleteSubscription(id string) error {
	err := l.Store.Update(func(d *store.Data) {
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
	l.changed()
	return err
}

// UpdateSubscription downloads the subscription, refreshes its metadata and
// replaces its servers.
func (l *Library) UpdateSubscription(id string) (int, error) {
	var sub store.Subscription
	l.Store.Read(func(d *store.Data) {
		for _, s := range d.Subscriptions {
			if s.ID == id {
				sub = *s
			}
		}
	})
	if sub.ID == "" {
		return 0, errors.New("подписка не найдена")
	}

	n, err := l.updateSubscription(sub)
	if err != nil {
		l.Log.Printf("подписка %s: %v", sub.URL, err)
	}
	_ = l.Store.Update(func(d *store.Data) {
		for _, s := range d.Subscriptions {
			if s.ID == id {
				s.LastError = ""
				if err != nil {
					s.LastError = err.Error()
				}
			}
		}
	})
	l.changed()
	return n, err
}

func (l *Library) updateSubscription(sub store.Subscription) (int, error) {
	body, header, err := l.Fetch(sub.URL)
	if err != nil {
		return 0, fmt.Errorf("загрузка подписки: %w", err)
	}
	text := profile.Decode(body)
	info := subscription.Parse(header, text)
	servers, errs := profile.ParseMany(text)
	for _, s := range servers {
		s.SubID = sub.ID
	}

	err = l.Store.Update(func(d *store.Data) {
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
func (l *Library) RenameSubscription(id, name string) error {
	err := l.Store.Update(func(d *store.Data) {
		for _, s := range d.Subscriptions {
			if s.ID == id {
				s.Name = strings.TrimSpace(name)
			}
		}
	})
	l.changed()
	return err
}

// UpdateAllSubscriptions refreshes every subscription.
func (l *Library) UpdateAllSubscriptions() error {
	var ids []string
	l.Store.Read(func(d *store.Data) {
		for _, s := range d.Subscriptions {
			ids = append(ids, s.ID)
		}
	})
	var errs []error
	for _, id := range ids {
		if _, err := l.UpdateSubscription(id); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// AutoUpdateLoop refreshes subscriptions according to the interval the
// server requested (profile-update-interval), or every 12 hours. It never
// returns.
func (l *Library) AutoUpdateLoop() {
	time.Sleep(20 * time.Second)
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for {
		var due []string
		now := time.Now().Unix()
		l.Store.Read(func(d *store.Data) {
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
			_, _ = l.UpdateSubscription(id)
		}
		<-t.C
	}
}

// Fetch downloads a URL with the client from HTTP.
func (l *Library) Fetch(rawURL string) (string, http.Header, error) {
	client := l.HTTP(30 * time.Second)
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
