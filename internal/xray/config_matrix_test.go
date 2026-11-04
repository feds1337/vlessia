package xray

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/feds1337/vlessia/internal/profile"
)

// TestConfigMatrix builds a config for every combination of the settings
// that change its shape and checks two things: Xray itself accepts it, and
// every routing rule points at something that exists (an outbound or a
// balancer). Both bugs of 0.5.0 were exactly of this kind: the JSON looked
// right but Xray rejected or silently ignored it.
func TestConfigMatrix(t *testing.T) {
	linkSet := map[string]string{
		"vless-reality": links[0],
		"vless-xhttp":   links[1],
		"vmess-ws":      links[7],
		"trojan":        links[4],
		"shadowsocks":   links[6],
		"hysteria2":     links[8],
		"socks":         "socks://user:pass@1.2.3.4:1080#socks",
		"http":          "http://user:pass@1.2.3.4:8080#http",
	}
	awg, err := profile.ParseMany(awgConf)
	if err != nil || len(awg) == 0 {
		t.Fatalf("awg sample: %v", err)
	}
	fullTunnel := map[string]*profile.Server{
		"amneziawg": awg[0],
		"openvpn": {
			Name: "ovpn", Protocol: "openvpn", Address: "1.2.3.4", Port: 1194, Security: "tls",
			Network: "udp", OVPN: &profile.OpenVPN{Config: "client\nremote 1.2.3.4 1194\n"},
		},
	}

	variants := []struct {
		name string
		opts Options
	}{
		{"plain", Options{}},
		{"tun", Options{TUN: true, TunName: "Vlessia", Interface: "Ethernet", ServerAddress: "9.9.9.9"}},
		{"rules", Options{DirectRules: []string{"bank.example", "8.8.8.8", "10.0.0.0/8"}, ProxyRules: []string{"youtube.com", "1.1.1.1"}, BypassRU: true}},
		{"tun+rules", Options{TUN: true, TunName: "Vlessia", Interface: "Ethernet", DirectRules: []string{"bank.example"}, ProxyRules: []string{"youtube.com"}, BypassRU: true}},
		{"apps", Options{DirectApps: []string{"Telegram.exe"}, ProxyApps: []string{"chrome.exe", "C:\\Program Files\\Firefox\\firefox.exe", "C:\\Games\\"}}},
	}

	for name, link := range linkSet {
		srv, err := profile.ParseLink(link)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		relay, err := profile.ParseLink(links[1])
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range variants {
			for _, chain := range []bool{false, true} {
				for _, balance := range []string{"", "roundrobin", "leastping"} {
					o := v.opts
					o.SocksPort, o.HTTPPort = 10808, 10809
					label := fmt.Sprintf("%s/%s/chain=%v/balance=%q", name, v.name, chain, balance)
					if chain {
						o.Via, o.ViaAddr = relay, "5.5.5.5"
					}
					if balance != "" {
						if len(o.ProxyApps) > 0 {
							// Keep the matrix small: apps and balancing are
							// covered apart.
							continue
						}
						o.Balance = []*profile.Server{srv, relay}
						o.BalanceAddrs = []string{"1.1.1.1", "2.2.2.2"}
						o.BalanceStrategy = balance
					}
					cfg, err := BuildConfig(srv, o)
					if err != nil {
						t.Fatalf("%s: %v", label, err)
					}
					if _, err := loadConfig(cfg); err != nil {
						t.Fatalf("%s: xray rejected the config: %v\n%s", label, err, cfg)
					}
					checkConfigRefs(t, label, cfg)
				}
			}
		}
	}

	for name, srv := range fullTunnel {
		for _, tun := range []bool{false, true} {
			o := Options{SocksPort: 10808, HTTPPort: 10809, BridgePort: 31000, Interface: "Ethernet"}
			if tun {
				o.TUN, o.TunName, o.ServerAddress = true, "Vlessia", "9.9.9.9"
			}
			cfg, err := BuildConfig(srv, o)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if _, err := loadConfig(cfg); err != nil {
				t.Fatalf("%s (tun=%v): xray rejected the config: %v\n%s", name, tun, err, cfg)
			}
			checkConfigRefs(t, name, cfg)
		}
	}

	for _, tun := range []bool{false, true} {
		cfg, err := BuildConfig(nil, Options{Block: true, TUN: tun, TunName: "Vlessia", SocksPort: 10808, HTTPPort: 10809})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := loadConfig(cfg); err != nil {
			t.Fatalf("block (tun=%v): xray rejected the config: %v\n%s", tun, err, cfg)
		}
		checkConfigRefs(t, "block", cfg)
	}
}

// checkConfigRefs verifies the routing references: every outboundTag names
// an outbound, every balancerTag names a balancer, and every balancer
// selector matches at least one outbound (the selector is a prefix match,
// so "bal" would match "bal-1", "bal-2", ...).
func checkConfigRefs(t *testing.T, label string, cfg []byte) {
	t.Helper()
	var parsed struct {
		Outbounds []struct {
			Tag string `json:"tag"`
		} `json:"outbounds"`
		Routing struct {
			Rules []struct {
				OutboundTag string `json:"outboundTag"`
				BalancerTag string `json:"balancerTag"`
			} `json:"rules"`
			Balancers []struct {
				Tag      string   `json:"tag"`
				Selector []string `json:"selector"`
			} `json:"balancers"`
		} `json:"routing"`
	}
	if err := json.Unmarshal(cfg, &parsed); err != nil {
		t.Fatalf("%s: %v", label, err)
	}
	outbounds := map[string]bool{}
	for _, o := range parsed.Outbounds {
		outbounds[o.Tag] = true
	}
	balancers := map[string]bool{}
	for _, b := range parsed.Routing.Balancers {
		balancers[b.Tag] = true
		matched := false
		for tag := range outbounds {
			for _, sel := range b.Selector {
				if strings.HasPrefix(tag, sel) {
					matched = true
				}
			}
		}
		if !matched {
			t.Fatalf("%s: balancer %q selects nothing: %v", label, b.Tag, b.Selector)
		}
	}
	for _, r := range parsed.Routing.Rules {
		if r.OutboundTag != "" && !outbounds[r.OutboundTag] {
			t.Fatalf("%s: a rule points at a missing outbound %q", label, r.OutboundTag)
		}
		if r.BalancerTag != "" && !balancers[r.BalancerTag] {
			t.Fatalf("%s: a rule points at a missing balancer %q", label, r.BalancerTag)
		}
		if r.OutboundTag != "" && r.BalancerTag != "" {
			t.Fatalf("%s: a rule has both outboundTag and balancerTag", label)
		}
	}
}
