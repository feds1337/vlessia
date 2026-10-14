package profile

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/n0madic/go-openvpn/pkg/ovpn"
)

// ---------- OpenVPN ----------

// IsOpenVPNConf reports whether text looks like an .ovpn client profile.
func IsOpenVPNConf(text string) bool {
	hasRemote, hasClient := false, false
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		switch strings.ToLower(f[0]) {
		case "remote":
			hasRemote = true
		case "client", "tls-client":
			hasClient = true
		}
	}
	return hasRemote && hasClient
}

// ParseOpenVPNConf validates an .ovpn profile and returns a server for it.
func ParseOpenVPNConf(text, name string) (*Server, error) {
	p, err := ParseOpenVPNProfile(text, "", "")
	if err != nil {
		return nil, err
	}
	host, portStr, err := net.SplitHostPort(p.Config.RemoteAddr)
	if err != nil {
		return nil, OpenVPNError(err)
	}
	port, _ := strconv.Atoi(portStr)
	network := strings.TrimRight(p.Config.Network, "46")
	if name == "" {
		name = host
	}
	return &Server{
		Protocol: "openvpn",
		Name:     name,
		Address:  host,
		Port:     port,
		Network:  network,
		Security: "tls",
		OVPN: &OpenVPN{
			Config:   text,
			NeedAuth: p.AuthUserPass && p.Config.Username == "",
		},
	}, nil
}

// ParseOpenVPNProfile parses an .ovpn profile with the given credentials.
func ParseOpenVPNProfile(text, user, pass string) (*ovpn.Parsed, error) {
	p, err := ovpn.Parse(strings.NewReader(text), &ovpn.ParseOptions{
		Username: user,
		Password: pass,
		// The official client accepts profiles that only pin the CA (plus
		// remote-cert-tls); most provider profiles are like that.
		AllowNoServerIdentity: true,
	})
	if err != nil {
		return nil, OpenVPNError(err)
	}
	return p, nil
}

// OpenVPNError turns go-openvpn policy errors into actionable messages.
func OpenVPNError(err error) error {
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "comp-lzo"), strings.Contains(msg, "compress"):
		return fmt.Errorf("OpenVPN: профиль использует сжатие (comp-lzo/compress), оно не поддерживается: %w", err)
	case strings.Contains(msg, "cipher"):
		return fmt.Errorf("OpenVPN: поддерживаются только шифры AES-GCM и CHACHA20-POLY1305: %w", err)
	case strings.Contains(msg, "control-channel protection"):
		return fmt.Errorf("OpenVPN: профиль без tls-auth / tls-crypt не поддерживается: %w", err)
	case strings.Contains(msg, "dev tap"):
		return fmt.Errorf("OpenVPN: режим dev tap не поддерживается, нужен dev tun: %w", err)
	}
	return fmt.Errorf("OpenVPN: %w", err)
}

// ---------- wireguard:// ----------

// parseWireGuardLink parses wireguard:// and wg:// links as used by
// v2rayN, sing-box and Hiddify subscriptions:
// wireguard://PRIVATEKEY@host:port?publickey=..&address=10.0.0.2/32&mtu=1280#name
// AmneziaWG parameters may be passed in the query (jc, jmin, s1, h1, ...).
func parseWireGuardLink(link string) (*Server, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	host, port, err := splitHostPort(u)
	if err != nil {
		return nil, err
	}
	q := map[string]string{}
	for k, v := range u.Query() {
		if len(v) > 0 {
			q[strings.ToLower(k)] = v[0]
		}
	}
	first := func(keys ...string) string {
		for _, k := range keys {
			if v := q[k]; v != "" {
				return v
			}
		}
		return ""
	}

	wg := &WireGuard{
		PrivateKey:          u.User.Username(),
		PublicKey:           first("publickey", "peer_public_key", "pbk"),
		PresharedKey:        first("presharedkey", "pre_shared_key", "psk"),
		PersistentKeepalive: first("keepalive", "persistentkeepalive"),
	}
	if wg.PrivateKey == "" {
		wg.PrivateKey = first("privatekey", "secretkey", "private_key")
	}
	// Keys are base64 and may arrive with '+' decoded as ' '.
	for _, k := range []*string{&wg.PrivateKey, &wg.PublicKey, &wg.PresharedKey} {
		*k = strings.ReplaceAll(*k, " ", "+")
	}
	wg.Addresses = splitList(first("address", "ip", "local_address", "localaddress"))
	wg.DNS = splitList(first("dns"))
	wg.AllowedIPs = splitList(first("allowedips", "allowed_ips"))
	wg.MTU, _ = strconv.Atoi(first("mtu"))

	known := map[string]bool{}
	for _, k := range []string{"publickey", "peer_public_key", "pbk", "presharedkey", "pre_shared_key", "psk",
		"keepalive", "persistentkeepalive", "privatekey", "secretkey", "private_key", "address", "ip",
		"local_address", "localaddress", "dns", "allowedips", "allowed_ips", "mtu", "reserved", "type", "flow"} {
		known[k] = true
	}
	for _, k := range sortedKeys(q) {
		if !known[k] {
			wg.AWG = append(wg.AWG, Param{Key: k, Value: q[k]})
		}
	}

	conf := &strings.Builder{}
	fmt.Fprintf(conf, "[Interface]\nPrivateKey = %s\nAddress = %s\n", wg.PrivateKey, strings.Join(wg.Addresses, ", "))
	fmt.Fprintf(conf, "[Peer]\nPublicKey = %s\nEndpoint = %s\n", wg.PublicKey, net.JoinHostPort(host, strconv.Itoa(port)))
	if wg.PresharedKey != "" {
		fmt.Fprintf(conf, "PresharedKey = %s\n", wg.PresharedKey)
	}
	// Reuse the .conf validation (keys, addresses).
	s, err := ParseWireGuardConf(conf.String(), u.Fragment)
	if err != nil {
		return nil, err
	}
	if len(wg.AllowedIPs) == 0 {
		wg.AllowedIPs = s.WG.AllowedIPs
	}
	s.WG = wg
	if len(wg.AWG) > 0 {
		s.Protocol = "amneziawg"
	}
	return s, nil
}

// ---------- SOCKS5 / HTTP proxies ----------

// parseProxyLink parses socks://, socks5://, socks5h://, http:// and
// https:// proxy links. v2rayN encodes socks credentials as
// base64(user:pass) in the user part.
func parseProxyLink(link string) (*Server, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	host, port, err := splitHostPort(u)
	if err != nil {
		return nil, err
	}
	s := &Server{Name: u.Fragment, Address: host, Port: port, Network: "tcp", Security: "none"}
	switch strings.ToLower(u.Scheme) {
	case "socks", "socks5", "socks5h":
		s.Protocol = "socks"
	case "http":
		s.Protocol = "http"
	case "https":
		s.Protocol = "http"
		s.Security = "tls"
		s.SNI = host
	}
	if u.User != nil {
		s.Username = u.User.Username()
		s.Password, _ = u.User.Password()
		if _, hasPass := u.User.Password(); !hasPass && s.Protocol == "socks" {
			if dec, err := decodeBase64(s.Username); err == nil {
				if user, pass, ok := strings.Cut(string(dec), ":"); ok {
					s.Username, s.Password = user, pass
				}
			}
		}
	}
	return s, nil
}

// IsProxyURL reports whether an http(s) URL is a proxy address rather than
// a subscription: proxies have no path or query, subscriptions do.
func IsProxyURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	return (u.Path == "" || u.Path == "/") && u.RawQuery == "" && (u.Port() != "" || u.User != nil)
}

// ---------- Outline ----------

// OutlineKeyURL converts an ssconf:// dynamic access key to the HTTPS URL
// it is fetched from.
func OutlineKeyURL(link string) (string, string, error) {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil || !strings.EqualFold(u.Scheme, "ssconf") {
		return "", "", errors.New("неверный ключ ssconf://")
	}
	name := u.Fragment
	u.Scheme, u.Fragment = "https", ""
	return u.String(), name, nil
}

// ParseOutlineConfig parses the body served for an Outline dynamic key:
// either a JSON object or a plain ss:// link.
func ParseOutlineConfig(body, name string) (*Server, error) {
	body = strings.TrimSpace(body)
	if strings.HasPrefix(body, "ss://") {
		s, err := ParseLink(body)
		if err == nil && name != "" {
			s.Name = name
		}
		return s, err
	}
	var c struct {
		Server     string          `json:"server"`
		ServerPort json.RawMessage `json:"server_port"`
		Password   string          `json:"password"`
		Method     string          `json:"method"`
		Prefix     string          `json:"prefix"`
		Error      *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &c); err != nil {
		return nil, errors.New("Outline: ответ сервера ключей не распознан")
	}
	if c.Error != nil {
		return nil, fmt.Errorf("Outline: %s", c.Error.Message)
	}
	port, _ := strconv.Atoi(strings.Trim(string(c.ServerPort), `"`))
	s := &Server{
		Protocol: "shadowsocks",
		Name:     name,
		Address:  c.Server,
		Port:     port,
		Cipher:   c.Method,
		Password: c.Password,
		Prefix:   c.Prefix,
	}
	return finalize(s, "")
}
