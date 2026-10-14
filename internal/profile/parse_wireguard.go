package profile

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"strconv"
	"strings"
)

// IsWireGuardConf reports whether text looks like a wg-quick / AmneziaWG
// .conf file.
func IsWireGuardConf(text string) bool {
	return strings.Contains(strings.ToLower(text), "[interface]")
}

// wgQuickKeys are standard [Interface] keys that are not AmneziaWG
// parameters. Everything else in [Interface] is kept as an AmneziaWG
// parameter; the tunnel decides which ones it can apply.
var wgQuickKeys = map[string]bool{
	"listenport": true, "fwmark": true, "table": true, "saveconfig": true,
	"preup": true, "postup": true, "predown": true, "postdown": true,
}

// ParseWireGuardConf parses a WireGuard or AmneziaWG client config. name is
// used when the config does not carry one.
func ParseWireGuardConf(text, name string) (*Server, error) {
	wg := &WireGuard{}
	var endpoint, section string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			section = strings.ToLower(strings.Trim(line, "[] "))
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(k))
		v = strings.TrimSpace(v)
		switch section {
		case "interface":
			switch key {
			case "privatekey":
				wg.PrivateKey = v
			case "address":
				wg.Addresses = append(wg.Addresses, splitList(v)...)
			case "dns":
				wg.DNS = append(wg.DNS, splitList(v)...)
			case "mtu":
				wg.MTU, _ = strconv.Atoi(v)
			default:
				if !wgQuickKeys[key] && v != "" {
					wg.AWG = append(wg.AWG, Param{Key: strings.TrimSpace(k), Value: v})
				}
			}
		case "peer":
			if wg.PublicKey != "" && key == "publickey" {
				// Only the first peer is used; clients normally have one.
				section = "ignored-peer"
				continue
			}
			switch key {
			case "publickey":
				wg.PublicKey = v
			case "presharedkey":
				wg.PresharedKey = v
			case "endpoint":
				endpoint = v
			case "allowedips":
				wg.AllowedIPs = append(wg.AllowedIPs, splitList(v)...)
			case "persistentkeepalive":
				wg.PersistentKeepalive = v
			}
		}
	}

	for _, k := range []struct{ name, val string }{{"PrivateKey", wg.PrivateKey}, {"PublicKey", wg.PublicKey}, {"PresharedKey", wg.PresharedKey}} {
		if k.val == "" && k.name == "PresharedKey" {
			continue
		}
		if b, err := base64.StdEncoding.DecodeString(k.val); err != nil || len(b) != 32 {
			return nil, fmt.Errorf("WireGuard: неверный %s", k.name)
		}
	}
	if len(wg.Addresses) == 0 {
		return nil, errors.New("WireGuard: нет Address в [Interface]")
	}
	if endpoint == "" {
		return nil, errors.New("WireGuard: нет Endpoint в [Peer]")
	}
	host, portStr, err := net.SplitHostPort(endpoint)
	if err != nil {
		return nil, errors.New("WireGuard: неверный Endpoint")
	}
	port, _ := strconv.Atoi(portStr)
	if len(wg.AllowedIPs) == 0 {
		wg.AllowedIPs = []string{"0.0.0.0/0", "::/0"}
	}

	s := &Server{
		Protocol: "wireguard",
		Name:     name,
		Address:  host,
		Port:     port,
		Network:  "udp",
		Security: "none",
		WG:       wg,
	}
	if len(wg.AWG) > 0 {
		s.Protocol = "amneziawg"
	}
	return s, nil
}

func splitList(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// parseAmneziaVPN parses vpn:// links exported by the AmneziaVPN app: a
// base64url payload compressed with Qt's qCompress (4-byte big-endian length
// followed by a zlib stream) holding a JSON description of the server.
func parseAmneziaVPN(link string) (*Server, error) {
	_, payload, _ := strings.Cut(link, "://")
	raw, err := decodeBase64(payload)
	if err != nil {
		return nil, errors.New("vpn://: неверный base64")
	}
	data, err := qUncompress(raw)
	if err != nil {
		return nil, fmt.Errorf("vpn://: не удалось распаковать: %w", err)
	}

	var cfg struct {
		Containers       []map[string]json.RawMessage `json:"containers"`
		DefaultContainer string                       `json:"defaultContainer"`
		Description      string                       `json:"description"`
		HostName         string                       `json:"hostName"`
		DNS1             string                       `json:"dns1"`
		DNS2             string                       `json:"dns2"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, errors.New("vpn://: неверный JSON")
	}
	name := cfg.Description
	if name == "" {
		name = cfg.HostName
	}
	dns1, dns2 := cfg.DNS1, cfg.DNS2
	if dns1 == "" {
		dns1 = "1.1.1.1"
	}
	if dns2 == "" {
		dns2 = "1.0.0.1"
	}

	// Try the default container first, then the rest.
	containerID := func(c map[string]json.RawMessage) string {
		var id string
		_ = json.Unmarshal(c["container"], &id)
		return id
	}
	containers := cfg.Containers
	sort.SliceStable(containers, func(i, j int) bool {
		return containerID(containers[i]) == cfg.DefaultContainer && containerID(containers[j]) != cfg.DefaultContainer
	})

	var found []string
	for _, c := range containers {
		found = append(found, containerID(c))
		for key, v := range c {
			if key == "container" {
				continue
			}
			var proto struct {
				LastConfig string `json:"last_config"`
			}
			if json.Unmarshal(v, &proto) != nil || proto.LastConfig == "" {
				continue
			}
			var last struct {
				Config string          `json:"config"`
				MTU    json.RawMessage `json:"mtu"` // number or string
			}
			if json.Unmarshal([]byte(proto.LastConfig), &last) != nil || !IsWireGuardConf(last.Config) {
				continue
			}
			conf := strings.NewReplacer("$PRIMARY_DNS", dns1, "$SECONDARY_DNS", dns2).Replace(last.Config)
			s, err := ParseWireGuardConf(conf, name)
			if err == nil && s.WG.MTU == 0 {
				// AmneziaVPN keeps the MTU next to the config, not in it.
				s.WG.MTU, _ = strconv.Atoi(strings.Trim(string(last.MTU), `"`))
			}
			return s, err
		}
	}
	return nil, fmt.Errorf("vpn://: нет конфигурации AmneziaWG/WireGuard (в ссылке: %s)", strings.Join(found, ", "))
}

func qUncompress(raw []byte) ([]byte, error) {
	if len(raw) > 0 && raw[0] == '{' {
		return raw, nil // older exports are not compressed
	}
	if len(raw) < 5 {
		return nil, errors.New("слишком короткие данные")
	}
	size := binary.BigEndian.Uint32(raw[:4])
	r, err := zlib.NewReader(bytes.NewReader(raw[4:]))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(io.LimitReader(r, int64(min(size, 16<<20))+1))
}
