package profile

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// NewID returns a random identifier for servers and subscriptions.
func NewID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ParseLink parses a single share link.
func ParseLink(link string) (*Server, error) {
	link = strings.TrimSpace(link)
	scheme, _, ok := strings.Cut(link, "://")
	if !ok {
		return nil, errors.New("не похоже на ссылку")
	}
	var (
		s   *Server
		err error
	)
	switch strings.ToLower(scheme) {
	case "vless":
		s, err = parseVless(link)
	case "vmess":
		s, err = parseVmess(link)
	case "trojan":
		s, err = parseTrojan(link)
	case "ss":
		s, err = parseShadowsocks(link)
	case "hysteria2", "hy2":
		s, err = parseHysteria2(link)
	case "vpn":
		s, err = parseAmneziaVPN(link)
	case "wireguard", "wg":
		s, err = parseWireGuardLink(link)
	case "socks", "socks5", "socks5h", "http", "https":
		s, err = parseProxyLink(link)
	case "ssconf":
		return nil, errors.New("ключ ssconf:// нужно загрузить — добавьте его через «Вставить» или «Добавить»")
	default:
		return nil, fmt.Errorf("протокол %q не поддерживается", scheme)
	}
	if err != nil {
		return nil, err
	}
	return finalize(s, link)
}

// finalize validates a parsed server and fills in defaults.
func finalize(s *Server, link string) (*Server, error) {
	if s.Address == "" || s.Port <= 0 || s.Port > 65535 {
		return nil, errors.New("в ссылке нет адреса или порта")
	}
	s.ID = NewID()
	s.Link = link
	if s.Name == "" {
		s.Name = net.JoinHostPort(s.Address, strconv.Itoa(s.Port))
	}
	if s.Network == "" || s.Network == "raw" {
		s.Network = "tcp"
	}
	if s.Network == "splithttp" {
		s.Network = "xhttp"
	}
	if s.Security == "" {
		s.Security = "none"
	}
	return s, nil
}

// ParseMany parses a block of text that is either a list of links (one per
// line) or a base64-encoded list, as returned by most subscription servers.
func ParseMany(text string) ([]*Server, []error) {
	text = Decode(text)
	if IsOpenVPNConf(text) {
		s, err := ParseOpenVPNConf(text, "")
		if err == nil {
			s, err = finalize(s, text)
		}
		if err != nil {
			return nil, []error{err}
		}
		return []*Server{s}, nil
	}
	if IsWireGuardConf(text) {
		s, err := ParseWireGuardConf(text, "")
		if err == nil {
			s, err = finalize(s, text)
		}
		if err != nil {
			return nil, []error{err}
		}
		return []*Server{s}, nil
	}
	var (
		servers []*Server
		errs    []error
	)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		s, err := ParseLink(line)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		servers = append(servers, s)
	}
	return servers, errs
}

// Decode returns the plain-text form of a subscription body, decoding it
// from base64 if needed.
func Decode(text string) string {
	text = strings.TrimSpace(text)
	if !strings.Contains(text, "://") {
		if dec, err := decodeBase64(text); err == nil {
			return strings.TrimSpace(string(dec))
		}
	}
	return text
}

func decodeBase64(s string) ([]byte, error) {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, s)
	s = strings.TrimRight(s, "=")
	if b, err := base64.RawStdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.RawURLEncoding.DecodeString(s)
}

func splitHostPort(u *url.URL) (string, int, error) {
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		return "", 0, errors.New("неверный порт")
	}
	return u.Hostname(), port, nil
}

// applyStreamQuery fills transport and security fields from the query string
// shared by vless:// and trojan:// links.
func applyStreamQuery(s *Server, q url.Values) {
	s.Network = q.Get("type")
	s.HeaderType = q.Get("headerType")
	s.Host = q.Get("host")
	s.Path = q.Get("path")
	s.ServiceName = q.Get("serviceName")
	s.Mode = q.Get("mode")
	s.Extra = q.Get("extra")
	s.Security = q.Get("security")
	s.SNI = q.Get("sni")
	if s.SNI == "" {
		s.SNI = q.Get("peer")
	}
	s.ALPN = q.Get("alpn")
	s.Fingerprint = q.Get("fp")
	s.PublicKey = q.Get("pbk")
	if s.PublicKey == "" {
		s.PublicKey = q.Get("password")
	}
	s.ShortID = q.Get("sid")
	s.SpiderX = q.Get("spx")
	s.PinnedSHA = q.Get("pcs")
	s.VerifyName = q.Get("vcn")
}

func parseVless(link string) (*Server, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	host, port, err := splitHostPort(u)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	s := &Server{
		Protocol:   "vless",
		Name:       u.Fragment,
		Address:    host,
		Port:       port,
		UUID:       u.User.Username(),
		Flow:       q.Get("flow"),
		Encryption: q.Get("encryption"),
	}
	if s.Encryption == "" {
		s.Encryption = "none"
	}
	applyStreamQuery(s, q)
	return s, nil
}

func parseTrojan(link string) (*Server, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	host, port, err := splitHostPort(u)
	if err != nil {
		return nil, err
	}
	s := &Server{
		Protocol: "trojan",
		Name:     u.Fragment,
		Address:  host,
		Port:     port,
		Password: u.User.Username(),
	}
	applyStreamQuery(s, u.Query())
	if s.Security == "" {
		s.Security = "tls"
	}
	return s, nil
}

// vmess:// links are base64-encoded JSON objects (v2rayN format).
func parseVmess(link string) (*Server, error) {
	raw, err := decodeBase64(strings.TrimPrefix(link, link[:len("vmess://")]))
	if err != nil {
		return nil, errors.New("vmess: неверный base64")
	}
	var v map[string]any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, errors.New("vmess: неверный JSON")
	}
	str := func(k string) string {
		switch x := v[k].(type) {
		case string:
			return x
		case float64:
			return strconv.FormatFloat(x, 'f', -1, 64)
		}
		return ""
	}
	port, _ := strconv.Atoi(str("port"))
	aid, _ := strconv.Atoi(str("aid"))
	s := &Server{
		Protocol:    "vmess",
		Name:        str("ps"),
		Address:     str("add"),
		Port:        port,
		UUID:        str("id"),
		AlterID:     aid,
		Cipher:      str("scy"),
		Network:     str("net"),
		HeaderType:  str("type"),
		Host:        str("host"),
		Path:        str("path"),
		Security:    str("tls"),
		SNI:         str("sni"),
		ALPN:        str("alpn"),
		Fingerprint: str("fp"),
	}
	if s.Cipher == "" {
		s.Cipher = "auto"
	}
	if s.Network == "grpc" {
		s.ServiceName, s.Path = s.Path, ""
		s.Mode = s.HeaderType
		s.HeaderType = ""
	}
	return s, nil
}

// ss:// supports both SIP002 (ss://base64(method:pass)@host:port) and the
// legacy form (ss://base64(method:pass@host:port)).
func parseShadowsocks(link string) (*Server, error) {
	body := strings.TrimPrefix(link, link[:len("ss://")])
	name := ""
	if i := strings.Index(body, "#"); i >= 0 {
		name, _ = url.PathUnescape(body[i+1:])
		body = body[:i]
	}
	// Plugin options are not supported; '/' is not stripped since it is a
	// valid character in standard base64. Outline's "prefix" is kept.
	var prefix string
	if i := strings.Index(body, "?"); i >= 0 {
		if q, err := url.ParseQuery(body[i+1:]); err == nil {
			prefix = q.Get("prefix")
		}
		body = body[:i]
	}
	if !strings.Contains(body, "@") {
		dec, err := decodeBase64(body)
		if err != nil {
			return nil, errors.New("ss: неверный base64")
		}
		body = string(dec)
	}
	at := strings.LastIndex(body, "@")
	if at < 0 {
		return nil, errors.New("ss: неверный формат")
	}
	userinfo, hostport := body[:at], body[at+1:]
	if i := strings.IndexAny(hostport, "/?"); i >= 0 {
		hostport = hostport[:i]
	}
	if dec, err := decodeBase64(userinfo); err == nil && strings.Contains(string(dec), ":") {
		userinfo = string(dec)
	} else if un, err := url.PathUnescape(userinfo); err == nil {
		userinfo = un
	}
	method, password, ok := strings.Cut(userinfo, ":")
	if !ok {
		return nil, errors.New("ss: нет метода шифрования")
	}
	host, portStr, err := net.SplitHostPort(hostport)
	if err != nil {
		return nil, errors.New("ss: неверный адрес")
	}
	port, _ := strconv.Atoi(portStr)
	return &Server{
		Protocol: "shadowsocks",
		Name:     name,
		Address:  host,
		Port:     port,
		Cipher:   method,
		Password: password,
		Prefix:   prefix,
	}, nil
}

// Reparse parses s.Link again with the current parser, keeping the identity
// fields (ID, name, subscription). Used to upgrade stored servers when the
// parser learns new parameters.
func Reparse(s *Server) (*Server, error) {
	servers, errs := ParseMany(s.Link)
	if len(servers) != 1 {
		if len(errs) > 0 {
			return nil, errs[0]
		}
		return nil, errors.New("ссылка не распознана")
	}
	fresh := servers[0]
	fresh.ID, fresh.Name, fresh.SubID, fresh.Link = s.ID, s.Name, s.SubID, s.Link
	return fresh, nil
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
