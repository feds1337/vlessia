package profile

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
)

// parseHysteria2 parses hysteria2:// and hy2:// links. The authority may
// carry several ports for port hopping ("host:443,20000-30000"), which
// url.Parse rejects, so it is split by hand.
func parseHysteria2(link string) (*Server, error) {
	_, rest, _ := strings.Cut(link, "://")
	rest, frag, _ := strings.Cut(rest, "#")
	rest, rawQuery, _ := strings.Cut(rest, "?")
	rest = strings.TrimSuffix(rest, "/")

	s := &Server{Protocol: "hysteria2", Network: "quic", Security: "tls"}
	s.Name, _ = url.PathUnescape(frag)

	if at := strings.LastIndex(rest, "@"); at >= 0 {
		s.Password, _ = url.PathUnescape(rest[:at])
		rest = rest[at+1:]
	}

	host, ports, err := splitHostPorts(rest)
	if err != nil {
		return nil, err
	}
	s.Address = host

	q, _ := url.ParseQuery(rawQuery)
	if mport := q.Get("mport"); mport != "" {
		ports = ports + "," + mport
	}
	first, hop, err := parsePortSpec(ports)
	if err != nil {
		return nil, err
	}
	s.Port, s.HopPorts = first, hop

	s.SNI = q.Get("sni")
	if s.SNI == "" {
		s.SNI = q.Get("peer")
	}
	s.ALPN = q.Get("alpn")
	s.Obfs = q.Get("obfs")
	s.ObfsPassword = q.Get("obfs-password")
	s.PinnedSHA = q.Get("pinSHA256")
	if s.PinnedSHA == "" {
		s.PinnedSHA = q.Get("pcs")
	}
	s.VerifyName = q.Get("vcn")
	s.Insecure = q.Get("insecure") == "1" || strings.EqualFold(q.Get("insecure"), "true")
	s.UpMbps = atoiAny(q.Get("upmbps"), q.Get("up"))
	s.DownMbps = atoiAny(q.Get("downmbps"), q.Get("down"))
	if s.Obfs != "" && s.Obfs != "salamander" {
		return nil, errors.New("hysteria2: неизвестная обфускация " + s.Obfs)
	}
	return s, nil
}

// splitHostPorts splits "host:ports" where host may be a bracketed IPv6.
func splitHostPorts(s string) (string, string, error) {
	if strings.HasPrefix(s, "[") {
		end := strings.Index(s, "]")
		if end < 0 || len(s) < end+2 || s[end+1] != ':' {
			return "", "", errors.New("неверный адрес")
		}
		return s[1:end], s[end+2:], nil
	}
	i := strings.LastIndex(s, ":")
	if i < 0 {
		return "", "", errors.New("в ссылке нет порта")
	}
	return s[:i], s[i+1:], nil
}

// parsePortSpec returns the first port and, if more than one port is
// listed, the whole spec normalised for port hopping.
func parsePortSpec(spec string) (int, string, error) {
	var parts []string
	first := 0
	for _, p := range strings.Split(spec, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		lo, hi, isRange := strings.Cut(p, "-")
		a, err := strconv.Atoi(lo)
		if err != nil || a <= 0 || a > 65535 {
			return 0, "", errors.New("неверный порт " + p)
		}
		if isRange {
			b, err := strconv.Atoi(hi)
			if err != nil || b < a || b > 65535 {
				return 0, "", errors.New("неверный диапазон портов " + p)
			}
		}
		if first == 0 {
			first = a
		}
		parts = append(parts, p)
	}
	if first == 0 {
		return 0, "", errors.New("в ссылке нет порта")
	}
	if len(parts) == 1 && !strings.Contains(parts[0], "-") {
		return first, "", nil
	}
	return first, strings.Join(parts, ","), nil
}

func atoiAny(vals ...string) int {
	for _, v := range vals {
		// Values may carry a unit ("100 mbps"); take the leading number.
		v = strings.TrimSpace(v)
		end := 0
		for end < len(v) && v[end] >= '0' && v[end] <= '9' {
			end++
		}
		if n, err := strconv.Atoi(v[:end]); err == nil && n > 0 {
			return n
		}
	}
	return 0
}
