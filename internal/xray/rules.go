package xray

import (
	"fmt"
	"net"
	"regexp"
	"strings"
)

// ruleDomain is a plain domain name such as "youtube.com" or "сайт.рф".
var ruleDomain = regexp.MustCompile(`^(?:[\p{L}\p{N}](?:[\p{L}\p{N}-]*[\p{L}\p{N}])?\.)*[\p{L}\p{N}](?:[\p{L}\p{N}-]*[\p{L}\p{N}])?$`)

// ParseRules turns the user's rule lines into Xray routing domains and IPs.
// A plain domain covers its subdomains; "*.example.com" means the same.
// Xray's own prefixes (domain:, full:, keyword:, regexp:) pass through.
// Empty lines and "#" comments are skipped.
func ParseRules(lines []string) (domains, ips []string, err error) {
	for _, line := range lines {
		r := strings.TrimSpace(line)
		if i := strings.Index(r, "#"); i >= 0 {
			r = strings.TrimSpace(r[:i])
		}
		if r == "" {
			continue
		}
		low := strings.ToLower(r)
		switch {
		case strings.HasPrefix(low, "regexp:"):
			if _, err := regexp.Compile(r[len("regexp:"):]); err != nil {
				return nil, nil, fmt.Errorf("правило %q: неверное регулярное выражение", r)
			}
			domains = append(domains, r)
		case strings.HasPrefix(low, "keyword:"), strings.HasPrefix(low, "full:"), strings.HasPrefix(low, "domain:"):
			if strings.TrimSpace(r[strings.Index(r, ":")+1:]) == "" {
				return nil, nil, fmt.Errorf("правило %q: пустое значение", r)
			}
			domains = append(domains, low)
		case net.ParseIP(r) != nil:
			ips = append(ips, r)
		default:
			if _, _, err := net.ParseCIDR(r); err == nil {
				ips = append(ips, r)
				continue
			}
			d := strings.TrimSuffix(strings.TrimPrefix(low, "*."), ".")
			// Pasted URLs: keep just the host.
			if i := strings.Index(d, "://"); i >= 0 {
				d = d[i+3:]
			}
			if i := strings.IndexAny(d, "/:?"); i >= 0 {
				d = d[:i]
			}
			if !ruleDomain.MatchString(d) {
				return nil, nil, fmt.Errorf("правило %q: это не домен, IP или подсеть", r)
			}
			domains = append(domains, "domain:"+d)
		}
	}
	return domains, ips, nil
}
