// Package hysteria probes Hysteria 2 servers with a bare QUIC handshake:
// it measures latency and reads the server certificate, which is used to
// pin self-signed certificates (Xray removed "allowInsecure").
package hysteria

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"net"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/apernet/quic-go"
	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/transport/internet/finalmask"
	"github.com/xtls/xray-core/transport/internet/finalmask/salamander"

	"github.com/feds1337/vlessia/internal/profile"
)

type Result struct {
	RTT time.Duration
	// CertSHA256 is the hex SHA-256 of the server's leaf certificate.
	CertSHA256 string
}

// Probe performs a QUIC handshake with the server at ip (the resolved
// address of s). control, if set, is applied to the UDP socket (used to
// bind it to the physical adapter while a TUN is active).
func Probe(ctx context.Context, s *profile.Server, ip string, control func(string, string, syscall.RawConn) error) (Result, error) {
	lc := net.ListenConfig{Control: control}
	network, laddr := "udp4", "0.0.0.0:0"
	if strings.Contains(ip, ":") {
		network, laddr = "udp6", "[::]:0"
	}
	udp, err := lc.ListenPacket(ctx, network, laddr)
	if err != nil {
		return Result{}, err
	}
	defer udp.Close()

	addr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(ip, strconv.Itoa(s.Port)))
	if err != nil {
		return Result{}, err
	}

	var pc net.PacketConn = udp
	if s.Obfs == "salamander" {
		fm := finalmask.NewFinalMask(nil, []finalmask.UDPMask{&salamander.Config{Password: s.ObfsPassword}}, nil, nil,
			func(context.Context, xnet.Destination) (xnet.PacketConn, xnet.Addr, error) {
				return udp, addr, nil
			}, nil)
		conn, err := fm.DialUDP(ctx, xnet.Destination{})
		if err != nil {
			return Result{}, err
		}
		pc = conn.(*xnet.PacketConnWrapper).PacketConn
	}
	sni := s.SNI
	if sni == "" && net.ParseIP(s.Address) == nil {
		sni = s.Address
	}
	alpn := []string{"h3"}
	if s.ALPN != "" {
		alpn = strings.Split(s.ALPN, ",")
	}
	tlsConf := &tls.Config{
		ServerName: sni,
		// Verification is not the point here: the hash is returned for
		// pinning, and Xray verifies the pin on the real connection.
		InsecureSkipVerify: true,
		NextProtos:         alpn,
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	start := time.Now()
	qc, err := quic.Dial(ctx, pc, addr, tlsConf, &quic.Config{HandshakeIdleTimeout: 5 * time.Second})
	if err != nil {
		return Result{}, err
	}
	rtt := time.Since(start)
	defer qc.CloseWithError(0, "")

	certs := qc.ConnectionState().TLS.PeerCertificates
	if len(certs) == 0 {
		return Result{}, errors.New("сервер не прислал сертификат")
	}
	sum := sha256.Sum256(certs[0].Raw)
	return Result{RTT: rtt, CertSHA256: hex.EncodeToString(sum[:])}, nil
}
