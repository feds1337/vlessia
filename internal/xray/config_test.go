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
	"vless://b831381d-6324-4d53-ad4f-8cda48b30811@10.0.0.1:80?type=ws&path=%2Fws&host=example.com#ws-plain",
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
				if s.Security == "none" && s.Protocol == "vless" {
					o.ServerAddress = "10.0.0.1"
				}
			}
			cfg, err := BuildConfig(s, o)
			if err != nil {
				t.Fatalf("build %s: %v", s.Name, err)
			}
			if _, err := serial.LoadJSONConfig(bytes.NewReader(cfg)); err != nil {
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
