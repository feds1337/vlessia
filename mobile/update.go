package mobile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/feds1337/vlessia/internal/store"
	"github.com/feds1337/vlessia/internal/update"
)

// updateInfo is the update state shown in the UI (same as on desktop).
type updateInfo struct {
	// State: idle | checking | latest | available | downloading | installing | error
	State     string  `json:"state"`
	Current   string  `json:"current"`
	Latest    string  `json:"latest"`
	Notes     string  `json:"notes"`
	PageURL   string  `json:"pageUrl"`
	Installed bool    `json:"installed"`
	Progress  float64 `json:"progress"`
	Error     string  `json:"error"`
}

type updater struct {
	mu          sync.Mutex
	info        updateInfo
	release     *update.Release
	applying    bool
	autoApplied string
}

func (a *client) updateState() updateInfo {
	a.upd.mu.Lock()
	defer a.upd.mu.Unlock()
	info := a.upd.info
	info.Current = a.version
	info.Installed = true
	if info.State == "" {
		info.State = "idle"
	}
	return info
}

func (a *client) setUpdate(fn func(u *updateInfo)) updateInfo {
	a.upd.mu.Lock()
	fn(&a.upd.info)
	a.upd.mu.Unlock()
	info := a.updateState()
	a.emit("update", info)
	return info
}

func (a *client) GetUpdate() updateInfo { return a.updateState() }

// CheckUpdate asks GitHub for the latest release.
func (a *client) CheckUpdate() (updateInfo, error) {
	a.setUpdate(func(u *updateInfo) { u.State, u.Error = "checking", "" })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	rel, err := update.Latest(ctx, a.httpClient(30*time.Second))
	if err != nil {
		a.log.Printf("проверка обновлений: %v", err)
		info := a.setUpdate(func(u *updateInfo) {
			u.State, u.Error = "error", "Не удалось проверить обновления: "+err.Error()
		})
		return info, err
	}
	rel = rel.ForAndroid(a.abi)
	a.upd.mu.Lock()
	a.upd.release = rel
	a.upd.mu.Unlock()
	return a.setUpdate(func(u *updateInfo) {
		u.Latest, u.Notes, u.PageURL, u.Error = rel.Version, rel.Notes, rel.PageURL, ""
		if update.Newer(a.version, rel.Version) {
			u.State = "available"
		} else {
			u.State = "latest"
		}
	}), nil
}

// ApplyUpdate downloads the APK for this device, checks it against the
// release's SHA256SUMS.txt and triggers installation. Without an APK
// in the release the release page is opened instead.
func (a *client) ApplyUpdate() error {
	a.upd.mu.Lock()
	if a.upd.applying {
		a.upd.mu.Unlock()
		return nil
	}
	rel := a.upd.release
	if rel == nil || !update.Newer(a.version, rel.Version) {
		a.upd.mu.Unlock()
		return errors.New("обновление не найдено")
	}
	if !rel.CanInstall() {
		a.upd.mu.Unlock()
		a.host.OpenURL(rel.PageURL)
		return nil
	}
	a.upd.applying = true
	a.upd.mu.Unlock()
	defer func() {
		a.upd.mu.Lock()
		a.upd.applying = false
		a.upd.mu.Unlock()
	}()

	a.setUpdate(func(u *updateInfo) { u.State, u.Progress, u.Error = "downloading", 0, "" })
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	var lastEmit time.Time
	dir := filepath.Join(a.store.Dir(), "updates")
	apk, err := update.Download(ctx, a.httpClient(0), rel, dir, func(done, total int64) {
		if total <= 0 || time.Since(lastEmit) < 150*time.Millisecond {
			return
		}
		lastEmit = time.Now()
		a.setUpdate(func(u *updateInfo) { u.Progress = float64(done) / float64(total) })
	})
	if err != nil {
		a.log.Printf("загрузка обновления: %v", err)
		a.setUpdate(func(u *updateInfo) {
			u.State, u.Error = "available", "Не удалось скачать обновление: "+err.Error()
		})
		return err
	}
	cleanOldAPKs(dir, filepath.Base(apk))
	a.log.Printf("установка обновления %s", rel.Version)
	a.setUpdate(func(u *updateInfo) { u.State, u.Progress = "installing", 1 })
	a.host.InstallPackage(apk)
	// The installer is asynchronous; keep the update available in the UI
	// until the new version replaces this one.
	a.setUpdate(func(u *updateInfo) { u.State, u.Progress = "available", 0 })
	return nil
}

// updateLoop checks for updates shortly after start and then every six
// hours, automatically downloading and installing new releases unless
// disabled in the settings.
func (a *client) updateLoop() {
	if a.version == "dev" {
		return
	}
	cleanInstalledAPKs(filepath.Join(a.store.Dir(), "updates"), a.version)
	time.Sleep(10 * time.Second)
	for {
		var off bool
		a.store.Read(func(d *store.Data) { off = d.Settings.NoUpdateCheck })
		if !off {
			if info := a.updateState(); info.State != "downloading" && info.State != "installing" {
				if checked, err := a.CheckUpdate(); err == nil && checked.State == "available" {
					a.upd.mu.Lock()
					canAuto := a.upd.release != nil && a.upd.release.CanInstall() && a.upd.autoApplied != checked.Latest
					if canAuto {
						a.upd.autoApplied = checked.Latest
					}
					a.upd.mu.Unlock()
					if canAuto {
						_ = a.ApplyUpdate()
					}
				}
			}
		}
		time.Sleep(6 * time.Hour)
	}
}

func cleanOldAPKs(dir, keepName string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.Name() != keepName {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

func cleanInstalledAPKs(dir, currentVersion string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".part") {
			_ = os.Remove(filepath.Join(dir, name))
			continue
		}
		// Match Vlessia-<ver>-android-*.apk and remove if not newer than currentVersion.
		if strings.HasPrefix(name, "Vlessia-") && strings.HasSuffix(name, ".apk") {
			rest := strings.TrimPrefix(name, "Vlessia-")
			if i := strings.IndexByte(rest, '-'); i > 0 {
				ver := rest[:i]
				if !update.Newer(currentVersion, ver) {
					_ = os.Remove(filepath.Join(dir, name))
				}
			}
		}
	}
}
