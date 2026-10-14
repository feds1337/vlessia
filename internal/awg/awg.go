// Package awg runs a WireGuard / AmneziaWG client on a userspace network
// stack (no system adapter) and exposes it as a local SOCKS5 server.
package awg

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"

	"github.com/amnezia-vpn/amneziawg-go/v3/conn"
	"github.com/amnezia-vpn/amneziawg-go/v3/device"
	"github.com/amnezia-vpn/amneziawg-go/v3/tun/netstack"

	"github.com/feds1337/vlessia/internal/profile"
	"github.com/feds1337/vlessia/internal/socks"
)

// uapiKeys maps AmneziaWG .conf keys (lower-cased) to amneziawg-go UAPI
// keys. Keys not listed (J1-J3, Itime from AWG 1.5) are skipped: the
// library rejects unknown keys.
var uapiKeys = map[string]string{
	"jc": "jc", "jmin": "jmin", "jmax": "jmax",
	"s1": "s1", "s2": "s2", "s3": "s3", "s4": "s4",
	"h1": "h1", "h2": "h2", "h3": "h3", "h4": "h4",
	"i1": "i1", "i2": "i2", "i3": "i3", "i4": "i4", "i5": "i5",
	// AmneziaWG 3.0
	"headerprotectionkey":    "header_protection_key",
	"contentpaddingaddition": "content_padding_addition",
	"rekeyaftertime":         "rekey_after_time",
	"rekeytimeout":           "rekey_timeout",
	"rejectaftertime":        "reject_after_time",
	"keepalivetimeout":       "keepalive_timeout",
	"maxhandshakeattempts":   "max_handshake_attempts",
	// AmneziaWG 3.1
	"randomtrailers": "random_trailers",
	"disablecookies": "disable_cookies",
}

// uapiValue converts a .conf value to the form the UAPI expects.
func uapiValue(key, v string) (string, error) {
	switch key {
	case "header_protection_key":
		raw, err := base64.StdEncoding.DecodeString(v)
		if err != nil || len(raw) != 32 {
			return "", errors.New("неверный HeaderProtectionKey")
		}
		return hex.EncodeToString(raw), nil
	case "random_trailers", "disable_cookies":
		switch strings.ToLower(v) {
		case "on", "yes", "true", "1":
			return "true", nil
		case "off", "no", "false", "0":
			return "false", nil
		}
		return "", fmt.Errorf("неверное значение %q", v)
	}
	return v, nil
}

// Verbose enables handshake-level logging.
var Verbose bool

type Tunnel struct {
	dev   *device.Device
	socks *socks.Server
}

// Start brings the tunnel up. endpoint must be an IP:port. bindIfIndex, if
// non-zero, pins the WireGuard UDP socket to that adapter so it bypasses a
// system-wide TUN.
func Start(wg *profile.WireGuard, endpoint netip.AddrPort, bindIfIndex int, logf func(string, ...any)) (*Tunnel, error) {
	var addrs []netip.Addr
	for _, a := range wg.Addresses {
		p, err := netip.ParsePrefix(a)
		if err != nil {
			ip, err2 := netip.ParseAddr(a)
			if err2 != nil {
				return nil, fmt.Errorf("неверный адрес туннеля %q", a)
			}
			p = netip.PrefixFrom(ip, ip.BitLen())
		}
		addrs = append(addrs, p.Addr())
	}
	var dns []netip.Addr
	for _, d := range wg.DNS {
		if ip, err := netip.ParseAddr(d); err == nil {
			dns = append(dns, ip)
		}
	}
	if len(dns) == 0 {
		dns = []netip.Addr{netip.MustParseAddr("1.1.1.1")}
	}
	mtu := wg.MTU
	if mtu <= 0 {
		mtu = 1376
	}

	tdev, tnet, err := netstack.CreateNetTUN(addrs, dns, mtu)
	if err != nil {
		return nil, fmt.Errorf("netstack: %w", err)
	}
	bind := conn.NewDefaultBind()
	logger := &device.Logger{
		Verbosef: func(string, ...any) {},
		Errorf:   func(f string, a ...any) { logf("awg: "+f, a...) },
	}
	if Verbose {
		logger.Verbosef = func(f string, a ...any) { logf("awg: "+f, a...) }
	}
	dev := device.NewDevice(tdev, bind, logger)

	uapi, err := buildUAPI(wg, endpoint, logf)
	if err != nil {
		dev.Close()
		return nil, err
	}
	if err := dev.IpcSet(uapi); err != nil {
		dev.Close()
		return nil, fmt.Errorf("конфигурация AmneziaWG отклонена: %w", err)
	}
	if err := dev.Up(); err != nil {
		dev.Close()
		return nil, err
	}
	if bindIfIndex > 0 {
		if b, ok := bind.(conn.BindSocketToInterface); ok {
			if endpoint.Addr().Is4() {
				err = b.BindSocketToInterface4(uint32(bindIfIndex), false)
			} else {
				err = b.BindSocketToInterface6(uint32(bindIfIndex), false)
			}
			if err != nil {
				dev.Close()
				return nil, fmt.Errorf("привязка к адаптеру: %w", err)
			}
		}
	}

	sn := stackNet{n: tnet}
	for _, a := range addrs {
		if a.Is4() && !sn.v4.IsValid() {
			sn.v4 = a
		}
		if a.Is6() && !sn.v6.IsValid() {
			sn.v6 = a
		}
	}
	srv, err := socks.Listen(sn, logf)
	if err != nil {
		dev.Close()
		return nil, err
	}
	return &Tunnel{dev: dev, socks: srv}, nil
}

// Port is the local SOCKS5 port.
func (t *Tunnel) Port() int { return t.socks.Port() }

func (t *Tunnel) Close() {
	t.socks.Close()
	t.dev.Close()
}

func buildUAPI(wg *profile.WireGuard, endpoint netip.AddrPort, logf func(string, ...any)) (string, error) {
	var b strings.Builder
	key := func(name, b64 string) error {
		raw, err := base64.StdEncoding.DecodeString(b64)
		if err != nil || len(raw) != 32 {
			return errors.New("неверный ключ " + name)
		}
		fmt.Fprintf(&b, "%s=%s\n", name, hex.EncodeToString(raw))
		return nil
	}

	if err := key("private_key", wg.PrivateKey); err != nil {
		return "", err
	}
	for _, p := range wg.AWG {
		k, ok := uapiKeys[strings.ToLower(p.Key)]
		if !ok {
			logf("awg: параметр %s не поддерживается и пропущен", p.Key)
			continue
		}
		v, err := uapiValue(k, p.Value)
		if err != nil {
			return "", fmt.Errorf("%s: %w", p.Key, err)
		}
		fmt.Fprintf(&b, "%s=%s\n", k, v)
	}
	b.WriteString("replace_peers=true\n")
	if err := key("public_key", wg.PublicKey); err != nil {
		return "", err
	}
	if wg.PresharedKey != "" {
		if err := key("preshared_key", wg.PresharedKey); err != nil {
			return "", err
		}
	}
	fmt.Fprintf(&b, "endpoint=%s\n", endpoint)
	keepalive := strings.TrimSpace(wg.PersistentKeepalive)
	if keepalive == "" {
		keepalive = "25" // keeps NAT mappings alive; most providers expect it
	}
	fmt.Fprintf(&b, "persistent_keepalive_interval=%s\n", keepalive)
	b.WriteString("replace_allowed_ips=true\n")
	for _, a := range wg.AllowedIPs {
		if _, err := netip.ParsePrefix(a); err != nil {
			return "", fmt.Errorf("неверный AllowedIPs %q", a)
		}
		fmt.Fprintf(&b, "allowed_ip=%s\n", a)
	}
	return b.String(), nil
}

// stackNet adapts netstack.Net to socks.Network.
type stackNet struct {
	n *netstack.Net
	// Tunnel addresses; UDP sockets must be bound to them, netstack cannot
	// send from an unspecified (0.0.0.0) address.
	v4, v6 netip.Addr
}

func (s stackNet) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return s.n.DialContext(ctx, network, address)
}

// ListenUDP returns a socket for each address family the tunnel has, joined
// into one when there are two (a netstack endpoint serves a single family).
func (s stackNet) ListenUDP() (net.PacketConn, error) {
	var conns []net.PacketConn
	for _, a := range []netip.Addr{s.v4, s.v6} {
		if !a.IsValid() {
			continue
		}
		c, err := s.n.ListenUDPAddrPort(netip.AddrPortFrom(a, 0))
		if err != nil {
			for _, c := range conns {
				_ = c.Close()
			}
			return nil, err
		}
		conns = append(conns, c)
	}
	switch len(conns) {
	case 0:
		return nil, errors.New("у туннеля нет адреса")
	case 1:
		return conns[0], nil
	}
	return newDualConn(conns[0], conns[1]), nil
}

func (s stackNet) LookupHost(ctx context.Context, host string) ([]string, error) {
	return s.n.LookupContextHost(ctx, host)
}

// dualConn sends by destination family and receives from both sockets.
type dualConn struct {
	net.PacketConn // IPv4; provides deadlines and LocalAddr
	v6             net.PacketConn
	in             chan packet
	done           chan struct{}
	once           sync.Once
}

type packet struct {
	data []byte
	from net.Addr
}

func newDualConn(v4, v6 net.PacketConn) *dualConn {
	d := &dualConn{PacketConn: v4, v6: v6, in: make(chan packet, 64), done: make(chan struct{})}
	go d.pump(v4)
	go d.pump(v6)
	return d
}

func (d *dualConn) pump(c net.PacketConn) {
	buf := make([]byte, 65535)
	for {
		n, from, err := c.ReadFrom(buf)
		if err != nil {
			_ = d.Close()
			return
		}
		select {
		case d.in <- packet{append([]byte(nil), buf[:n]...), from}:
		case <-d.done:
			return
		}
	}
}

func (d *dualConn) ReadFrom(p []byte) (int, net.Addr, error) {
	select {
	case pkt := <-d.in:
		return copy(p, pkt.data), pkt.from, nil
	case <-d.done:
		return 0, nil, net.ErrClosed
	}
}

func (d *dualConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	if ua, ok := addr.(*net.UDPAddr); ok && ua.IP.To4() == nil {
		return d.v6.WriteTo(p, addr)
	}
	return d.PacketConn.WriteTo(p, addr)
}

func (d *dualConn) Close() error {
	d.once.Do(func() {
		close(d.done)
		_ = d.PacketConn.Close()
		_ = d.v6.Close()
	})
	return nil
}
