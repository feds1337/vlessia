package core

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/feds1337/vlessia/internal/profile"
)

// Watchdog checks a running connection and reports when it stops passing
// traffic while the internet itself still works.
type Watchdog struct {
	// Check makes a request through the VPN.
	Check func() error
	// Online reports whether the internet is reachable without the VPN.
	Online func() bool
	// Healthy is called after every successful check.
	Healthy func()
	// Failed is called once the connection is considered dead; the
	// watchdog stops then.
	Failed func()
	Logf   func(string, ...any)

	Interval time.Duration // between checks; 20 s by default
	Failures int           // failed checks in a row that count as dead; 2 by default
}

// Run checks until stop is closed or the connection is found dead.
func (w Watchdog) Run(stop <-chan struct{}) {
	interval, need := w.Interval, w.Failures
	if interval == 0 {
		interval = 20 * time.Second
	}
	if need == 0 {
		need = 2
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	fails := 0
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		err := w.Check()
		select {
		case <-stop:
			return // disconnected meanwhile; the failure is ours
		default:
		}
		if err == nil {
			fails = 0
			if w.Healthy != nil {
				w.Healthy()
			}
			continue
		}
		fails++
		w.Logf("проверка соединения не прошла (%d из %d): %v", fails, need, err)
		if fails < need {
			continue
		}
		if !w.Online() {
			// Without the internet a reconnect cannot help; keep waiting.
			w.Logf("интернет недоступен и без VPN — жду")
			fails = need - 1
			continue
		}
		w.Failed()
		return
	}
}

// CheckThrough makes a small HTTPS request through the proxy, trying a
// second host before giving up.
func CheckThrough(proxy *url.URL) error {
	if proxy == nil {
		return errors.New("не подключено")
	}
	var err error
	for _, u := range []string{"https://www.gstatic.com/generate_204", "https://cp.cloudflare.com/generate_204"} {
		var resp *http.Response
		resp, err = HTTPClient(proxy, 10*time.Second).Get(u)
		if err != nil {
			continue
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusOK {
			return nil
		}
		err = fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return err
}

// Online reports whether well-known hosts answer over TCP with dialer
// (bound to the physical adapter in TUN mode).
func Online(dialer *net.Dialer) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, addr := range []string{"1.1.1.1:443", "77.88.8.8:443", "8.8.8.8:443"} {
		c, err := dialer.DialContext(ctx, "tcp", addr)
		if err == nil {
			c.Close()
			return true
		}
	}
	return false
}

// Fastest pings servers in parallel and returns the one that answered
// quickest, or nil if none did. Protocols that cannot be pinged are
// skipped.
func Fastest(servers []*profile.Server, dialer *net.Dialer) *profile.Server {
	var (
		mu   sync.Mutex
		best *profile.Server
		ms   int64 = -1
	)
	PingAll(servers, dialer, func(id string, got int64) {
		if got < 0 {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if ms < 0 || got < ms {
			for _, s := range servers {
				if s.ID == id {
					best, ms = s, got
				}
			}
		}
	})
	return best
}

// Group returns the servers of the same subscription as srv (or the
// manually added ones), except those in skip.
func Group(all []*profile.Server, srv *profile.Server, skip ...string) []*profile.Server {
	var out []*profile.Server
next:
	for _, s := range all {
		if s.SubID != srv.SubID {
			continue
		}
		for _, id := range skip {
			if s.ID == id {
				continue next
			}
		}
		out = append(out, s)
	}
	return out
}
