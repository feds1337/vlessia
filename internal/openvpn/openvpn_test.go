package openvpn

import (
	"bytes"
	"context"

	"golang.org/x/net/proxy"

	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/feds1337/vlessia/internal/profile"
)

type pki struct {
	ca, serverCert, serverKey, clientCert, clientKey, tlsCrypt string
}

func newPKI(t *testing.T) pki {
	enc := func(typ string, b []byte) string { return string(pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: b})) }
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	caTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test-ca"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, _ := x509.ParseCertificate(caDER)
	leaf := func(serial int64, cn string, usage x509.ExtKeyUsage) (string, string) {
		key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		tmpl := &x509.Certificate{
			SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: cn}, DNSNames: []string{cn},
			NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
			ExtKeyUsage: []x509.ExtKeyUsage{usage}, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyAgreement,
		}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
		if err != nil {
			t.Fatal(err)
		}
		kb, _ := x509.MarshalPKCS8PrivateKey(key)
		return enc("CERTIFICATE", der), enc("PRIVATE KEY", kb)
	}
	p := pki{ca: enc("CERTIFICATE", caDER)}
	p.serverCert, p.serverKey = leaf(2, "vpn.test", x509.ExtKeyUsageServerAuth)
	p.clientCert, p.clientKey = leaf(3, "client", x509.ExtKeyUsageClientAuth)

	b := make([]byte, 256)
	_, _ = rand.Read(b)
	var sk strings.Builder
	sk.WriteString("-----BEGIN OpenVPN Static key V1-----\n")
	for i := 0; i < len(b); i += 16 {
		sk.WriteString(hex.EncodeToString(b[i:i+16]) + "\n")
	}
	sk.WriteString("-----END OpenVPN Static key V1-----\n")
	p.tlsCrypt = sk.String()
	return p
}

// syncBuffer collects the server log from the child process.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// TestOpenVPNAgainstOfficialServer runs the official openvpn.exe as a
// server (dev null: no adapter, nothing installed) and connects to it.
// Set OPENVPN_EXE to the path of openvpn.exe to enable.
func TestOpenVPNAgainstOfficialServer(t *testing.T) {
	exe := os.Getenv("OPENVPN_EXE")
	if exe == "" {
		t.Skip("set OPENVPN_EXE")
	}
	for _, proto := range []string{"udp", "tcp"} {
		t.Run(proto, func(t *testing.T) { runAgainstServer(t, exe, proto) })
	}
}

func runAgainstServer(t *testing.T, exe, proto string) {
	p := newPKI(t)
	dir := t.TempDir()
	write := func(name, data string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		// openvpn treats backslashes in config files as escapes.
		return filepath.ToSlash(path)
	}
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	serverProto := map[string]string{"udp": "udp4", "tcp": "tcp4-server"}[proto]
	conf := write("server.conf", strings.Join([]string{
		"mode server", "tls-server", "dev null", "proto " + serverProto, "local 127.0.0.1",
		"port " + itoa(port), "topology subnet",
		"ifconfig 10.8.0.1 255.255.255.0", "ifconfig-pool 10.8.0.2 10.8.0.50",
		`push "topology subnet"`, `push "route-gateway 10.8.0.1"`, `push "dhcp-option DNS 10.8.0.1"`,
		"ca " + write("ca.crt", p.ca), "cert " + write("server.crt", p.serverCert),
		"key " + write("server.key", p.serverKey), "dh none",
		"tls-crypt " + write("tc.key", p.tlsCrypt),
		"data-ciphers AES-256-GCM:AES-128-GCM:CHACHA20-POLY1305",
		"keepalive 10 60", "verb 7", // 7 logs data packets, checked below
	}, "\n")+"\n")

	var serverLog syncBuffer
	cmd := exec.Command(exe, "--config", conf)
	cmd.Stdout, cmd.Stderr = &serverLog, &serverLog
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if t.Failed() {
			t.Logf("server log:\n%s", serverLog.String())
		}
	}()
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(serverLog.String(), "Initialization Sequence Completed") {
		if time.Now().After(deadline) {
			t.Fatalf("server did not start")
		}
		time.Sleep(100 * time.Millisecond)
	}

	client := strings.Join([]string{
		"client", "dev tun", "proto " + proto, "remote vpn.test " + itoa(port), "nobind",
		"remote-cert-tls server", "verify-x509-name vpn.test name",
		"<ca>", p.ca, "</ca>", "<cert>", p.clientCert, "</cert>", "<key>", p.clientKey, "</key>",
		"<tls-crypt>", p.tlsCrypt, "</tls-crypt>",
	}, "\n")
	servers, errs := profile.ParseMany(client)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	s := servers[0]

	var logMu sync.Mutex
	var clientLog []string
	logf := func(f string, a ...any) {
		logMu.Lock()
		clientLog = append(clientLog, sprintf(f, a...))
		logMu.Unlock()
	}
	start := time.Now()
	tun, err := Start(s.OVPN, "127.0.0.1", nil, logf)
	if err != nil {
		t.Fatalf("connect: %v\nclient log: %v", err, clientLog)
	}
	t.Logf("%s: connected in %v; %v", proto, time.Since(start).Round(time.Millisecond), clientLog)
	if !strings.Contains(strings.Join(clientLog, "\n"), "10.8.0.") {
		t.Fatalf("no tunnel address pushed: %v", clientLog)
	}
	// A dev-null server does not route between clients, so the reply path
	// cannot be tested here (it is shared with AmneziaWG and tested there).
	// Check that traffic from the SOCKS bridge leaves as encrypted data.
	before := strings.Count(serverLog.String(), "P_DATA_V2")
	dialer, _ := proxy.SOCKS5("tcp", "127.0.0.1:"+itoa(tun.Port()), nil, proxy.Direct)
	go func() {
		if c, err := dialer.Dial("tcp", "10.8.0.1:80"); err == nil {
			c.Close()
		}
	}()
	deadline = time.Now().Add(5 * time.Second)
	for strings.Count(serverLog.String(), "P_DATA_V2") == before {
		if time.Now().After(deadline) {
			t.Fatal("no data-channel packets reached the server")
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Logf("%s: data from the SOCKS bridge reaches the server", proto)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() { tun.Close(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("Close hung")
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

func sprintf(f string, a ...any) string { return fmt.Sprintf(f, a...) }
