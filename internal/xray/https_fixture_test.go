package xray

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The client requests a domain, so private-address bypass cannot short-circuit
// the tested tunnel. Only the remote test server redirects it to this fixture.
func httpsFixture(t *testing.T) (string, *tls.Config) {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	t.Cleanup(server.Close)
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	return server.Listener.Addr().String(), &tls.Config{RootCAs: roots, ServerName: server.Certificate().DNSNames[0]}
}

func fixtureOutbound(t *testing.T, config []byte, addr string) []byte {
	t.Helper()
	var cfg map[string]any
	if err := json.Unmarshal(config, &cfg); err != nil {
		t.Fatal(err)
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	// Xray blocks private destinations for VLESS/Hysteria servers by default.
	// Permit only this fixture's exact loopback endpoint in the test server.
	cfg["outbounds"].([]any)[0].(map[string]any)["settings"] = map[string]any{
		"redirect":   addr,
		"finalRules": []any{map[string]any{"action": "allow", "ip": []string{"127.0.0.1/32"}, "port": port}},
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
