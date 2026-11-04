package xray

import (
	"encoding/json"
	"testing"

	"github.com/feds1337/vlessia/internal/profile"
)

type parsedConfig struct {
	Outbounds []struct {
		Tag string `json:"tag"`
	} `json:"outbounds"`
	Routing struct {
		Rules []struct {
			Process     []string `json:"process"`
			OutboundTag string   `json:"outboundTag"`
			BalancerTag string   `json:"balancerTag"`
			Domain      []string `json:"domain"`
		} `json:"rules"`
		Balancers []struct {
			Tag         string   `json:"tag"`
			Selector    []string `json:"selector"`
			FallbackTag string   `json:"fallbackTag"`
			Strategy    struct {
				Type string `json:"type"`
			} `json:"strategy"`
		} `json:"balancers"`
	} `json:"routing"`
	BurstObservatory *struct {
		SubjectSelector []string `json:"subjectSelector"`
		PingConfig      struct {
			Destination string `json:"destination"`
			Interval    string `json:"interval"`
			Sampling    int    `json:"sampling"`
		} `json:"pingConfig"`
	} `json:"burstObservatory"`
}

func parseConfig(t *testing.T, cfg []byte) parsedConfig {
	t.Helper()
	var p parsedConfig
	if err := json.Unmarshal(cfg, &p); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestBalanceLeastPing: the leastping strategy needs the burst observatory
// and a fallback outbound — without health data the strategy returns no tag
// at all and every connection would fail.
func TestBalanceLeastPing(t *testing.T) {
	a, _ := profile.ParseLink(links[0])
	b, _ := profile.ParseLink(links[1])
	cfg, err := BuildConfig(a, Options{
		SocksPort: 10808, HTTPPort: 10809,
		Balance: []*profile.Server{a, b}, BalanceAddrs: []string{"1.1.1.1", "2.2.2.2"},
		BalanceStrategy: "leastping",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(cfg); err != nil {
		t.Fatalf("xray rejected the config: %v\n%s", err, cfg)
	}
	p := parseConfig(t, cfg)
	bal := p.Routing.Balancers[0]
	if bal.Strategy.Type != "leastping" {
		t.Fatalf("strategy = %q", bal.Strategy.Type)
	}
	if bal.FallbackTag != TagBalance+"-1" {
		t.Fatalf("fallback = %q: without it the first connections fail", bal.FallbackTag)
	}
	if p.BurstObservatory == nil {
		t.Fatal("leastping without a burstObservatory would never get health data")
	}
	if len(p.BurstObservatory.SubjectSelector) != 1 || p.BurstObservatory.SubjectSelector[0] != TagBalance {
		t.Fatalf("subjectSelector = %v", p.BurstObservatory.SubjectSelector)
	}
	if p.BurstObservatory.PingConfig.Destination == "" || p.BurstObservatory.PingConfig.Interval == "" {
		t.Fatalf("ping config = %+v", p.BurstObservatory.PingConfig)
	}

	// roundrobin must not drag the observatory in.
	cfg, err = BuildConfig(a, Options{
		SocksPort: 10808, HTTPPort: 10809,
		Balance: []*profile.Server{a, b}, BalanceAddrs: []string{"1.1.1.1", "2.2.2.2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(cfg); err != nil {
		t.Fatalf("xray rejected the roundrobin config: %v\n%s", err, cfg)
	}
	p = parseConfig(t, cfg)
	if p.BurstObservatory != nil {
		t.Fatal("roundrobin does not need the observatory")
	}
	if p.Routing.Balancers[0].Strategy.Type != "roundrobin" {
		t.Fatalf("strategy = %q", p.Routing.Balancers[0].Strategy.Type)
	}
}

// TestAppRules: application rules come first, both are emitted with the
// right tags, and the through-VPN one follows the balancer when balancing.
func TestAppRules(t *testing.T) {
	a, _ := profile.ParseLink(links[0])
	b, _ := profile.ParseLink(links[1])

	cfg, err := BuildConfig(a, Options{
		SocksPort: 10808, HTTPPort: 10809, TUN: true, TunName: "Vlessia",
		ProxyApps:  []string{"chrome.exe, firefox.exe", "C:\\Games\\"},
		DirectApps: []string{"Telegram.exe"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(cfg); err != nil {
		t.Fatalf("xray rejected the config: %v\n%s", err, cfg)
	}
	p := parseConfig(t, cfg)
	var proxyApp, directApp bool
	for i, r := range p.Routing.Rules {
		if len(r.Process) == 0 {
			continue
		}
		if i > 2 {
			t.Fatalf("application rules must come first, found one at position %d", i)
		}
		if r.OutboundTag == TagProxy {
			proxyApp = true
			if len(r.Process) != 3 || r.Process[0] != "chrome.exe" || r.Process[1] != "firefox.exe" || r.Process[2] != "C:\\Games\\" {
				t.Fatalf("proxy apps = %v", r.Process)
			}
		}
		if r.OutboundTag == TagDirect && r.Process[0] == "Telegram.exe" {
			directApp = true
		}
	}
	if !proxyApp || !directApp {
		t.Fatalf("application rules: proxy=%v direct=%v", proxyApp, directApp)
	}

	// The same rules with balancing: the proxy one must use balancerTag.
	cfg, err = BuildConfig(a, Options{
		SocksPort: 10808, HTTPPort: 10809,
		ProxyApps: []string{"chrome.exe"},
		Balance:   []*profile.Server{a, b}, BalanceAddrs: []string{"1.1.1.1", "2.2.2.2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(cfg); err != nil {
		t.Fatalf("xray rejected the balanced config: %v\n%s", err, cfg)
	}
	p = parseConfig(t, cfg)
	for _, r := range p.Routing.Rules {
		if len(r.Process) > 0 && r.BalancerTag != TagProxy {
			t.Fatalf("app rule must use the balancer: %+v", r)
		}
	}
}
