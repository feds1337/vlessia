package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/feds1337/vlessia/internal/profile"
)

// TestSettingsJSON: the web interface reads these exact keys, and the new
// features start switched off.
func TestSettingsJSON(t *testing.T) {
	d := DefaultSettings()
	if d.KillSwitch || d.Balance {
		t.Fatalf("new settings must default to off: %+v", d)
	}
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"killSwitch", "balance"} {
		if _, ok := m[key]; !ok {
			t.Errorf("settings JSON has no %q key: %s", key, b)
		}
	}
}

// TestDataJSON: the relay of a chain and the reminder state survive a
// round trip through config.json.
func TestDataJSON(t *testing.T) {
	in := Data{
		Servers:       []*profile.Server{{ID: "a", Name: "A", Protocol: "vless", Address: "1.2.3.4", Port: 443, Via: "b"}},
		Subscriptions: []*Subscription{{ID: "s", Name: "S", Total: 100, Notified: 3}},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out Data
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.Servers[0].Via != "b" {
		t.Errorf("server relay lost: %+v", out.Servers[0])
	}
	if out.Subscriptions[0].Notified != 3 {
		t.Errorf("reminder state lost: %+v", out.Subscriptions[0])
	}
}

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
