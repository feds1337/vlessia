package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateLegacyDir(t *testing.T) {
	write := func(path, data string) {
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	read := func(path string) string {
		b, _ := os.ReadFile(path)
		return string(b)
	}

	t.Run("new folder missing: moved", func(t *testing.T) {
		base := t.TempDir()
		old, dir := filepath.Join(base, "VPNClient"), filepath.Join(base, "Vlessia")
		write(filepath.Join(old, "config.json"), "old")
		migrateLegacyDir(old, dir)
		if read(filepath.Join(dir, "config.json")) != "old" {
			t.Fatal("config not moved")
		}
	})
	t.Run("new folder without config: copied", func(t *testing.T) {
		base := t.TempDir()
		old, dir := filepath.Join(base, "VPNClient"), filepath.Join(base, "Vlessia")
		write(filepath.Join(old, "config.json"), "old")
		write(filepath.Join(dir, "app.log"), "log")
		migrateLegacyDir(old, dir)
		if read(filepath.Join(dir, "config.json")) != "old" {
			t.Fatal("config not copied into the existing folder")
		}
	})
	t.Run("new config present: untouched", func(t *testing.T) {
		base := t.TempDir()
		old, dir := filepath.Join(base, "VPNClient"), filepath.Join(base, "Vlessia")
		write(filepath.Join(old, "config.json"), "old")
		write(filepath.Join(dir, "config.json"), "new")
		migrateLegacyDir(old, dir)
		if read(filepath.Join(dir, "config.json")) != "new" {
			t.Fatal("existing config overwritten")
		}
	})
}
