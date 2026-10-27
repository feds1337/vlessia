package xray

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/feds1337/vlessia/internal/profile"
)

// TestHysteriaResetLayout checks that the linknamed Hysteria client cache
// still matches xray-core: a dial must add an entry and Close must remove it.
func TestHysteriaResetLayout(t *testing.T) {
	s, err := profile.ParseLink("hy2://x@127.0.0.1:9?sni=a.test#unreachable")
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := BuildConfig(s, Options{SocksPort: 21380, HTTPPort: 21381})
	inst, err := Start(cfg)
	if err != nil {
		t.Fatal(err)
	}
	proxy, _ := url.Parse("http://127.0.0.1:21381")
	hc := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxy)}, Timeout: 3 * time.Second}
	_, _ = hc.Get("http://example.com/") // fails, but creates the cached client

	hyManager.RLock()
	n := len(hyManager.m)
	hyManager.RUnlock()
	if n != 1 {
		t.Fatalf("expected 1 cached Hysteria client, got %d (xray internals changed?)", n)
	}
	_ = inst.Close()
	if len(hyManager.m) != 0 {
		t.Fatal("Hysteria client cache was not reset")
	}
}
