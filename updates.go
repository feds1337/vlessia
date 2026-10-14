package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/feds1337/vlessia/internal/store"
	"github.com/feds1337/vlessia/internal/update"
)

// UpdateInfo is the update state shown in the UI.
type UpdateInfo struct {
	// State: idle | checking | latest | available | downloading | installing | error
	State     string  `json:"state"`
	Current   string  `json:"current"`
	Latest    string  `json:"latest"`
	Notes     string  `json:"notes"`
	PageURL   string  `json:"pageUrl"`
	Installed bool    `json:"installed"` // false for the portable build: no in-place update
	Progress  float64 `json:"progress"`  // 0..1 while downloading
	Error     string  `json:"error"`
}

type updater struct {
	mu      sync.Mutex
	info    UpdateInfo
	release *update.Release
}

func (a *App) updateState() UpdateInfo {
	a.upd.mu.Lock()
	defer a.upd.mu.Unlock()
	info := a.upd.info
	info.Current = version
	info.Installed = update.Installed()
	if info.State == "" {
		info.State = "idle"
	}
	return info
}

func (a *App) setUpdate(fn func(u *UpdateInfo)) UpdateInfo {
	a.upd.mu.Lock()
	fn(&a.upd.info)
	a.upd.mu.Unlock()
	info := a.updateState()
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "update", info)
	}
	if a.tray != nil {
		a.tray.SetUpdate(info)
	}
	return info
}

// GetUpdate returns the current update state.
func (a *App) GetUpdate() UpdateInfo { return a.updateState() }

// CheckUpdate asks GitHub for the latest release.
func (a *App) CheckUpdate() (UpdateInfo, error) {
	a.setUpdate(func(u *UpdateInfo) { u.State, u.Error = "checking", "" })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	rel, err := update.Latest(ctx, a.httpClient(30*time.Second))
	if err != nil {
		a.log.Printf("проверка обновлений: %v", err)
		info := a.setUpdate(func(u *UpdateInfo) {
			u.State, u.Error = "error", "Не удалось проверить обновления: "+err.Error()
		})
		return info, err
	}
	a.upd.mu.Lock()
	a.upd.release = rel
	a.upd.mu.Unlock()
	return a.setUpdate(func(u *UpdateInfo) {
		u.Latest, u.Notes, u.PageURL, u.Error = rel.Version, rel.Notes, rel.PageURL, ""
		if update.Newer(version, rel.Version) {
			u.State = "available"
		} else {
			u.State = "latest"
		}
	}), nil
}

// ApplyUpdate downloads the new installer, disconnects and installs it.
// The installer closes this process and starts the new version. A portable
// copy cannot update itself; the release page is opened instead.
func (a *App) ApplyUpdate() error {
	a.upd.mu.Lock()
	rel := a.upd.release
	a.upd.mu.Unlock()
	if rel == nil || !update.Newer(version, rel.Version) {
		return errors.New("обновление не найдено")
	}
	if !update.Installed() || !rel.CanInstall() {
		runtime.BrowserOpenURL(a.ctx, rel.PageURL)
		return nil
	}

	a.setUpdate(func(u *UpdateInfo) { u.State, u.Progress, u.Error = "downloading", 0, "" })
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	var lastEmit time.Time
	dir := filepath.Join(os.TempDir(), "Vlessia-update")
	setup, err := update.Download(ctx, a.httpClient(0), rel, dir, func(done, total int64) {
		if total <= 0 || time.Since(lastEmit) < 150*time.Millisecond {
			return
		}
		lastEmit = time.Now()
		a.setUpdate(func(u *UpdateInfo) { u.Progress = float64(done) / float64(total) })
	})
	if err != nil {
		a.log.Printf("загрузка обновления: %v", err)
		a.setUpdate(func(u *UpdateInfo) {
			u.State, u.Error = "available", "Не удалось скачать обновление: "+err.Error()
		})
		return err
	}

	// Disconnect cleanly first: the installer kills the process, which would
	// leave the system proxy pointing at a dead port.
	wasConnected := a.instance() != nil
	_ = a.Disconnect()
	a.setUpdate(func(u *UpdateInfo) { u.State, u.Progress = "installing", 1 })
	a.log.Printf("установка обновления %s", rel.Version)
	if err := update.Install(setup, wasConnected); err != nil {
		// Typically the UAC prompt was declined.
		a.log.Printf("установщик не запущен: %v", err)
		a.setUpdate(func(u *UpdateInfo) { u.State, u.Error = "available", "Установка отменена" })
		if wasConnected {
			go a.Connect()
		}
		return err
	}
	a.Quit()
	return nil
}

// updateLoop checks for updates shortly after start and then every six
// hours, unless disabled in the settings.
func (a *App) updateLoop() {
	if version == "dev" {
		return
	}
	time.Sleep(15 * time.Second)
	for {
		var off bool
		a.store.Read(func(d *store.Data) { off = d.Settings.NoUpdateCheck })
		if !off {
			if info := a.updateState(); info.State != "downloading" && info.State != "installing" {
				_, _ = a.CheckUpdate()
			}
		}
		time.Sleep(6 * time.Hour)
	}
}

// httpClient goes through the local proxy while connected, so that GitHub
// and subscription hosts stay reachable where they are blocked.
func (a *App) httpClient(timeout time.Duration) *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil
	if p := a.localProxyURL(); p != nil {
		tr.Proxy = http.ProxyURL(p)
	}
	return &http.Client{Transport: tr, Timeout: timeout}
}
