package main

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func (a *App) ExportBackup() (bool, error) {
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		DefaultFilename: "vlessia-backup.json",
		Filters:         []runtime.FileFilter{{DisplayName: "Vlessia backup (JSON)", Pattern: "*.json"}},
	})
	if err != nil || path == "" {
		return false, err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".vlessia-backup-*.tmp")
	if err != nil {
		return false, err
	}
	defer os.Remove(f.Name())
	err = a.lib.WriteBackup(f)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return false, err
	}
	err = os.Rename(f.Name(), path)
	return err == nil, err
}

func (a *App) RestoreBackup() (bool, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Filters: []runtime.FileFilter{{DisplayName: "Vlessia backup (JSON)", Pattern: "*.json"}},
	})
	if err != nil || path == "" {
		return false, err
	}
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	a.opMu.Lock()
	defer a.opMu.Unlock()
	if a.instance() != nil {
		return false, errors.New("отключите VPN перед восстановлением")
	}
	if err := a.lib.RestoreBackup(f, false); err != nil {
		return false, err
	}
	a.applyWindowTheme(a.GetState().Settings.Theme)
	a.syncKillFrom("")
	a.emitState()
	return true, nil
}
