package core

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/feds1337/vlessia/internal/hysteria"
	"github.com/feds1337/vlessia/internal/profile"
)

// Resolve returns an IP for host, preferring IPv4.
func Resolve(host string) (string, error) {
	if ip := net.ParseIP(host); ip != nil {
		return host, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return "", err
	}
	for _, ip := range ips {
		if ip.To4() != nil {
			return ip.String(), nil
		}
	}
	return ips[0].String(), nil
}

// PingAll measures every server with up to 16 probes in parallel and
// reports each result as it arrives.
func PingAll(servers []*profile.Server, dialer *net.Dialer, report func(id string, ms int64)) {
	sem := make(chan struct{}, 16)
	var wg sync.WaitGroup
	for _, s := range servers {
		wg.Add(1)
		go func(s *profile.Server) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			report(s.ID, PingServer(s, dialer))
		}(s)
	}
	wg.Wait()
}

// PingServer returns the connect time in ms, -1 if unreachable and -2 if
// the protocol cannot be pinged: WireGuard deliberately ignores packets it
// cannot authenticate.
func PingServer(s *profile.Server, dialer *net.Dialer) int64 {
	switch {
	case s.WG != nil, s.OVPN != nil && s.Network == "udp":
		return -2
	case s.Protocol == "hysteria2":
		ip, err := Resolve(s.Address)
		if err != nil {
			return -1
		}
		res, err := hysteria.Probe(context.Background(), s, ip, dialer.Control)
		if err != nil {
			return -1
		}
		return res.RTT.Milliseconds()
	}
	start := time.Now()
	c, err := dialer.Dial("tcp", net.JoinHostPort(s.Address, strconv.Itoa(s.Port)))
	if err != nil {
		return -1
	}
	_ = c.Close()
	return time.Since(start).Milliseconds()
}

// LocalProxy is the URL of the client's HTTP inbound.
func LocalProxy(port int) *url.URL {
	return &url.URL{Scheme: "http", Host: net.JoinHostPort("127.0.0.1", strconv.Itoa(port))}
}

// HTTPClient returns a client that goes through proxy when it is non-nil.
func HTTPClient(proxy *url.URL, timeout time.Duration) *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil
	if proxy != nil {
		tr.Proxy = http.ProxyURL(proxy)
	}
	return &http.Client{Transport: tr, Timeout: timeout}
}

// TestConnection measures an HTTPS request through the proxy.
func TestConnection(proxy *url.URL) (int64, error) {
	if proxy == nil {
		return 0, errors.New("не подключено")
	}
	tr := &http.Transport{Proxy: http.ProxyURL(proxy), DisableKeepAlives: true}
	client := &http.Client{Transport: tr, Timeout: 10 * time.Second}
	start := time.Now()
	resp, err := client.Get("https://www.gstatic.com/generate_204")
	if err != nil {
		return 0, err
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return time.Since(start).Milliseconds(), nil
}

// Logs returns the tails of app.log and xray.log in dir.
func Logs(dir string) string {
	var b strings.Builder
	for _, name := range []string{"app.log", "xray.log"} {
		fmt.Fprintf(&b, "===== %s =====\n", name)
		b.WriteString(tail(filepath.Join(dir, name), 200))
		b.WriteString("\n")
	}
	return b.String()
}

func tail(path string, n int) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return "(пусто)\n"
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
