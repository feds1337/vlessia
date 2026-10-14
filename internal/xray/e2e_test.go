package xray

import (
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/feds1337/vlessia/internal/profile"
)

// TestEndToEnd runs a local VLESS server and the client config against it,
// then makes an HTTPS request through the client's HTTP inbound.
// Needs internet access; enable with E2E=1.
func TestEndToEnd(t *testing.T) {
	if os.Getenv("E2E") == "" {
		t.Skip("set E2E=1")
	}
	const uuid = "b831381d-6324-4d53-ad4f-8cda48b30811"
	server, err := Start([]byte(`{
		"log": {"loglevel": "warning"},
		"inbounds": [{"listen": "127.0.0.1", "port": 24443, "protocol": "vless",
			"settings": {"clients": [{"id": "` + uuid + `"}], "decryption": "none"},
			"streamSettings": {"network": "ws", "wsSettings": {"path": "/ws"}}}],
		"outbounds": [{"protocol": "freedom"}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	s, err := profile.ParseLink("vless://" + uuid + "@127.0.0.1:24443?type=ws&path=%2Fws&security=none#local")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := BuildConfig(s, Options{SocksPort: 21080, HTTPPort: 21081})
	if err != nil {
		t.Fatal(err)
	}
	client, err := Start(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	proxy, _ := url.Parse("http://127.0.0.1:21081")
	hc := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxy)}, Timeout: 15 * time.Second}
	resp, err := hc.Get("https://www.gstatic.com/generate_204")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status %d", resp.StatusCode)
	}
	up, down := client.Traffic()
	if up == 0 || down == 0 {
		t.Fatalf("traffic counters not updated: up=%d down=%d", up, down)
	}
	t.Logf("ok, up=%d down=%d", up, down)
}
