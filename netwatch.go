package main

import (
	"net"
	"sort"
	"strings"
	"time"

	"github.com/feds1337/vlessia/internal/tun"
)

// netWatch reconnects a live connection when the network changes under it:
// another adapter takes over (Wi-Fi to Ethernet, a hotspot), an address
// changes, or the machine wakes up from sleep. The watchdog alone notices
// that only after two failed checks, up to a minute later.
func (a *App) netWatch() {
	const every = 5 * time.Second
	sig := networkSignature()
	// A change must survive one tick: adapters come and go while a
	// connection is being established.
	pending := ""
	lastTick, lastReconnect := time.Now(), time.Time{}
	t := time.NewTicker(every)
	defer t.Stop()
	for range t.C {
		now := time.Now()
		woke := now.Sub(lastTick) > 30*time.Second
		lastTick = now
		current := networkSignature()

		reason := ""
		switch {
		case woke:
			reason = "компьютер вышел из сна"
		case current == "" || current == sig:
			pending = ""
			continue
		case pending != current:
			pending = current
			continue
		default:
			reason = "сменилась сеть"
		}
		pending = ""
		sig = current

		if a.instance() == nil {
			continue // nothing to reconnect
		}
		if now.Sub(lastReconnect) < 30*time.Second {
			continue
		}
		lastReconnect = now
		a.log.Printf("%s — переподключаюсь", reason)
		a.notice(reason + " — переподключаюсь")
		if err := a.reconnectIfActive(); err != nil {
			a.log.Printf("переподключение после смены сети: %v", err)
		}
	}
}

// networkSignature describes the network the client runs on: the up
// interfaces with their IPv4 addresses, without loopback, link-local
// addresses and the client's own tunnel adapter (it appears and disappears
// with every connection).
func networkSignature() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	var parts []string
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 || ifc.Name == tun.AdapterName {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		var ips []string
		for _, a := range addrs {
			ip, _, err := net.ParseCIDR(a.String())
			if err != nil || ip.To4() == nil || ip.IsLinkLocalUnicast() {
				continue
			}
			ips = append(ips, ip.String())
		}
		if len(ips) == 0 {
			continue // e.g. a virtual adapter that is up but unused
		}
		sort.Strings(ips)
		parts = append(parts, ifc.Name+":"+strings.Join(ips, ","))
	}
	sort.Strings(parts)
	return strings.Join(parts, ";")
}
