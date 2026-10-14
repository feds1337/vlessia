package xray

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/feds1337/vlessia/internal/profile"
)

func TestParseRules(t *testing.T) {
	domains, ips, err := ParseRules([]string{
		"youtube.com", " *.googlevideo.com ", "https://www.Example.org/path?x=1", "сайт.рф",
		"full:exact.example.com", "keyword:tracker", "regexp:^ads\\.", "1.2.3.4", "10.0.0.0/8",
		"2001:db8::/32", "", "# comment", "vk.com # social",
	})
	if err != nil {
		t.Fatal(err)
	}
	wantDomains := []string{"domain:youtube.com", "domain:googlevideo.com", "domain:www.example.org", "domain:сайт.рф",
		"full:exact.example.com", "keyword:tracker", "regexp:^ads\\.", "domain:vk.com"}
	wantIPs := []string{"1.2.3.4", "10.0.0.0/8", "2001:db8::/32"}
	if !reflect.DeepEqual(domains, wantDomains) || !reflect.DeepEqual(ips, wantIPs) {
		t.Fatalf("domains %q\nips %q", domains, ips)
	}

	for _, bad := range []string{"not a domain", "regexp:([", "keyword:", "-bad-.com"} {
		if _, _, err := ParseRules([]string{bad}); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

func TestRulesInConfig(t *testing.T) {
	s, err := profile.ParseLink("vless://b831381d-6324-4d53-ad4f-8cda48b30811@127.0.0.1:443?type=tcp#x")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := BuildConfig(s, Options{
		SocksPort: 21480, HTTPPort: 21481, BypassRU: true,
		DirectRules: []string{"bank.example", "8.8.8.8"},
		ProxyRules:  []string{"youtube.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var c struct {
		Routing struct {
			DomainStrategy string `json:"domainStrategy"`
			Rules          []struct {
				Domain      []string `json:"domain"`
				IP          []string `json:"ip"`
				OutboundTag string   `json:"outboundTag"`
			} `json:"rules"`
		} `json:"routing"`
	}
	if err := json.Unmarshal(cfg, &c); err != nil {
		t.Fatal(err)
	}
	if c.Routing.DomainStrategy != "IPIfNonMatch" {
		t.Errorf("domainStrategy %q with IP rules", c.Routing.DomainStrategy)
	}
	var order []string
	for _, r := range c.Routing.Rules {
		order = append(order, r.OutboundTag+":"+strings.Join(append(r.Domain, r.IP...), ","))
	}
	want := []string{
		"direct:" + strings.Join(privateCIDRs, ","),
		"proxy:domain:youtube.com",
		"direct:domain:bank.example",
		"direct:8.8.8.8",
		"direct:domain:ru,domain:su,domain:xn--p1ai,domain:xn--d1acj3b",
	}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("rules:\n%s", strings.Join(order, "\n"))
	}

	// Xray accepts it.
	inst, err := Start(cfg)
	if err != nil {
		t.Fatal(err)
	}
	_ = inst.Close()

	if _, err := BuildConfig(s, Options{SocksPort: 1, HTTPPort: 2, DirectRules: []string{"no such thing"}}); err == nil {
		t.Error("an invalid rule was accepted")
	}
	plain, _ := BuildConfig(s, Options{SocksPort: 1, HTTPPort: 2, ProxyRules: []string{"a.com"}})
	if !strings.Contains(string(plain), `"domainStrategy": "AsIs"`) {
		t.Error("domain-only rules should keep AsIs")
	}
}
