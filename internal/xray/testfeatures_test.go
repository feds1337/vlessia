package xray

import (
	"encoding/base64"
	"testing"

	// Server-side inbounds used by the end-to-end tests only.
	_ "github.com/xtls/xray-core/proxy/vless/inbound"

	"github.com/feds1337/vlessia/internal/profile"
)

// TestStartEveryProtocol starts Xray with every supported outbound: Start
// fails when a config refers to an unregistered feature.
func TestStartEveryProtocol(t *testing.T) {
	all := append([]string{}, links...)
	all = append(all,
		"ss://"+base64.RawURLEncoding.EncodeToString([]byte("2022-blake3-aes-128-gcm:"+base64.StdEncoding.EncodeToString(make([]byte, 16))))+"@1.2.3.4:443#ss2022",
		"socks5://u:p@1.2.3.4:1080#socks",
		"https://u:p@1.2.3.4:443#https-proxy",
		"vless://b831381d-6324-4d53-ad4f-8cda48b30811@1.2.3.4:443?type=httpupgrade&security=tls&sni=a.com#httpupgrade",
		"vless://b831381d-6324-4d53-ad4f-8cda48b30811@1.2.3.4:80?type=tcp&headerType=http&host=a.com#tcp-http-header",
	)
	for i, link := range all {
		s, err := profile.ParseLink(link)
		if err != nil {
			t.Fatalf("%s: %v", link, err)
		}
		cfg, err := BuildConfig(s, Options{SocksPort: 25000 + i*2, HTTPPort: 25001 + i*2, BypassRU: true})
		if err != nil {
			t.Fatal(err)
		}
		inst, err := Start(cfg)
		if err != nil {
			t.Fatalf("%s: %v", s.Name, err)
		}
		_ = inst.Close()
	}
}
