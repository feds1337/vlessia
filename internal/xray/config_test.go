package xray

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"strings"
	"testing"

	"github.com/xtls/xray-core/infra/conf/serial"

	"github.com/feds1337/vlessia/internal/profile"
)

var vmessJSON = `{"v":"2","ps":"vm","add":"example.com","port":"443","id":"b831381d-6324-4d53-ad4f-8cda48b30811","aid":"0","scy":"auto","net":"ws","type":"none","host":"example.com","path":"/ws","tls":"tls","sni":"example.com","fp":"chrome"}`

var links = []string{
	"vless://b831381d-6324-4d53-ad4f-8cda48b30811@1.2.3.4:443?encryption=none&flow=xtls-rprx-vision&security=reality&sni=www.microsoft.com&fp=chrome&pbk=SbVKOEMjK0sIlbwg4akyBg5mL5KZwwB-ed4eEE7YnRc&sid=6ba85179e30d4fc2&type=tcp&headerType=none#Reality%20NL",
	"vless://b831381d-6324-4d53-ad4f-8cda48b30811@example.com:443?type=xhttp&security=tls&sni=example.com&path=%2Fxh&mode=auto&alpn=h2%2Chttp%2F1.1#xhttp",
	"vless://b831381d-6324-4d53-ad4f-8cda48b30811@example.com:443?type=grpc&security=tls&serviceName=grpcsvc&mode=multi#grpc",
	"vless://b831381d-6324-4d53-ad4f-8cda48b30811@example.com:80?type=ws&path=%2Fws&host=example.com#ws-plain",
	"trojan://secret@example.com:443?security=tls&sni=example.com&type=tcp#trojan",
	"ss://" + base64.RawURLEncoding.EncodeToString([]byte("chacha20-ietf-poly1305:pass")) + "@5.6.7.8:8388#ss-sip002",
	"ss://" + base64.StdEncoding.EncodeToString([]byte("aes-256-gcm:pass@5.6.7.8:8388")) + "#ss-legacy",
	"vmess://" + base64.StdEncoding.EncodeToString([]byte(vmessJSON)),
	"hysteria2://p%40ss@example.com:443/?sni=example.com&obfs=salamander&obfs-password=secret&pinSHA256=" + strings.Repeat("ab", 32) + "#hy2-obfs",
	"hy2://pass@1.2.3.4:443,20000-30000?insecure=1&upmbps=50&downmbps=200#hy2-hop",
}

func TestParseAndBuild(t *testing.T) {
	for _, link := range links {
		s, err := profile.ParseLink(link)
		if err != nil {
			t.Fatalf("parse %q: %v", link, err)
		}
		for _, tunMode := range []bool{false, true} {
			o := Options{SocksPort: 10808, HTTPPort: 10809, BypassRU: true}
			if tunMode {
				// Leave TunName empty on the first link to verify the default
				// ("tun0") is filled in and Xray does not call net.Interfaces()
				// (which fails on Android with netlinkrib: permission denied).
				o.TUN, o.Interface, o.ServerAddress = true, "Ethernet", "9.9.9.9"
				if link != links[0] {
					o.TunName = "Vlessia"
				}
			}
			cfg, err := BuildConfig(s, o)
			if err != nil {
				t.Fatalf("build %s: %v", s.Name, err)
			}
			if _, err := loadConfig(cfg); err != nil {
				t.Fatalf("xray rejected config for %s (tun=%v): %v\n%s", s.Name, tunMode, err, cfg)
			}
		}
	}
}

func TestParseFields(t *testing.T) {
	s, err := profile.ParseLink(links[0])
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "Reality NL" || s.Security != "reality" || s.Flow != "xtls-rprx-vision" || s.ShortID != "6ba85179e30d4fc2" || s.Port != 443 {
		t.Fatalf("unexpected: %+v", s)
	}
	ss, err := profile.ParseLink(links[6])
	if err != nil || ss.Cipher != "aes-256-gcm" || ss.Password != "pass" || ss.Address != "5.6.7.8" {
		t.Fatalf("legacy ss: %+v %v", ss, err)
	}
	vm, err := profile.ParseLink(links[7])
	if err != nil || vm.Network != "ws" || vm.Path != "/ws" || vm.Security != "tls" {
		t.Fatalf("vmess: %+v %v", vm, err)
	}
}

func TestParseManyBase64(t *testing.T) {
	body := base64.StdEncoding.EncodeToString([]byte(links[0] + "\n" + links[4] + "\ngarbage\n"))
	servers, errs := profile.ParseMany(body)
	if len(servers) != 2 || len(errs) != 1 {
		t.Fatalf("got %d servers, %d errors", len(servers), len(errs))
	}
}

func TestHysteria2Parse(t *testing.T) {
	s, err := profile.ParseLink("hy2://pass@1.2.3.4:443,20000-30000?insecure=1&upmbps=50&mport=40000#x")
	if err != nil {
		t.Fatal(err)
	}
	if s.Port != 443 || s.HopPorts != "443,20000-30000,40000" || !s.Insecure || s.UpMbps != 50 || s.Password != "pass" {
		t.Fatalf("%+v", s)
	}
	s, err = profile.ParseLink(links[8])
	if err != nil || s.Password != "p@ss" || s.Obfs != "salamander" || s.HopPorts != "" {
		t.Fatalf("%+v %v", s, err)
	}
}

const awgConf = `[Interface]
PrivateKey = yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk=
Address = 10.8.1.2/32, fd00::2/128
DNS = 1.1.1.1
MTU = 1280
Jc = 4
Jmin = 40
Jmax = 70
S1 = 52
S2 = 18
H1 = 1234567
H2 = 2345678
H3 = 3456789
H4 = 4567890
J1 = <b 0xff>

[Peer]
PublicKey = xTIBA5rboUvnH4htodjb6e697QjLERt1NAB4mZqp8Dg=
PresharedKey = TrMvSoP4jYQlY6RIzBgbssQqY3vxI2Pi+y71lOWWXX0=
Endpoint = vpn.example.com:51820
AllowedIPs = 0.0.0.0/0, ::/0
PersistentKeepalive = 25
`

func TestWireGuardConf(t *testing.T) {
	servers, errs := profile.ParseMany(awgConf)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	s := servers[0]
	if s.Protocol != "amneziawg" || s.Address != "vpn.example.com" || s.Port != 51820 || len(s.WG.AWG) != 10 || s.WG.MTU != 1280 {
		t.Fatalf("%+v %+v", s, s.WG)
	}
	cfg, err := BuildConfig(s, Options{SocksPort: 1, HTTPPort: 2, BridgePort: 3, TUN: true, TunName: "t", Interface: "Ethernet"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := serial.LoadJSONConfig(bytes.NewReader(cfg)); err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	_ = json.Unmarshal(cfg, &parsed)
	if p := parsed.Outbounds[0]; p["protocol"] != "socks" || p["streamSettings"] != nil {
		t.Fatalf("bridge outbound must be plain loopback socks: %v", p)
	}
}

// TestChainConfig: the relay (multi-tunnel) becomes a second outbound the
// main one points at with outboundTag.
func TestChainConfig(t *testing.T) {
	main, err := profile.ParseLink(links[0])
	if err != nil {
		t.Fatal(err)
	}
	relay, err := profile.ParseLink(links[4]) // trojan
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := BuildConfig(main, Options{
		SocksPort: 10808, HTTPPort: 10809, TUN: true, TunName: "Vlessia", Interface: "Ethernet",
		ServerAddress: "9.9.9.9", Via: relay, ViaAddr: "5.5.5.5",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(cfg); err != nil {
		t.Fatalf("xray rejected the chained config: %v\n%s", err, cfg)
	}
	var parsed struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	if err := json.Unmarshal(cfg, &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Outbounds) != 5 {
		t.Fatalf("outbounds = %d, want proxy, via, direct, block, dns", len(parsed.Outbounds))
	}
	proxy, via := parsed.Outbounds[0], parsed.Outbounds[1]
	if proxy["tag"] != TagProxy || proxy["outboundTag"] != TagVia {
		t.Fatalf("the proxy outbound must go through the relay: %v", proxy)
	}
	if via["tag"] != TagVia || via["protocol"] != "trojan" {
		t.Fatalf("relay outbound: %v", via)
	}
	if !strings.Contains(string(cfg), `"5.5.5.5"`) {
		t.Fatal("the relay must use the pre-resolved address")
	}
}

// TestBalanceConfig: the balancing tag replaces the single proxy outbound
// and the routing rules keep pointing at "proxy".
func TestBalanceConfig(t *testing.T) {
	a, _ := profile.ParseLink(links[0])
	b, _ := profile.ParseLink(links[1])
	cfg, err := BuildConfig(a, Options{
		SocksPort: 10808, HTTPPort: 10809, TUN: true, TunName: "Vlessia", Interface: "Ethernet",
		Balance: []*profile.Server{a, b}, BalanceAddrs: []string{"1.1.1.1", "2.2.2.2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(cfg); err != nil {
		t.Fatalf("xray rejected the balancing config: %v\n%s", err, cfg)
	}
	var parsed struct {
		Outbounds []map[string]any `json:"outbounds"`
		Balancing []map[string]any `json:"balancing"`
	}
	if err := json.Unmarshal(cfg, &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Balancing) != 1 {
		t.Fatalf("balancing sections = %d", len(parsed.Balancing))
	}
	bal := parsed.Balancing[0]
	if bal["tag"] != TagProxy || bal["strategy"] != "roundrobin" {
		t.Fatalf("balancing: %v", bal)
	}
	tags := bal["outbounds"].([]any)
	if len(tags) != 2 || tags[0] != TagBalance+"-1" || tags[1] != TagBalance+"-2" {
		t.Fatalf("balanced outbounds: %v", tags)
	}
	for _, ob := range parsed.Outbounds {
		if ob["tag"] == TagProxy {
			t.Fatalf("the proxy must be the balancing tag, not an outbound: %v", ob)
		}
	}
}

// TestBlockConfig: the kill switch config must drop everything; the only
// non-block outbound is the direct one for private networks.
func TestBlockConfig(t *testing.T) {
	cfg, err := BuildConfig(nil, Options{Block: true, TUN: true, TunName: "Vlessia", SocksPort: 10808, HTTPPort: 10809})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(cfg); err != nil {
		t.Fatalf("xray rejected the block config: %v\n%s", err, cfg)
	}
	var parsed struct {
		Inbounds  []map[string]any `json:"inbounds"`
		Outbounds []map[string]any `json:"outbounds"`
	}
	if err := json.Unmarshal(cfg, &parsed); err != nil {
		t.Fatal(err)
	}
	// The first outbound is the default one: it must be the black hole.
	if parsed.Outbounds[0]["tag"] != TagBlock || parsed.Outbounds[0]["protocol"] != "blackhole" {
		t.Fatalf("the default outbound must block: %v", parsed.Outbounds[0])
	}
	var tun bool
	for _, in := range parsed.Inbounds {
		if in["tag"] == TagTun {
			tun = true
		}
	}
	if !tun {
		t.Fatal("the block config has no TUN inbound: the adapter would disappear")
	}
}

// vpn:// links from AmneziaVPN are qCompress'ed JSON.
func TestAmneziaVPNLink(t *testing.T) {
	last, _ := json.Marshal(map[string]string{"config": strings.ReplaceAll(awgConf, "DNS = 1.1.1.1", "DNS = $PRIMARY_DNS, $SECONDARY_DNS")})
	doc, _ := json.Marshal(map[string]any{
		"containers": []any{
			map[string]any{"container": "amnezia-xray", "xray": map[string]string{"last_config": "{}"}},
			map[string]any{"container": "amnezia-awg", "awg": map[string]string{"last_config": string(last)}},
		},
		"defaultContainer": "amnezia-awg",
		"description":      "Мой сервер",
		"dns1":             "9.9.9.9",
	})
	var z bytes.Buffer
	_ = binary.Write(&z, binary.BigEndian, uint32(len(doc)))
	w := zlib.NewWriter(&z)
	_, _ = w.Write(doc)
	_ = w.Close()
	link := "vpn://" + base64.RawURLEncoding.EncodeToString(z.Bytes())

	s, err := profile.ParseLink(link)
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "Мой сервер" || s.Protocol != "amneziawg" || strings.Join(s.WG.DNS, ",") != "9.9.9.9,1.0.0.1" {
		t.Fatalf("%+v %+v", s, s.WG)
	}
}
