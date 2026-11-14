package core

import (
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/feds1337/vlessia/internal/profile"
)

type DiagnosticStep struct {
	Check  string `json:"check"`
	Result string `json:"result"`
	MS     int64  `json:"ms"`
}

// Diagnose uses the active local HTTP proxy when connected and never falls
// back to a direct internet request. When disconnected, check the selected
// endpoint only, unless the kill switch is active.
func Diagnose(srv *profile.Server, proxy *url.URL, mode string, blocked, routeReady bool) []DiagnosticStep {
	if blocked {
		return []DiagnosticStep{{Check: "route", Result: "blocked"}}
	}
	if srv == nil && proxy == nil {
		return []DiagnosticStep{{Check: "server", Result: "no_server"}}
	}
	if proxy == nil {
		ip, err := Resolve(srv.Address)
		if err != nil {
			return []DiagnosticStep{{Check: "dns", Result: "failed"}}
		}
		steps := []DiagnosticStep{{Check: "dns", Result: "ok"}}
		if srv.IsUDPOnly() || srv.OVPN != nil && srv.Network == "udp" {
			return append(steps, DiagnosticStep{Check: "server", Result: "udp"})
		}
		return append(steps, probeTCP("server", net.JoinHostPort(ip, strconv.Itoa(srv.Port))))
	}

	route := DiagnosticStep{Check: mode, Result: "ok"}
	if !routeReady {
		route.Result = "failed"
	}
	listener := probeTCP("local_proxy", proxy.Host)
	if listener.Result != "ok" {
		return []DiagnosticStep{route, listener}
	}

	client := HTTPClient(proxy, 5*time.Second)
	defer client.CloseIdleConnections()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	for _, endpoint := range []string{"https://www.gstatic.com/generate_204", "https://cp.cloudflare.com/generate_204"} {
		start := time.Now()
		resp, err := client.Get(endpoint)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusNoContent {
				return []DiagnosticStep{route, listener, {Check: "https", Result: "ok", MS: time.Since(start).Milliseconds()}}
			}
		}
	}
	return []DiagnosticStep{route, listener, {Check: "https", Result: "failed"}}
}

func probeTCP(check, addr string) DiagnosticStep {
	start := time.Now()
	c, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return DiagnosticStep{Check: check, Result: "failed"}
	}
	c.Close()
	return DiagnosticStep{Check: check, Result: "ok", MS: time.Since(start).Milliseconds()}
}
