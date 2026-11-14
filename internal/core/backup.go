package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/feds1337/vlessia/internal/profile"
	"github.com/feds1337/vlessia/internal/store"
	"github.com/feds1337/vlessia/internal/xray"
)

const MaxBackupSize = 32 << 20

type backup struct {
	Format        string                `json:"format"`
	Version       int                   `json:"version"`
	Servers       []*profile.Server     `json:"servers"`
	Subscriptions []*store.Subscription `json:"subscriptions"`
	Selected      string                `json:"selected"`
	Settings      *store.Settings       `json:"settings"`
}

func (l *Library) WriteBackup(w io.Writer) error {
	var b backup
	l.Store.Read(func(d *store.Data) {
		settings := d.Settings
		b = backup{"vlessia-backup", 1, d.Servers, d.Subscriptions, d.Selected, &settings}
	})
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	if len(data) > MaxBackupSize {
		return errors.New("файл слишком большой")
	}
	_, err = w.Write(data)
	return err
}

func (l *Library) RestoreBackup(r io.Reader, android bool) error {
	data, err := io.ReadAll(io.LimitReader(r, MaxBackupSize+1))
	if err != nil {
		return err
	}
	if len(data) > MaxBackupSize {
		return errors.New("файл слишком большой")
	}
	var b backup
	if err := json.Unmarshal(data, &b); err != nil || b.Format != "vlessia-backup" || b.Version != 1 || b.Settings == nil {
		return errors.New("неподдерживаемый формат резервной копии")
	}
	if err := b.validate(); err != nil {
		return err
	}
	if android {
		b.Settings.Mode = "tun"
	}
	l.refreshMu.Lock()
	defer l.refreshMu.Unlock()
	if len(l.refreshing) != 0 {
		return errors.New("дождитесь обновления подписок")
	}
	return l.Store.Update(func(d *store.Data) {
		d.Servers, d.Subscriptions, d.Selected, d.Settings = b.Servers, b.Subscriptions, b.Selected, *b.Settings
	})
}

func (b *backup) validate() error {
	settings := b.Settings
	if settings.Mode != "proxy" && settings.Mode != "tun" {
		return errors.New("неизвестный режим")
	}
	if !validPort(settings.SocksPort) || !validPort(settings.HTTPPort) || settings.SocksPort == settings.HTTPPort {
		return errors.New("неверные порты")
	}
	for _, rules := range [][]string{settings.DirectRules, settings.ProxyRules} {
		if _, _, err := xray.ParseRules(rules); err != nil {
			return err
		}
	}
	subs := make(map[string]bool, len(b.Subscriptions))
	for i, sub := range b.Subscriptions {
		if sub == nil || sub.ID == "" || subs[sub.ID] || !IsWebURL(sub.URL) {
			return fmt.Errorf("неверная подписка в резервной копии (№%d)", i+1)
		}
		subs[sub.ID] = true
	}
	servers := make(map[string]bool, len(b.Servers))
	for i, s := range b.Servers {
		if s == nil || s.ID == "" || servers[s.ID] || s.Address == "" || !validPort(s.Port) ||
			(s.SubID != "" && !subs[s.SubID]) {
			return fmt.Errorf("неверный сервер в резервной копии (№%d)", i+1)
		}
		servers[s.ID] = true
	}
	// Older builds could leave dangling selections and relay references.
	if !servers[b.Selected] {
		b.Selected = ""
		if len(b.Servers) != 0 {
			b.Selected = b.Servers[0].ID
		}
	}
	for _, s := range b.Servers {
		if s.Via == s.ID || !servers[s.Via] {
			s.Via = ""
		}
	}
	return nil
}

func validPort(p int) bool { return p > 0 && p <= 65535 }
