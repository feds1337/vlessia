// Package socks is a minimal SOCKS5 server (CONNECT and UDP ASSOCIATE,
// no authentication) on loopback. It exposes a userspace network stack to
// Xray, which talks to it through a regular "socks" outbound.
package socks

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Network is where connections are actually made.
type Network interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
	// ListenUDP returns an unconnected UDP socket.
	ListenUDP() (net.PacketConn, error)
	LookupHost(ctx context.Context, host string) ([]string, error)
}

type Server struct {
	// ctx is cancelled by Close, aborting dials and lookups still in flight
	// (an unanswered SYN would otherwise hold Close until its timeout).
	ctx    context.Context
	cancel context.CancelFunc
	ln     net.Listener
	net    Network
	logf   func(string, ...any)
	wg     sync.WaitGroup
	mu     sync.Mutex
	conns  map[io.Closer]struct{}
	closed bool

	logMu      sync.Mutex
	logWindow  time.Time
	logged     int
	suppressed int
}

// Errors per window that are written to the log; the rest are counted. A
// browser opening a blocked page can produce hundreds of failed dials.
const (
	logBurst  = 5
	logWindow = 10 * time.Second
)

// logError writes a connection error to the log, rate-limited. Expected
// failures are skipped: EOF, and "network is unreachable" for IPv6
// destinations when the tunnel has no IPv6 address.
func (s *Server) logError(err error) {
	if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "network is unreachable") {
		return
	}
	s.logMu.Lock()
	now := time.Now()
	if now.Sub(s.logWindow) >= logWindow {
		if s.suppressed > 0 {
			s.logf("socks: ещё %d ошибок соединения не записано", s.suppressed)
		}
		s.logWindow, s.logged, s.suppressed = now, 0, 0
	}
	write := s.logged < logBurst
	if write {
		s.logged++
	} else {
		s.suppressed++
	}
	s.logMu.Unlock()
	if write {
		s.logf("socks: %v", err)
	}
}

// Listen starts a server on a random loopback port.
func Listen(n Network, logf func(string, ...any)) (*Server, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{ctx: ctx, cancel: cancel, ln: ln, net: n, logf: logf, conns: map[io.Closer]struct{}{}}
	s.wg.Add(1)
	go s.serve()
	return s, nil
}

func (s *Server) Port() int { return s.ln.Addr().(*net.TCPAddr).Port }

// Close stops the listener and drops all connections, both the clients'
// and the tunnel side, so no handler waits for a remote peer to hang up.
func (s *Server) Close() {
	s.cancel()
	_ = s.ln.Close()
	s.mu.Lock()
	s.closed = true
	for c := range s.conns {
		_ = c.Close()
	}
	s.mu.Unlock()
	s.wg.Wait()
}

func (s *Server) serve() {
	defer s.wg.Done()
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.track(c, true)
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer s.track(c, false)
			defer c.Close()
			if err := s.handle(c); err != nil {
				s.logError(err)
			}
		}()
	}
}

// track registers c for Close. A connection added after Close is closed
// immediately.
func (s *Server) track(c io.Closer, add bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !add {
		delete(s.conns, c)
		return
	}
	if s.closed {
		_ = c.Close()
		return
	}
	s.conns[c] = struct{}{}
}

const (
	cmdConnect   = 1
	cmdAssociate = 3
	atypIPv4     = 1
	atypDomain   = 3
	atypIPv6     = 4
)

func (s *Server) handle(c net.Conn) error {
	_ = c.SetDeadline(time.Now().Add(15 * time.Second))
	buf := make([]byte, 262)

	// Greeting: VER NMETHODS METHODS...
	if _, err := io.ReadFull(c, buf[:2]); err != nil {
		return err
	}
	if buf[0] != 5 {
		return errors.New("not SOCKS5")
	}
	if _, err := io.ReadFull(c, buf[:buf[1]]); err != nil {
		return err
	}
	if _, err := c.Write([]byte{5, 0}); err != nil {
		return err
	}

	// Request: VER CMD RSV ATYP DST.ADDR DST.PORT
	if _, err := io.ReadFull(c, buf[:3]); err != nil {
		return err
	}
	cmd := buf[1]
	host, port, err := readAddr(c)
	if err != nil {
		return err
	}
	_ = c.SetDeadline(time.Time{})

	switch cmd {
	case cmdConnect:
		return s.connect(c, net.JoinHostPort(host, strconv.Itoa(port)))
	case cmdAssociate:
		return s.associate(c)
	default:
		_, _ = c.Write([]byte{5, 7, 0, atypIPv4, 0, 0, 0, 0, 0, 0})
		return fmt.Errorf("unsupported command %d", cmd)
	}
}

func (s *Server) connect(c net.Conn, target string) error {
	ctx, cancel := context.WithTimeout(s.ctx, 15*time.Second)
	remote, err := s.net.DialContext(ctx, "tcp", target)
	cancel()
	if err != nil {
		_, _ = c.Write([]byte{5, 5, 0, atypIPv4, 0, 0, 0, 0, 0, 0})
		return fmt.Errorf("connect %s: %w", target, err)
	}
	s.track(remote, true)
	defer s.track(remote, false)
	defer remote.Close()
	if _, err := c.Write([]byte{5, 0, 0, atypIPv4, 0, 0, 0, 0, 0, 0}); err != nil {
		return err
	}

	done := make(chan struct{}, 2)
	pipe := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		// Half-close if possible so the other direction can finish.
		if cw, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		} else {
			_ = dst.Close()
		}
		done <- struct{}{}
	}
	go pipe(remote, c)
	go pipe(c, remote)
	<-done
	// One side is finished; don't let the other linger on a peer that never
	// hangs up (keep-alive connections).
	deadline := time.Now().Add(5 * time.Second)
	_ = c.SetDeadline(deadline)
	_ = remote.SetDeadline(deadline)
	<-done
	return nil
}

// associate relays UDP between a loopback socket the client sends SOCKS5
// UDP datagrams to and an unconnected socket in the tunnel. The relay lives
// as long as the control TCP connection.
func (s *Server) associate(c net.Conn) error {
	local, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		return err
	}
	defer local.Close()
	remote, err := s.net.ListenUDP()
	if err != nil {
		_, _ = c.Write([]byte{5, 1, 0, atypIPv4, 0, 0, 0, 0, 0, 0})
		return err
	}
	s.track(remote, true)
	defer s.track(remote, false)
	defer remote.Close()

	la := local.LocalAddr().(*net.UDPAddr)
	reply := []byte{5, 0, 0, atypIPv4, 127, 0, 0, 1, 0, 0}
	binary.BigEndian.PutUint16(reply[8:], uint16(la.Port))
	if _, err := c.Write(reply); err != nil {
		return err
	}

	var (
		clientMu sync.Mutex
		client   *net.UDPAddr
	)
	// Client -> tunnel.
	go func() {
		buf := make([]byte, 65535)
		for {
			n, from, err := local.ReadFromUDP(buf)
			if err != nil {
				return
			}
			clientMu.Lock()
			client = from
			clientMu.Unlock()
			if n < 4 || buf[2] != 0 { // fragmentation is not supported
				continue
			}
			host, port, hdrLen, err := parseAddr(buf[3:n])
			if err != nil {
				continue
			}
			dst, err := s.resolveUDP(host, port)
			if err != nil {
				continue
			}
			_, _ = remote.WriteTo(buf[3+hdrLen:n], dst)
		}
	}()
	// Tunnel -> client.
	go func() {
		buf := make([]byte, 65535)
		for {
			n, from, err := remote.ReadFrom(buf[22:])
			if err != nil {
				return
			}
			clientMu.Lock()
			to := client
			clientMu.Unlock()
			ua, ok := from.(*net.UDPAddr)
			if to == nil || !ok {
				continue
			}
			hdr := udpHeader(ua)
			start := 22 - len(hdr)
			copy(buf[start:], hdr)
			_, _ = local.WriteToUDP(buf[start:22+n], to)
		}
	}()

	// Block until the client closes the control connection.
	_, _ = io.Copy(io.Discard, c)
	return nil
}

func (s *Server) resolveUDP(host string, port int) (*net.UDPAddr, error) {
	if ip, err := netip.ParseAddr(host); err == nil {
		return net.UDPAddrFromAddrPort(netip.AddrPortFrom(ip.Unmap(), uint16(port))), nil
	}
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()
	addrs, err := s.net.LookupHost(ctx, host)
	if err != nil || len(addrs) == 0 {
		return nil, fmt.Errorf("resolve %s: %v", host, err)
	}
	ip, err := netip.ParseAddr(addrs[0])
	if err != nil {
		return nil, err
	}
	return net.UDPAddrFromAddrPort(netip.AddrPortFrom(ip, uint16(port))), nil
}

func udpHeader(a *net.UDPAddr) []byte {
	ip := a.AddrPort().Addr().Unmap()
	var h []byte
	if ip.Is4() {
		b := ip.As4()
		h = append([]byte{0, 0, 0, atypIPv4}, b[:]...)
	} else {
		b := ip.As16()
		h = append([]byte{0, 0, 0, atypIPv6}, b[:]...)
	}
	return binary.BigEndian.AppendUint16(h, uint16(a.Port))
}

func readAddr(r io.Reader) (string, int, error) {
	var atyp [1]byte
	if _, err := io.ReadFull(r, atyp[:]); err != nil {
		return "", 0, err
	}
	var host string
	switch atyp[0] {
	case atypIPv4, atypIPv6:
		b := make([]byte, map[byte]int{atypIPv4: 4, atypIPv6: 16}[atyp[0]])
		if _, err := io.ReadFull(r, b); err != nil {
			return "", 0, err
		}
		ip, _ := netip.AddrFromSlice(b)
		host = ip.String()
	case atypDomain:
		var l [1]byte
		if _, err := io.ReadFull(r, l[:]); err != nil {
			return "", 0, err
		}
		b := make([]byte, l[0])
		if _, err := io.ReadFull(r, b); err != nil {
			return "", 0, err
		}
		host = string(b)
	default:
		return "", 0, errors.New("bad address type")
	}
	var p [2]byte
	if _, err := io.ReadFull(r, p[:]); err != nil {
		return "", 0, err
	}
	return host, int(binary.BigEndian.Uint16(p[:])), nil
}

// parseAddr parses ATYP+ADDR+PORT at the start of b and returns its length.
func parseAddr(b []byte) (string, int, int, error) {
	if len(b) < 1 {
		return "", 0, 0, errors.New("short")
	}
	var host string
	var n int
	switch b[0] {
	case atypIPv4:
		n = 1 + 4
		if len(b) < n+2 {
			return "", 0, 0, errors.New("short")
		}
		ip, _ := netip.AddrFromSlice(b[1:5])
		host = ip.String()
	case atypIPv6:
		n = 1 + 16
		if len(b) < n+2 {
			return "", 0, 0, errors.New("short")
		}
		ip, _ := netip.AddrFromSlice(b[1:17])
		host = ip.String()
	case atypDomain:
		if len(b) < 2 {
			return "", 0, 0, errors.New("short")
		}
		n = 2 + int(b[1])
		if len(b) < n+2 {
			return "", 0, 0, errors.New("short")
		}
		host = string(b[2:n])
	default:
		return "", 0, 0, errors.New("bad address type")
	}
	return host, int(binary.BigEndian.Uint16(b[n:])), n + 2, nil
}
