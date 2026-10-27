package mobile

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type fakeHost struct {
	mu     sync.Mutex
	events []string
	clip   string
}

func (h *fakeHost) Emit(name, payload string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, name)
}
func (h *fakeHost) OpenTun(string) (int32, error) { return 0, errors.New("нет VPN") }
func (h *fakeHost) TunClosed()                    {}
func (h *fakeHost) OpenURL(string)                {}
func (h *fakeHost) SetClipboard(text string)      { h.clip = text }
func (h *fakeHost) PlaySound(string)              {}
func (h *fakeHost) InstallPackage(string)         {}
func (h *fakeHost) OpenVpnSettings()              {}

func TestCall(t *testing.T) {
	host := &fakeHost{}
	if err := Start(t.TempDir(), "dev", "arm64-v8a", host); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		appMu.Lock()
		_ = app.logFile.Close()
		app = nil
		appMu.Unlock()
	})

	link := "vless://11111111-1111-1111-1111-111111111111@1.2.3.4:443#Нидерланды"
	args, _ := json.Marshal([]string{link})
	out, err := Call("ImportText", string(args))
	if err != nil || out != "1" {
		t.Fatalf("ImportText = %q, %v", out, err)
	}

	out, err = Call("GetState", "[]")
	if err != nil {
		t.Fatal(err)
	}
	var st appState
	if err := json.Unmarshal([]byte(out), &st); err != nil {
		t.Fatal(err)
	}
	if len(st.Servers) != 1 || st.Selected != st.Servers[0].ID || st.Status.State != "disconnected" {
		t.Fatalf("state: %+v", st)
	}
	if ServerName() != "Нидерланды" {
		t.Errorf("ServerName = %q", ServerName())
	}

	id, _ := json.Marshal([]string{st.Servers[0].ID})
	if _, err := Call("CopyLink", string(id)); err != nil || host.clip != link {
		t.Errorf("CopyLink: %v, clipboard %q", err, host.clip)
	}
	if out, err := Call("GetPlatform", ""); err != nil || out != `"android"` {
		t.Errorf("GetPlatform = %q, %v", out, err)
	}
	if _, err := Call("GetTraffic", ""); err != nil {
		t.Errorf("GetTraffic: %v", err)
	}
	if _, err := Call("SetAutoBest", "[true]"); err != nil {
		t.Errorf("SetAutoBest: %v", err)
	}

	// Settings arrive as a JSON object; the mode is always TUN on Android.
	settings, _ := json.Marshal([]any{map[string]any{
		"mode": "proxy", "socksPort": 10808, "httpPort": 10809, "dns": []string{" 1.1.1.1 ", ""}, "logLevel": "warning",
		"directRules": []string{" example.com ", ""},
	}})
	if _, err := Call("SaveSettings", string(settings)); err != nil {
		t.Fatal(err)
	}
	out, _ = Call("GetState", "")
	_ = json.Unmarshal([]byte(out), &st)
	if st.Settings.Mode != "tun" || strings.Join(st.Settings.DNS, ",") != "1.1.1.1" || strings.Join(st.Settings.DirectRules, ",") != "example.com" {
		t.Errorf("settings: %+v", st.Settings)
	}

	// Errors come back as errors, unknown and unexported methods are refused.
	if _, err := Call("Connect", "[]"); err == nil {
		t.Error("Connect should fail when the VPN interface cannot be opened")
	}
	if _, err := Call("NoSuchMethod", "[]"); err == nil {
		t.Error("unknown method was called")
	}
	if _, err := Call("emitState", "[]"); err == nil {
		t.Error("unexported method was called")
	}
	if _, err := Call("DeleteServer", "[]"); err == nil {
		t.Error("missing arguments were accepted")
	}
	if len(host.events) == 0 {
		t.Error("no events were emitted")
	}
}

func TestCleanAPKs(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"Vlessia-v0.3.0-android-arm64-v8a.apk",
		"Vlessia-v0.4.0-android-arm64-v8a.apk",
		"Vlessia-v0.5.0-android-arm64-v8a.apk",
		"Vlessia-v0.5.0-android-arm64-v8a.apk.part",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("apk"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cleanInstalledAPKs(dir, "0.4.0")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "Vlessia-v0.5.0-android-arm64-v8a.apk" {
		t.Fatalf("remaining files: %v", entries)
	}
}
