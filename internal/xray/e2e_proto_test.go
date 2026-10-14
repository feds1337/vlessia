package xray

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/amnezia-vpn/amneziawg-go/v3/conn"
	"github.com/amnezia-vpn/amneziawg-go/v3/device"
	"github.com/amnezia-vpn/amneziawg-go/v3/tun/netstack"
	"golang.org/x/crypto/curve25519"

	"github.com/feds1337/vlessia/internal/awg"
	"github.com/feds1337/vlessia/internal/hysteria"
	"github.com/feds1337/vlessia/internal/profile"
)

func httpVia(t *testing.T, httpPort int, target string) (int, string) {
	t.Helper()
	proxy, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", httpPort))
	hc := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxy)}, Timeout: 15 * time.Second}
	resp, err := hc.Get(target)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func selfSigned(t *testing.T) (certPEM, keyPEM string) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "hy.test"},
		DNSNames:     []string{"hy.test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	kb, _ := x509.MarshalECPrivateKey(key)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}))
}

// TestHysteriaEndToEnd: Hysteria 2 server with a self-signed certificate
// and salamander obfuscation; the client link says insecure=1, so the
// certificate is pinned via a probe first. Needs internet; E2E=1.
func TestHysteriaEndToEnd(t *testing.T) {
	if os.Getenv("E2E") == "" {
		t.Skip("set E2E=1")
	}
	certPEM, keyPEM := selfSigned(t)
	lines := func(s string) []string { return strings.Split(strings.TrimSpace(s), "\n") }
	serverCfg, _ := json.Marshal(map[string]any{
		"log": map[string]any{"loglevel": "warning"},
		"inbounds": []any{map[string]any{
			"listen": "127.0.0.1", "port": 24444, "protocol": "hysteria",
			"settings": map[string]any{"version": 2, "clients": []any{map[string]any{"auth": "s3cret"}}},
			"streamSettings": map[string]any{
				"network": "hysteria", "security": "tls",
				"tlsSettings": map[string]any{
					"alpn":         []string{"h3"},
					"certificates": []any{map[string]any{"certificate": lines(certPEM), "key": lines(keyPEM)}},
				},
				"hysteriaSettings": map[string]any{"version": 2},
				"finalmask":        map[string]any{"udp": []any{map[string]any{"type": "salamander", "settings": map[string]any{"password": "obfspw"}}}},
			},
		}},
		"outbounds": []any{map[string]any{"protocol": "freedom"}},
	})
	server, err := Start(serverCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	s, err := profile.ParseLink("hy2://s3cret@127.0.0.1:24444/?sni=hy.test&insecure=1&obfs=salamander&obfs-password=obfspw#local")
	if err != nil {
		t.Fatal(err)
	}

	// Without a pin the self-signed certificate must be rejected.
	cfg, _ := BuildConfig(s, Options{SocksPort: 21180, HTTPPort: 21181})
	bad, err := Start(cfg)
	if err != nil {
		t.Fatal(err)
	}
	proxy, _ := url.Parse("http://127.0.0.1:21181")
	hc := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxy)}, Timeout: 8 * time.Second}
	if resp, err := hc.Get("https://www.gstatic.com/generate_204"); err == nil && resp.StatusCode == http.StatusNoContent {
		t.Fatal("connected without a pinned certificate")
	}
	bad.Close()

	res, err := hysteria.Probe(context.Background(), s, "127.0.0.1", nil)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	t.Logf("probe rtt=%v pin=%s", res.RTT, res.CertSHA256)
	s.PinnedSHA = res.CertSHA256

	cfg, _ = BuildConfig(s, Options{SocksPort: 21180, HTTPPort: 21181})
	client, err := Start(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if code, _ := httpVia(t, 21181, "https://www.gstatic.com/generate_204"); code != http.StatusNoContent {
		t.Fatalf("status %d", code)
	}
	up, down := client.Traffic()
	t.Logf("ok, up=%d down=%d", up, down)
}

func wgKeypair() (priv, pub []byte) {
	priv = make([]byte, 32)
	_, _ = rand.Read(priv)
	priv[0] &= 248
	priv[31] = (priv[31] & 127) | 64
	pub, _ = curve25519.X25519(priv, curve25519.Basepoint)
	return priv, pub
}

func freeUDPPort(t *testing.T) int {
	c, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	return c.LocalAddr().(*net.UDPAddr).Port
}

// headerKey is the AmneziaWG 3.0 header protection key of the test pair.
var headerKey = func() []byte {
	k := make([]byte, 32)
	_, _ = rand.Read(k)
	return k
}()

// awgParams are AmneziaWG 3.1 settings shared by both ends, spelled as in
// .conf files (the client imports them from a config text).
var awgParams = []profile.Param{
	{Key: "Jc", Value: "4"}, {Key: "Jmin", Value: "40"}, {Key: "Jmax", Value: "70"},
	{Key: "S1", Value: "52"}, {Key: "S2", Value: "18"}, {Key: "S3", Value: "24"}, {Key: "S4", Value: "12"},
	{Key: "H1", Value: "1234567-1234600"}, {Key: "H2", Value: "2345678-2345700"},
	{Key: "H3", Value: "3456789-3456800"}, {Key: "H4", Value: "4567890-4567900"},
	{Key: "I1", Value: "<b 0xc70000000108ce1bf31eec7d93360000449e227e4596ed7f75c4d35ce31880b4133107c822c6355b51f0d7c1bba96d5c210a48aca01885fed0871cfc37d59137d73b506dc013bb4a13c060ca5b04b7ae215af71e37d6e8ff1db235f9fe0c25cb8b492471054a7c8d0d6077d430d07f6e87a8699287f6e69f54263c7334a8e144a29851429bf2e350e519445172d36953e96085110ce1fb641e5efad42c0feb4711ece959b72cc4d6f3c1e83251adb572b921534f6ac4b10927167f41fe50040a75acef62f45bded67c0b45b9d655ce374589cad6f568b8475b2e8921ff98628f86ff2eb5bcce6f3ddb7dc89e37c5b5e78ddc8d93a58896e530b5f9f1448ab3b7a1d1f24a63bf981634f6183a21af310ffa52e9ddf5521561760288669de01a5f2f1a4f922e68d0592026bbe4329b654d4f5d6ace4f6a23b8560b720a5350691c0037b10acfac9726add44e7d3e880ee6f3b0d6429ff33655c297fee786bb5ac032e48d2062cd45e305e6d8d8b82bfbf0fdbc5ec09943d1ad02b0b5868ac4b24bb10255196be883562c35a713002014016b8cc5224768b3d330016cf8ed9300fe6bf39b4b19b3667cddc6e7c7ebe4437a58862606a2a66bd4184b09ab9d2cd3d3faed4d2ab71dd821422a9540c4c5fa2a9b2e6693d411a22854a8e541ed930796521f03a54254074bc4c5bca3a84a19c7b0f9e0f99c4e4ee8a5c56a7b22b18da5e4dfba5f6b80e6c34dcf37e4ef8b6ca4ec1ad3ba31f9bb98bdfce5ae36e83ca9e1c4ac3e3a03a3b1fe4b6a1a9e7f9eeb8bb8226d2e72ec4cc0a3ea54b1bb2f4290e8e6b14c3e1a27e6df5e1fb6e1ca3d1c3e1a3e5f3e4d1d8ee7e9e0dd5a8e8c7ff1>"},
	{Key: "HeaderProtectionKey", Value: base64.StdEncoding.EncodeToString(headerKey)},
	{Key: "RekeyAfterTime", Value: "100-120"}, {Key: "RejectAfterTime", Value: "150-180"},
	{Key: "KeepaliveTimeout", Value: "5-15"}, {Key: "MaxHandshakeAttempts", Value: "15-20"},
	{Key: "RandomTrailers", Value: "on"}, {Key: "DisableCookies", Value: "on"},
}

func startAWGServer(t *testing.T, serverPriv, clientPub []byte, port int) func() {
	tdev, tnet, err := netstack.CreateNetTUN([]netip.Addr{netip.MustParseAddr("198.18.0.1")}, nil, 1280)
	if err != nil {
		t.Fatal(err)
	}
	dev := device.NewDevice(tdev, conn.NewDefaultBind(), device.NewLogger(device.LogLevelError, "server: "))
	var uapi strings.Builder
	fmt.Fprintf(&uapi, "private_key=%s\nlisten_port=%d\n", hex.EncodeToString(serverPriv), port)
	// The server is configured directly through the UAPI, independently of
	// the client-side translation in package awg.
	serverKeys := map[string]string{
		"HeaderProtectionKey":  "header_protection_key=" + hex.EncodeToString(headerKey),
		"RekeyAfterTime":       "rekey_after_time=100-120",
		"RejectAfterTime":      "reject_after_time=150-180",
		"KeepaliveTimeout":     "keepalive_timeout=5-15",
		"MaxHandshakeAttempts": "max_handshake_attempts=15-20",
		"RandomTrailers":       "random_trailers=true",
		"DisableCookies":       "disable_cookies=true",
	}
	for _, p := range awgParams {
		if line, ok := serverKeys[p.Key]; ok {
			uapi.WriteString(line + "\n")
		} else {
			fmt.Fprintf(&uapi, "%s=%s\n", strings.ToLower(p.Key), p.Value)
		}
	}
	fmt.Fprintf(&uapi, "public_key=%s\nallowed_ip=198.18.0.2/32\n", hex.EncodeToString(clientPub))
	if err := dev.IpcSet(uapi.String()); err != nil {
		t.Fatal(err)
	}
	if err := dev.Up(); err != nil {
		t.Fatal(err)
	}

	// HTTP and UDP echo inside the tunnel.
	ln, err := tnet.ListenTCP(&net.TCPAddr{IP: net.ParseIP("198.18.0.1"), Port: 80})
	if err != nil {
		t.Fatal(err)
	}
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "hello from awg") }))
	// A peer that accepts and never hangs up, for the shutdown test.
	hold, err := tnet.ListenTCP(&net.TCPAddr{IP: net.ParseIP("198.18.0.1"), Port: 81})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, err := hold.Accept()
			if err != nil {
				return
			}
			defer c.Close()
		}
	}()
	uc, err := tnet.ListenUDP(&net.UDPAddr{IP: net.ParseIP("198.18.0.1"), Port: 7})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		buf := make([]byte, 2048)
		for {
			n, from, err := uc.ReadFrom(buf)
			if err != nil {
				return
			}
			_, _ = uc.WriteTo(buf[:n], from)
		}
	}()
	return func() { ln.Close(); hold.Close(); uc.Close(); dev.Close() }
}

// socksUDPEcho sends a datagram through a SOCKS5 UDP ASSOCIATE.
func socksUDPEcho(t *testing.T, socksPort int, target string, payload []byte) []byte {
	t.Helper()
	ctrl, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", socksPort))
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()
	_ = ctrl.SetDeadline(time.Now().Add(10 * time.Second))
	_, _ = ctrl.Write([]byte{5, 1, 0})
	buf := make([]byte, 512)
	if _, err := io.ReadFull(ctrl, buf[:2]); err != nil {
		t.Fatal(err)
	}
	_, _ = ctrl.Write([]byte{5, 3, 0, 1, 0, 0, 0, 0, 0, 0})
	if _, err := io.ReadFull(ctrl, buf[:10]); err != nil || buf[1] != 0 {
		t.Fatalf("associate failed: %v %v", err, buf[:10])
	}
	relay := &net.UDPAddr{IP: net.IP(buf[4:8]), Port: int(binary.BigEndian.Uint16(buf[8:10]))}

	uc, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer uc.Close()
	ta, _ := net.ResolveUDPAddr("udp", target)
	pkt := append([]byte{0, 0, 0, 1}, ta.IP.To4()...)
	pkt = binary.BigEndian.AppendUint16(pkt, uint16(ta.Port))
	pkt = append(pkt, payload...)
	_ = uc.SetDeadline(time.Now().Add(10 * time.Second))
	for i := 0; i < 5; i++ { // UDP may be lost while the handshake settles
		_, _ = uc.WriteToUDP(pkt, relay)
		_ = uc.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, _, err := uc.ReadFromUDP(buf)
		if err == nil && n > 10 {
			return buf[10:n]
		}
	}
	t.Fatal("no UDP echo")
	return nil
}

// TestAmneziaWGEndToEnd: an AmneziaWG 2.0 server (amneziawg-go on its own
// netstack) and the client chain xray -> socks bridge -> AWG. Fully local.
func TestAmneziaWGEndToEnd(t *testing.T) {
	serverPriv, serverPub := wgKeypair()
	clientPriv, clientPub := wgKeypair()
	port := freeUDPPort(t)
	stop := startAWGServer(t, serverPriv, clientPub, port)
	defer stop()

	conf := fmt.Sprintf("[Interface]\nPrivateKey = %s\nAddress = 198.18.0.2/32\nDNS = 198.18.0.1\nMTU = 1280\n",
		base64.StdEncoding.EncodeToString(clientPriv))
	for _, p := range awgParams {
		conf += fmt.Sprintf("%s = %s\n", p.Key, p.Value)
	}
	conf += fmt.Sprintf("\n[Peer]\nPublicKey = %s\nEndpoint = 127.0.0.1:%d\nAllowedIPs = 198.18.0.0/15\nPersistentKeepalive = 25-35\n",
		base64.StdEncoding.EncodeToString(serverPub), port)
	servers, errs := profile.ParseMany(conf)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	s := servers[0]

	tunnel, err := awg.Start(s.WG, netip.MustParseAddrPort(fmt.Sprintf("127.0.0.1:%d", port)), 0, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	defer tunnel.Close()

	cfg, err := BuildConfig(s, Options{SocksPort: 21280, HTTPPort: 21281, BridgePort: tunnel.Port()})
	if err != nil {
		t.Fatal(err)
	}
	client, err := Start(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	code, body := httpVia(t, 21281, "http://198.18.0.1/")
	if code != 200 || body != "hello from awg" {
		t.Fatalf("got %d %q", code, body)
	}
	echo := socksUDPEcho(t, 21280, "198.18.0.1:7", []byte("ping-udp"))
	if string(echo) != "ping-udp" {
		t.Fatalf("udp echo %q", echo)
	}
	up, down := client.Traffic()
	t.Logf("ok: tcp+udp through AmneziaWG, up=%d down=%d", up, down)

	// Shutdown must not wait for remote peers to hang up.
	held, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", tunnel.Port()))
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	_, _ = held.Write([]byte{5, 1, 0, 5, 1, 0, 1, 198, 18, 0, 1, 0, 81})
	reply := make([]byte, 12)
	if _, err := io.ReadFull(held, reply); err != nil || reply[3] != 0 {
		t.Fatalf("socks connect to holding peer: %v %v", err, reply)
	}
	// ...nor for a dial to a host that never answers (SYN in flight).
	pending, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", tunnel.Port()))
	if err != nil {
		t.Fatal(err)
	}
	defer pending.Close()
	_, _ = pending.Write([]byte{5, 1, 0, 5, 1, 0, 1, 198, 18, 0, 250, 0, 80})
	time.Sleep(300 * time.Millisecond)

	start := time.Now()
	_ = client.Close()
	tunnel.Close()
	if d := time.Since(start); d > time.Second {
		t.Fatalf("shutdown took %v", d)
	} else {
		t.Logf("shutdown with an open connection took %v", d)
	}
}
