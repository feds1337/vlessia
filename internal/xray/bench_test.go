package xray

import (
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"testing"
	"time"

	"golang.org/x/net/proxy"

	"github.com/feds1337/vlessia/internal/awg"
	"github.com/feds1337/vlessia/internal/profile"
)

// payload is served by the benchmark HTTP servers.
const payloadSize = 32 << 20

func payloadHandler() http.Handler {
	chunk := make([]byte, 64<<10)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(payloadSize))
		for sent := 0; sent < payloadSize; sent += len(chunk) {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	})
}

func download(b *testing.B, httpPort int, target string) {
	proxy, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", httpPort))
	hc := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxy), DisableKeepAlives: true}, Timeout: 2 * time.Minute}
	b.SetBytes(payloadSize)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := hc.Get(target)
		if err != nil {
			b.Fatal(err)
		}
		n, err := io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if err != nil || n != payloadSize {
			b.Fatalf("got %d bytes: %v", n, err)
		}
	}
}

// BenchmarkVLESS downloads through the client's HTTP inbound -> VLESS ->
// local server. localtest.me resolves to 127.0.0.1 but is a domain, so the
// "private IPs direct" rule does not short-circuit the proxy.
func BenchmarkVLESS(b *testing.B) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	go http.Serve(ln, payloadHandler())
	webPort := ln.Addr().(*net.TCPAddr).Port

	const uuid = "b831381d-6324-4d53-ad4f-8cda48b30811"
	server, err := Start([]byte(`{"log":{"loglevel":"error"},"inbounds":[{"listen":"127.0.0.1","port":24601,"protocol":"vless",
		"settings":{"clients":[{"id":"` + uuid + `"}],"decryption":"none"}}],"outbounds":[{"protocol":"freedom"}]}`))
	if err != nil {
		b.Fatal(err)
	}
	defer server.Close()
	s, _ := profile.ParseLink("vless://" + uuid + "@127.0.0.1:24601?type=tcp#bench")
	cfg, _ := BuildConfig(s, Options{SocksPort: 24610, HTTPPort: 24611, LogLevel: "error"})
	client, err := Start(cfg)
	if err != nil {
		b.Fatal(err)
	}
	defer client.Close()
	download(b, 24611, fmt.Sprintf("http://bench.localtest.me:%d/", webPort))
}

// BenchmarkAmneziaWG downloads through xray -> SOCKS bridge -> AmneziaWG
// (userspace) -> server netstack, i.e. the path full-tunnel protocols use.
func BenchmarkAmneziaWG(b *testing.B) {
	serverPriv, serverPub := wgKeypair()
	clientPriv, clientPub := wgKeypair()
	port := freeUDPPort(b)
	stop := startAWGServerWith(b, serverPriv, clientPub, port, payloadHandler())
	defer stop()

	wg := &profile.WireGuard{
		PrivateKey: base64.StdEncoding.EncodeToString(clientPriv), Addresses: []string{"198.18.0.2/32"},
		PublicKey: base64.StdEncoding.EncodeToString(serverPub), AllowedIPs: []string{"198.18.0.0/15"},
		AWG: awgParams, MTU: 1280,
	}
	tunnel, err := awg.Start(wg, netip.MustParseAddrPort(fmt.Sprintf("127.0.0.1:%d", port)), 0, b.Logf)
	if err != nil {
		b.Fatal(err)
	}
	defer tunnel.Close()
	s := &profile.Server{Protocol: "amneziawg", WG: wg, Address: "127.0.0.1", Port: port}
	cfg, _ := BuildConfig(s, Options{SocksPort: 24620, HTTPPort: 24621, BridgePort: tunnel.Port(), LogLevel: "error"})
	client, err := Start(cfg)
	if err != nil {
		b.Fatal(err)
	}
	defer client.Close()
	download(b, 24621, "http://198.18.0.1/")
}

// BenchmarkAmneziaWGBridgeOnly skips xray: SOCKS bridge -> AmneziaWG, to
// tell bridge/tunnel costs apart from xray's.
func BenchmarkAmneziaWGBridgeOnly(b *testing.B) {
	serverPriv, serverPub := wgKeypair()
	clientPriv, clientPub := wgKeypair()
	port := freeUDPPort(b)
	stop := startAWGServerWith(b, serverPriv, clientPub, port, payloadHandler())
	defer stop()
	wg := &profile.WireGuard{
		PrivateKey: base64.StdEncoding.EncodeToString(clientPriv), Addresses: []string{"198.18.0.2/32"},
		PublicKey: base64.StdEncoding.EncodeToString(serverPub), AllowedIPs: []string{"198.18.0.0/15"},
		AWG: awgParams, MTU: 1280,
	}
	tunnel, err := awg.Start(wg, netip.MustParseAddrPort(fmt.Sprintf("127.0.0.1:%d", port)), 0, b.Logf)
	if err != nil {
		b.Fatal(err)
	}
	defer tunnel.Close()
	dialer, _ := proxy.SOCKS5("tcp", fmt.Sprintf("127.0.0.1:%d", tunnel.Port()), nil, proxy.Direct)
	hc := &http.Client{Transport: &http.Transport{Dial: dialer.Dial, DisableKeepAlives: true}, Timeout: 2 * time.Minute}
	b.SetBytes(payloadSize)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := hc.Get("http://198.18.0.1/")
		if err != nil {
			b.Fatal(err)
		}
		n, _ := io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if n != payloadSize {
			b.Fatalf("got %d", n)
		}
	}
}
