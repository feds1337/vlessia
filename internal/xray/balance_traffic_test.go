package xray

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/feds1337/vlessia/internal/profile"
	"github.com/feds1337/vlessia/internal/socks"
	"github.com/xtls/xray-core/features/stats"
)

// plainNet dials the real network, so the local SOCKS server below can
// reach the test HTTP server.
type plainNet struct{}

func (plainNet) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, network, address)
}
func (plainNet) ListenUDP() (net.PacketConn, error) {
	return net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
}
func (plainNet) LookupHost(ctx context.Context, host string) ([]string, error) {
	return net.DefaultResolver.LookupHost(ctx, host)
}

// TestBalanceTraffic: behind a balancing tag the connections are handled by
// the bal-N outbounds, so there is no "proxy" outbound to read the counters
// from; Instance.Traffic must sum the group's counters instead.
func TestBalanceTraffic(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	}))
	defer target.Close()

	cfg := []byte(`{
		"log": {"loglevel": "warning", "access": "none"},
		"inbounds": [{"tag": "http-in", "listen": "127.0.0.1", "port": 21380, "protocol": "http"}],
		"outbounds": [
			{"tag": "bal-1", "protocol": "freedom"},
			{"tag": "bal-2", "protocol": "freedom"}
		],
		"routing": {
			"rules": [{"type": "field", "network": "tcp,udp", "balancerTag": "proxy"}],
			"balancers": [{"tag": "proxy", "selector": ["bal-1", "bal-2"], "strategy": {"type": "roundrobin"}}]
		},
		"stats": {},
		"policy": {"system": {"statsOutboundUplink": true, "statsOutboundDownlink": true}}
	}`)
	inst, err := Start(cfg)
	if err != nil {
		t.Fatalf("xray rejected the balancing config: %v", err)
	}
	defer inst.Close()

	proxy, _ := url.Parse("http://127.0.0.1:21380")
	hc := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(proxy)},
		Timeout:   10 * time.Second,
	}
	// Several requests, so both balanced outbounds get a connection.
	for i := 0; i < 4; i++ {
		resp, err := hc.Get(target.URL)
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		resp.Body.Close()
	}

	up, down := inst.Traffic()
	if up == 0 || down == 0 {
		t.Fatalf("traffic counters not updated: up=%d down=%d", up, down)
	}
	t.Logf("ok, up=%d down=%d", up, down)
}

// TestBalanceEndToEnd checks the config BuildConfig really produces: new
// connections must reach the target through the balancer, and the traffic
// must be counted. A balancing tag in outboundTag (the shape this test
// guards against) makes Xray answer "non existing outTag: proxy", so every
// connection fails.
//
// The outbounds are SOCKS ones pointing at a local SOCKS server: Xray's
// freedom blocks private destinations, so a local VLESS server cannot reach
// a test target on loopback.
func TestBalanceEndToEnd(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	}))
	defer target.Close()
	_, port, err := net.SplitHostPort(target.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	local, err := socks.Listen(plainNet{}, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	defer local.Close()

	link := fmt.Sprintf("socks://127.0.0.1:%d#local", local.Port())
	first, err := profile.ParseLink(link)
	if err != nil {
		t.Fatal(err)
	}
	second, err := profile.ParseLink(link)
	if err != nil {
		t.Fatal(err)
	}
	// Two distinct servers of the same "subscription" to balance between.
	first.ID, second.ID = "first", "second"

	cfg, err := BuildConfig(first, Options{
		SocksPort: 21390, HTTPPort: 21391,
		Balance: []*profile.Server{first, second},
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := Start(cfg)
	if err != nil {
		t.Fatalf("xray rejected the generated balancing config: %v\n%s", err, cfg)
	}
	defer client.Close()

	proxy, _ := url.Parse("http://127.0.0.1:21391")
	hc := &http.Client{
		// A fresh connection per request, so the balancer really chooses
		// an outbound each time.
		Transport: &http.Transport{Proxy: http.ProxyURL(proxy), DisableKeepAlives: true},
		Timeout:   10 * time.Second,
	}
	// A domain, not an IP: an IP target in a private range is routed
	// directly by the built-in rule and would never reach the balancer.
	requestURL := "http://localhost:" + port + "/"
	for i := 0; i < 4; i++ {
		resp, err := hc.Get(requestURL)
		if err != nil {
			t.Fatalf("request %d through the balancer: %v", i, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("request %d: status %d", i, resp.StatusCode)
		}
	}
	// Round-robin: both members of the group must have handled something.
	if m, ok := client.core.GetFeature(stats.ManagerType()).(stats.Manager); ok {
		hit := 0
		for n := 1; n <= 2; n++ {
			tag := fmt.Sprintf("%s-%d", TagBalance, n)
			if c := m.GetCounter("outbound>>>" + tag + ">>>traffic>>>uplink"); c != nil && c.Value() > 0 {
				hit++
			}
		}
		if hit != 2 {
			t.Fatalf("round-robin used %d of the 2 servers", hit)
		}
	} else {
		t.Fatal("no stats manager")
	}
	up, down := client.Traffic()
	if up == 0 || down == 0 {
		t.Fatalf("traffic counters not updated: up=%d down=%d", up, down)
	}
	t.Logf("ok, up=%d down=%d", up, down)
}
