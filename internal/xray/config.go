// Package xray builds Xray-core JSON configs and runs Xray-core in-process.
package xray

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/feds1337/vlessia/internal/profile"
)

const (
	TagProxy  = "proxy"
	TagDirect = "direct"
	TagBlock  = "block"
	TagDNS    = "dns-out"
	TagTun    = "tun-in"
)

// Options control everything apart from the server itself.
type Options struct {
	TUN       bool
	TunName   string
	SocksPort int
	HTTPPort  int
	// Interface binds outbound sockets to the physical adapter so that
	// Xray's own traffic does not loop back into the TUN device.
	Interface string
	// ServerAddress overrides the server address (pre-resolved IP in TUN mode).
	ServerAddress string
	// BridgePort is the local SOCKS5 port of a full-tunnel protocol
	// (WireGuard/AmneziaWG/OpenVPN), which then replaces the proxy outbound.
	BridgePort int
	BypassRU   bool
	// DirectRules and ProxyRules are the user's rules (see ParseRules);
	// ProxyRules win.
	DirectRules []string
	ProxyRules  []string
	DNS         []string
	LogFile     string
	LogLevel    string
}

type obj = map[string]any

// privateCIDRs are always routed directly.
var privateCIDRs = []string{
	"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16",
	"172.16.0.0/12", "192.168.0.0/16", "224.0.0.0/4", "255.255.255.255/32",
	"::1/128", "fc00::/7", "fe80::/10", "ff00::/8",
}

// BuildConfig returns a complete Xray JSON config.
func BuildConfig(s *profile.Server, o Options) ([]byte, error) {
	sniffing := obj{"enabled": true, "destOverride": []string{"http", "tls", "quic"}, "routeOnly": true}

	inbounds := []obj{
		{
			"tag": "socks-in", "listen": "127.0.0.1", "port": o.SocksPort, "protocol": "socks",
			"settings": obj{"udp": true}, "sniffing": sniffing,
		},
		{
			"tag": "http-in", "listen": "127.0.0.1", "port": o.HTTPPort, "protocol": "http",
			"sniffing": sniffing,
		},
	}
	if o.TUN {
		// Always pass a non-empty name: Xray 26.10.21+ otherwise calls
		// net.Interfaces() to pick one, which Android blocks via SELinux
		// ("route ip+net: netlinkrib: permission denied").
		inbounds = append(inbounds, obj{
			"tag": TagTun, "port": 0, "protocol": "tun",
			"settings": obj{"name": nonEmpty(o.TunName, "tun0"), "MTU": 1500},
			"sniffing": sniffing,
		})
	}

	proxy, err := buildOutbound(s, o)
	if err != nil {
		return nil, err
	}
	directSockopt := obj{"domainStrategy": "UseIP"}
	if o.Interface != "" {
		directSockopt["interface"] = o.Interface
	}
	direct := obj{"tag": TagDirect, "protocol": "freedom", "streamSettings": obj{"sockopt": directSockopt}}
	outbounds := []obj{
		proxy,
		direct,
		{"tag": TagBlock, "protocol": "blackhole"},
		{"tag": TagDNS, "protocol": "dns"},
	}

	var rules []obj
	if o.TUN {
		// Hijack plain DNS that enters through the TUN device and resolve it
		// with Xray's DNS, whose queries go out through the proxy.
		rules = append(rules, obj{"type": "field", "inboundTag": []string{TagTun}, "port": "53", "outboundTag": TagDNS})
	}
	rules = append(rules, obj{"type": "field", "ip": privateCIDRs, "outboundTag": TagDirect})
	// The user's rules come before the .ru bypass: "through VPN" wins over
	// "direct", and both over the bypass.
	hasIPRules := false
	for _, r := range []struct {
		lines []string
		tag   string
	}{{o.ProxyRules, TagProxy}, {o.DirectRules, TagDirect}} {
		domains, ips, err := ParseRules(r.lines)
		if err != nil {
			return nil, err
		}
		if len(domains) > 0 {
			rules = append(rules, obj{"type": "field", "domain": domains, "outboundTag": r.tag})
		}
		if len(ips) > 0 {
			rules = append(rules, obj{"type": "field", "ip": ips, "outboundTag": r.tag})
			hasIPRules = true
		}
	}
	if o.BypassRU {
		rules = append(rules, obj{
			"type": "field", "outboundTag": TagDirect,
			"domain": []string{"domain:ru", "domain:su", "domain:xn--p1ai", "domain:xn--d1acj3b"},
		})
	}

	dns := o.DNS
	if len(dns) == 0 {
		dns = []string{"1.1.1.1", "8.8.8.8"}
	}

	// IP rules only match connections by IP unless Xray resolves domains
	// that no domain rule matched; do so only when there are IP rules.
	domainStrategy := "AsIs"
	if hasIPRules {
		domainStrategy = "IPIfNonMatch"
	}

	cfg := obj{
		"log":       obj{"loglevel": nonEmpty(o.LogLevel, "warning"), "access": "none", "error": o.LogFile},
		"dns":       obj{"servers": dns, "queryStrategy": "UseIPv4"},
		"inbounds":  inbounds,
		"outbounds": outbounds,
		"routing":   obj{"domainStrategy": domainStrategy, "rules": rules},
		"stats":     obj{},
		"policy": obj{"system": obj{
			"statsOutboundUplink":   true,
			"statsOutboundDownlink": true,
		}},
	}
	return json.MarshalIndent(cfg, "", "  ")
}

func buildOutbound(s *profile.Server, o Options) (obj, error) {
	if s.IsFullTunnel() {
		if o.BridgePort == 0 {
			return nil, errors.New("туннель " + s.Protocol + " не запущен")
		}
		// Loopback: no interface binding, it must not leave the machine.
		return obj{"tag": TagProxy, "protocol": "socks", "settings": obj{
			"servers": []obj{{"address": "127.0.0.1", "port": o.BridgePort}},
		}}, nil
	}

	addr := nonEmpty(o.ServerAddress, s.Address)
	out := obj{"tag": TagProxy, "protocol": s.Protocol}

	switch s.Protocol {
	case "vless":
		user := obj{"id": s.UUID, "encryption": nonEmpty(s.Encryption, "none")}
		if s.Flow != "" {
			user["flow"] = s.Flow
		}
		out["settings"] = obj{"vnext": []obj{{"address": addr, "port": s.Port, "users": []obj{user}}}}
	case "vmess":
		out["settings"] = obj{"vnext": []obj{{"address": addr, "port": s.Port, "users": []obj{{
			"id": s.UUID, "alterId": s.AlterID, "security": nonEmpty(s.Cipher, "auto"),
		}}}}}
	case "trojan":
		out["settings"] = obj{"servers": []obj{{"address": addr, "port": s.Port, "password": s.Password}}}
	case "shadowsocks":
		out["settings"] = obj{"servers": []obj{{"address": addr, "port": s.Port, "method": s.Cipher, "password": s.Password}}}
	case "hysteria2":
		out["protocol"] = "hysteria"
		out["settings"] = obj{"version": 2, "address": addr, "port": s.Port}
	case "socks", "http":
		srv := obj{"address": addr, "port": s.Port}
		if s.Username != "" {
			srv["user"] = s.Username
			srv["pass"] = s.Password
		}
		out["settings"] = srv
	default:
		return nil, fmt.Errorf("протокол %q не поддерживается", s.Protocol)
	}

	// When the address was replaced by an IP, the original host name must
	// still be presented as SNI / Host header.
	sni := s.SNI
	if sni == "" && addr != s.Address {
		sni = s.Address
	}

	stream := obj{"network": s.Network, "security": s.Security}
	if s.Protocol == "hysteria2" {
		stream = hysteriaStream(s)
	}
	switch s.Network {
	case "ws":
		stream["wsSettings"] = obj{"path": nonEmpty(s.Path, "/"), "host": s.Host}
	case "httpupgrade":
		stream["httpupgradeSettings"] = obj{"path": nonEmpty(s.Path, "/"), "host": s.Host}
	case "grpc":
		stream["grpcSettings"] = obj{"serviceName": s.ServiceName, "multiMode": s.Mode == "multi", "authority": s.Host}
	case "xhttp":
		x := obj{"path": nonEmpty(s.Path, "/"), "host": s.Host, "mode": nonEmpty(s.Mode, "auto")}
		if s.Extra != "" && json.Valid([]byte(s.Extra)) {
			x["extra"] = json.RawMessage(s.Extra)
		}
		stream["xhttpSettings"] = x
	case "tcp":
		if s.HeaderType == "http" {
			req := obj{"path": []string{nonEmpty(s.Path, "/")}}
			if s.Host != "" {
				req["headers"] = obj{"Host": strings.Split(s.Host, ",")}
			}
			stream["tcpSettings"] = obj{"header": obj{"type": "http", "request": req}}
		}
	}

	switch s.Security {
	case "tls":
		t := obj{"serverName": nonEmpty(sni, s.Host), "fingerprint": nonEmpty(s.Fingerprint, "chrome")}
		if s.ALPN != "" {
			t["alpn"] = strings.Split(s.ALPN, ",")
		}
		if s.Protocol == "hysteria2" {
			// QUIC: browser fingerprints (uTLS) do not apply.
			delete(t, "fingerprint")
			if s.ALPN == "" {
				t["alpn"] = []string{"h3"}
			}
		}
		if s.PinnedSHA != "" {
			t["pinnedPeerCertSha256"] = s.PinnedSHA
		}
		if s.VerifyName != "" {
			t["verifyPeerCertByName"] = s.VerifyName
		}
		stream["tlsSettings"] = t
	case "reality":
		stream["realitySettings"] = obj{
			"serverName":  sni,
			"fingerprint": nonEmpty(s.Fingerprint, "chrome"),
			"publicKey":   s.PublicKey,
			"shortId":     s.ShortID,
			"spiderX":     s.SpiderX,
		}
	}

	if o.Interface != "" {
		stream["sockopt"] = obj{"interface": o.Interface}
	}
	out["streamSettings"] = stream
	return out, nil
}

// hysteriaStream returns the transport part for Hysteria 2; TLS settings
// are added by the common code below.
func hysteriaStream(s *profile.Server) obj {
	stream := obj{
		"network":          "hysteria",
		"security":         "tls",
		"hysteriaSettings": obj{"version": 2, "auth": s.Password},
	}
	mask := obj{}
	if s.Obfs == "salamander" {
		mask["udp"] = []obj{{"type": "salamander", "settings": obj{"password": s.ObfsPassword}}}
	}
	quic := obj{}
	if s.HopPorts != "" {
		quic["udpHop"] = obj{"ports": s.HopPorts}
	}
	if s.UpMbps > 0 {
		// Brutal needs to know the uplink; download is controlled by the server.
		quic["congestion"] = "brutal"
		quic["brutalUp"] = fmt.Sprintf("%d mbps", s.UpMbps)
		if s.DownMbps > 0 {
			quic["brutalDown"] = fmt.Sprintf("%d mbps", s.DownMbps)
		}
	}
	if len(quic) > 0 {
		mask["quicParams"] = quic
	}
	if len(mask) > 0 {
		stream["finalmask"] = mask
	}
	return stream
}

func nonEmpty(v, def string) string {
	if v != "" {
		return v
	}
	return def
}
