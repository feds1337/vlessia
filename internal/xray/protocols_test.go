package xray

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/xtls/xray-core/infra/conf/serial"

	"github.com/feds1337/vlessia/internal/profile"
)

func mustBuild(t *testing.T, s *profile.Server, o Options) {
	t.Helper()
	cfg, err := BuildConfig(s, o)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := serial.LoadJSONConfig(bytes.NewReader(cfg)); err != nil {
		t.Fatalf("xray rejected %s: %v\n%s", s.Name, err, cfg)
	}
}

func TestWireGuardLink(t *testing.T) {
	priv := "yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk="
	pub := "xTIBA5rboUvnH4htodjb6e697QjLERt1NAB4mZqp8Dg="
	// '+' inside query values is often decoded as a space; the parser must cope.
	link := "wireguard://" + strings.ReplaceAll(priv, "+", "%2B") + "@1.2.3.4:51820?publickey=" + pub +
		"&address=10.0.0.2/32,fd00::2/128&mtu=1280&reserved=1,2,3&jc=4&jmin=40&jmax=70#WG%20link"
	s, err := profile.ParseLink(link)
	if err != nil {
		t.Fatal(err)
	}
	if s.Protocol != "amneziawg" || s.Name != "WG link" || s.WG.PublicKey != pub || s.WG.PrivateKey != priv ||
		len(s.WG.Addresses) != 2 || s.WG.MTU != 1280 || len(s.WG.AWG) != 3 {
		t.Fatalf("%+v %+v", s, s.WG)
	}
	plain, err := profile.ParseLink("wg://" + priv + "@1.2.3.4:51820?publickey=" + pub + "&address=10.0.0.2/32")
	if err != nil || plain.Protocol != "wireguard" {
		t.Fatalf("%+v %v", plain, err)
	}
}

func TestProxyLinks(t *testing.T) {
	cases := []struct {
		link, proto, user, pass, security string
	}{
		{"socks://" + base64.StdEncoding.EncodeToString([]byte("alice:s3cret")) + "@1.2.3.4:1080#v2rayN", "socks", "alice", "s3cret", "none"},
		{"socks5://bob:pw@1.2.3.4:1080", "socks", "bob", "pw", "none"},
		{"socks5h://1.2.3.4:1080", "socks", "", "", "none"},
		{"http://u:p@proxy.example:3128#corp", "http", "u", "p", "none"},
		{"https://u:p@proxy.example:443", "http", "u", "p", "tls"},
	}
	for _, c := range cases {
		s, err := profile.ParseLink(c.link)
		if err != nil {
			t.Fatalf("%s: %v", c.link, err)
		}
		if s.Protocol != c.proto || s.Username != c.user || s.Password != c.pass || s.Security != c.security {
			t.Fatalf("%s: %+v", c.link, s)
		}
		mustBuild(t, s, Options{SocksPort: 1, HTTPPort: 2})
	}

	for raw, proxy := range map[string]bool{
		"http://1.2.3.4:3128":                true,
		"https://u:p@proxy.example":          true,
		"https://panel.example/sub/abc":      false,
		"https://panel.example:2096/sub?x=1": false,
		"https://panel.example":              false, // no port: a subscription
	} {
		if profile.IsProxyURL(raw) != proxy {
			t.Errorf("IsProxyURL(%q) != %v", raw, proxy)
		}
	}
}

func TestOutline(t *testing.T) {
	s, err := profile.ParseOutlineConfig(`{"server":"5.6.7.8","server_port":443,"password":"pw","method":"chacha20-ietf-poly1305","prefix":"\u0016\u0003\u0001"}`, "Outline")
	if err != nil {
		t.Fatal(err)
	}
	if s.Address != "5.6.7.8" || s.Port != 443 || s.Prefix != "\x16\x03\x01" || s.Name != "Outline" {
		t.Fatalf("%+v", s)
	}
	mustBuild(t, s, Options{SocksPort: 1, HTTPPort: 2})

	if _, err := profile.ParseOutlineConfig(`{"error":{"message":"ключ отозван"}}`, ""); err == nil || !strings.Contains(err.Error(), "отозван") {
		t.Fatalf("expected provider error, got %v", err)
	}

	userinfo := base64.RawURLEncoding.EncodeToString([]byte("chacha20-ietf-poly1305:pw"))
	ss, err := profile.ParseLink("ss://" + userinfo + "@5.6.7.8:443/?outline=1&prefix=%16%03%01#x")
	if err != nil || ss.Prefix != "\x16\x03\x01" || ss.Port != 443 {
		t.Fatalf("%+v %v", ss, err)
	}

	u, name, err := profile.OutlineKeyURL("ssconf://keys.example.com/abc.json#Мой ключ")
	if err != nil || u != "https://keys.example.com/abc.json" || name != "Мой ключ" {
		t.Fatalf("%q %q %v", u, name, err)
	}
}

// testPKI returns a CA and a client certificate/key in PEM.
func testPKI(t *testing.T) (caPEM, certPEM, keyPEM string) {
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	caTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test-ca"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	caDER, _ := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	ca, _ := x509.ParseCertificate(caDER)
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "client"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, KeyUsage: x509.KeyUsageDigitalSignature,
	}
	der, _ := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
	kb, _ := x509.MarshalPKCS8PrivateKey(key)
	enc := func(typ string, b []byte) string { return string(pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: b})) }
	return enc("CERTIFICATE", caDER), enc("CERTIFICATE", der), enc("PRIVATE KEY", kb)
}

// staticKey returns an OpenVPN static key (for tls-crypt / tls-auth).
func staticKey() string {
	b := make([]byte, 256)
	_, _ = rand.Read(b)
	var sb strings.Builder
	sb.WriteString("-----BEGIN OpenVPN Static key V1-----\n")
	for i := 0; i < len(b); i += 16 {
		sb.WriteString(hex.EncodeToString(b[i:i+16]) + "\n")
	}
	sb.WriteString("-----END OpenVPN Static key V1-----\n")
	return sb.String()
}

func ovpnProfile(t *testing.T, extra string) string {
	ca, cert, key := testPKI(t)
	extra += "<tls-crypt>\n" + staticKey() + "</tls-crypt>\n"
	return "client\ndev tun\nproto udp\nremote vpn.example.com 1194\nnobind\npersist-key\npersist-tun\n" +
		"remote-cert-tls server\ncipher AES-256-GCM\n" + extra +
		"<ca>\n" + ca + "</ca>\n<cert>\n" + cert + "</cert>\n<key>\n" + key + "</key>\n"
}

func TestOpenVPNProfile(t *testing.T) {
	servers, errs := profile.ParseMany(ovpnProfile(t, "auth-user-pass\n"))
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	s := servers[0]
	if s.Protocol != "openvpn" || s.Address != "vpn.example.com" || s.Port != 1194 || s.Network != "udp" || !s.OVPN.NeedAuth || !s.IsFullTunnel() {
		t.Fatalf("%+v %+v", s, s.OVPN)
	}
	mustBuild(t, s, Options{SocksPort: 1, HTTPPort: 2, BridgePort: 3})

	if _, errs := profile.ParseMany(ovpnProfile(t, "comp-lzo\n")); len(errs) == 0 || !strings.Contains(errs[0].Error(), "сжатие") {
		t.Fatalf("compression must be rejected with a clear message, got %v", errs)
	}
	noProtection := strings.Split(ovpnProfile(t, ""), "<tls-crypt>")[0]
	if _, errs := profile.ParseMany(noProtection); len(errs) == 0 || !strings.Contains(errs[0].Error(), "tls-auth") {
		t.Fatalf("missing tls-auth/tls-crypt must be explained, got %v", errs)
	}
}

// TestProxyEndToEnd: SOCKS5 and HTTP proxies with authentication. E2E=1.
func TestProxyEndToEnd(t *testing.T) {
	if os.Getenv("E2E") == "" {
		t.Skip("set E2E=1")
	}
	server, err := Start([]byte(`{
		"log": {"loglevel": "warning"},
		"inbounds": [
			{"listen": "127.0.0.1", "port": 24501, "protocol": "socks",
			 "settings": {"auth": "password", "accounts": [{"user": "alice", "pass": "s3cret"}], "udp": true}},
			{"listen": "127.0.0.1", "port": 24502, "protocol": "http",
			 "settings": {"accounts": [{"user": "bob", "pass": "pw"}]}}
		],
		"outbounds": [{"protocol": "freedom"}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	for i, c := range []struct {
		link string
		ok   bool
	}{
		{"socks://" + base64.StdEncoding.EncodeToString([]byte("alice:s3cret")) + "@127.0.0.1:24501#socks", true},
		{"socks5://alice:wrong@127.0.0.1:24501#bad", false},
		{"http://bob:pw@127.0.0.1:24502#http", true},
	} {
		s, err := profile.ParseLink(c.link)
		if err != nil {
			t.Fatal(err)
		}
		port := 21500 + i*2
		cfg, _ := BuildConfig(s, Options{SocksPort: port, HTTPPort: port + 1})
		client, err := Start(cfg)
		if err != nil {
			t.Fatal(err)
		}
		proxy, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port+1))
		hc := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxy)}, Timeout: 10 * time.Second}
		resp, err := hc.Get("https://www.gstatic.com/generate_204")
		ok := err == nil && resp.StatusCode == http.StatusNoContent
		if resp != nil {
			resp.Body.Close()
		}
		client.Close()
		if ok != c.ok {
			t.Fatalf("%s: works=%v, want %v (err %v)", s.Name, ok, c.ok, err)
		}
		t.Logf("%s: works=%v as expected", s.Name, ok)
	}
}
