// Package openvpn runs an OpenVPN client (go-openvpn, userspace) on a gVisor
// network stack and exposes it as a local SOCKS5 server, like package awg.
package openvpn

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"strings"
	"sync"
	"syscall"
	"time"

	ovpnclient "github.com/n0madic/go-openvpn"
	"github.com/n0madic/go-openvpn/pkg/netstack"
	"golang.org/x/net/dns/dnsmessage"

	"github.com/feds1337/vlessia/internal/profile"
	"github.com/feds1337/vlessia/internal/socks"
)

type Tunnel struct {
	cli   *ovpnclient.Client
	ns    *netstack.Net
	socks *socks.Server
}

// Start connects. remoteIP is the pre-resolved server address; control, if
// set, is applied to the transport socket (pins it to the physical adapter
// while a system TUN is active).
func Start(o *profile.OpenVPN, remoteIP string, control func(string, string, syscall.RawConn) error, logf func(string, ...any)) (*Tunnel, error) {
	p, err := profile.ParseOpenVPNProfile(o.Config, o.Username, o.Password)
	if err != nil {
		return nil, err
	}
	cfg := p.Config
	if p.AuthUserPass && cfg.Username == "" {
		return nil, ErrNeedAuth
	}
	_, port, err := net.SplitHostPort(cfg.RemoteAddr)
	if err != nil {
		return nil, err
	}
	target := net.JoinHostPort(remoteIP, port)
	stream := strings.HasPrefix(cfg.Network, "tcp")

	// Dial the pre-resolved IP ourselves: on reconnect, system DNS may
	// already point into the tunnel.
	cfg.DialTransport = func(ctx context.Context, _, _ string) (ovpnclient.Transport, error) {
		d := net.Dialer{Control: control}
		if stream {
			c, err := d.DialContext(ctx, "tcp", target)
			if err != nil {
				return nil, err
			}
			return ovpnclient.NewStreamTransport(c), nil
		}
		c, err := d.DialContext(ctx, "udp", target)
		if err != nil {
			return nil, err
		}
		return ovpnclient.NewDatagramTransport(c), nil
	}
	cfg.AutoReconnect = true
	cfg.Logger = slog.New(&logHandler{logf: logf})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cli, err := ovpnclient.Dial(ctx, cfg)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "auth") {
			return nil, fmt.Errorf("OpenVPN: сервер отклонил логин или пароль: %w", err)
		}
		return nil, profile.OpenVPNError(err)
	}
	ns, err := netstack.New(cli)
	if err != nil {
		_ = cli.Close()
		return nil, err
	}
	pr := cli.PushedOptions()
	logf("openvpn: подключено, адрес %s, DNS %v, MTU %d, шифр %s", pr.LocalIP, pr.DNS, pr.MTU, pr.Cipher)

	dns := pr.DNS
	if len(dns) == 0 {
		dns = []netip.Addr{netip.MustParseAddr("1.1.1.1"), netip.MustParseAddr("8.8.8.8")}
	}
	srv, err := socks.Listen(&stackNet{ns: ns, dns: dns}, logf)
	if err != nil {
		_ = ns.Close()
		_ = cli.Close()
		return nil, err
	}
	return &Tunnel{cli: cli, ns: ns, socks: srv}, nil
}

// ErrNeedAuth means the profile requires a username and password.
var ErrNeedAuth = errors.New("need_ovpn_auth")

func (t *Tunnel) Port() int { return t.socks.Port() }

func (t *Tunnel) Close() {
	t.socks.Close()
	_ = t.ns.Close()
	_ = t.cli.Close()
}

// stackNet adapts netstack.Net to socks.Network. The stack only dials
// literal IPs, so names are resolved here through the pushed DNS servers.
type stackNet struct {
	ns  *netstack.Net
	dns []netip.Addr
}

func (s *stackNet) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	if _, err := netip.ParseAddr(host); err != nil {
		addrs, err := s.LookupHost(ctx, host)
		if err != nil {
			return nil, err
		}
		host = addrs[0]
	}
	return s.ns.DialContext(ctx, network, net.JoinHostPort(host, port))
}

func (s *stackNet) ListenUDP() (net.PacketConn, error) {
	return newPerDestUDP(s.ns), nil
}

// LookupHost resolves A (and AAAA when the tunnel has IPv6) records.
func (s *stackNet) LookupHost(ctx context.Context, host string) ([]string, error) {
	types := []dnsmessage.Type{dnsmessage.TypeA}
	if s.ns.HasIPv6() {
		types = append(types, dnsmessage.TypeAAAA)
	}
	var lastErr error
	for _, server := range s.dns {
		var out []string
		for _, t := range types {
			addrs, err := s.query(ctx, server, host, t)
			if err != nil {
				lastErr = err
				continue
			}
			out = append(out, addrs...)
		}
		if len(out) > 0 {
			return out, nil
		}
	}
	if lastErr == nil {
		lastErr = errors.New("нет адресов")
	}
	return nil, fmt.Errorf("DNS %s: %w", host, lastErr)
}

func (s *stackNet) query(ctx context.Context, server netip.Addr, host string, qtype dnsmessage.Type) ([]string, error) {
	name, err := dnsmessage.NewName(strings.TrimSuffix(host, ".") + ".")
	if err != nil {
		return nil, err
	}
	id := uint16(time.Now().UnixNano())
	msg := dnsmessage.Message{
		Header:    dnsmessage.Header{ID: id, RecursionDesired: true},
		Questions: []dnsmessage.Question{{Name: name, Type: qtype, Class: dnsmessage.ClassINET}},
	}
	pkt, err := msg.Pack()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	c, err := s.ns.DialContext(ctx, "udp", netip.AddrPortFrom(server, 53).String())
	if err != nil {
		return nil, err
	}
	defer c.Close()
	deadline, _ := ctx.Deadline()
	_ = c.SetDeadline(deadline)
	buf := make([]byte, 1500)
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := c.Write(pkt); err != nil {
			return nil, err
		}
		_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, err := c.Read(buf)
		if err != nil {
			continue
		}
		var resp dnsmessage.Message
		if resp.Unpack(buf[:n]) != nil || resp.ID != id {
			continue
		}
		var out []string
		for _, a := range resp.Answers {
			switch r := a.Body.(type) {
			case *dnsmessage.AResource:
				out = append(out, netip.AddrFrom4(r.A).String())
			case *dnsmessage.AAAAResource:
				out = append(out, netip.AddrFrom16(r.AAAA).String())
			}
		}
		return out, nil
	}
	return nil, errors.New("DNS-сервер не ответил")
}

// perDestUDP emulates an unconnected UDP socket with one connected socket
// per destination: the stack only offers connected UDP dials.
type perDestUDP struct {
	ns    *netstack.Net
	mu    sync.Mutex
	conns map[netip.AddrPort]net.Conn
	in    chan udpPacket
	done  chan struct{}
	once  sync.Once
}

type udpPacket struct {
	data []byte
	from *net.UDPAddr
}

func newPerDestUDP(ns *netstack.Net) *perDestUDP {
	return &perDestUDP{ns: ns, conns: map[netip.AddrPort]net.Conn{}, in: make(chan udpPacket, 64), done: make(chan struct{})}
}

func (p *perDestUDP) WriteTo(b []byte, addr net.Addr) (int, error) {
	ua, ok := addr.(*net.UDPAddr)
	if !ok {
		return 0, errors.New("не UDP-адрес")
	}
	dst := ua.AddrPort()
	dst = netip.AddrPortFrom(dst.Addr().Unmap(), dst.Port())

	p.mu.Lock()
	c, ok := p.conns[dst]
	if !ok {
		select {
		case <-p.done:
			p.mu.Unlock()
			return 0, net.ErrClosed
		default:
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		var err error
		c, err = p.ns.DialContext(ctx, "udp", dst.String())
		cancel()
		if err != nil {
			p.mu.Unlock()
			return 0, err
		}
		p.conns[dst] = c
		go p.pump(c, net.UDPAddrFromAddrPort(dst))
	}
	p.mu.Unlock()
	return c.Write(b)
}

func (p *perDestUDP) pump(c net.Conn, from *net.UDPAddr) {
	buf := make([]byte, 65535)
	for {
		n, err := c.Read(buf)
		if err != nil {
			return
		}
		select {
		case p.in <- udpPacket{append([]byte(nil), buf[:n]...), from}:
		case <-p.done:
			return
		}
	}
}

func (p *perDestUDP) ReadFrom(b []byte) (int, net.Addr, error) {
	select {
	case pkt := <-p.in:
		return copy(b, pkt.data), pkt.from, nil
	case <-p.done:
		return 0, nil, net.ErrClosed
	}
}

func (p *perDestUDP) Close() error {
	p.once.Do(func() {
		close(p.done)
		p.mu.Lock()
		for _, c := range p.conns {
			_ = c.Close()
		}
		p.mu.Unlock()
	})
	return nil
}

func (p *perDestUDP) LocalAddr() net.Addr                { return &net.UDPAddr{} }
func (p *perDestUDP) SetDeadline(t time.Time) error      { return nil }
func (p *perDestUDP) SetReadDeadline(t time.Time) error  { return nil }
func (p *perDestUDP) SetWriteDeadline(t time.Time) error { return nil }

// logHandler forwards the library's warnings and errors to the app log.
type logHandler struct {
	logf  func(string, ...any)
	attrs []slog.Attr
}

func (h *logHandler) Enabled(_ context.Context, l slog.Level) bool { return l >= slog.LevelWarn }

func (h *logHandler) Handle(_ context.Context, r slog.Record) error {
	var b strings.Builder
	b.WriteString("openvpn: ")
	b.WriteString(r.Message)
	for _, a := range h.attrs {
		fmt.Fprintf(&b, " %s=%v", a.Key, a.Value)
	}
	r.Attrs(func(a slog.Attr) bool {
		fmt.Fprintf(&b, " %s=%v", a.Key, a.Value)
		return true
	})
	h.logf("%s", b.String())
	return nil
}

func (h *logHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &logHandler{logf: h.logf, attrs: append(append([]slog.Attr{}, h.attrs...), attrs...)}
}

func (h *logHandler) WithGroup(string) slog.Handler { return h }
