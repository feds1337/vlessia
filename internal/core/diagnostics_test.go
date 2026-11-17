package core

import (
	"net"
	"net/url"
	"strconv"
	"testing"

	"github.com/feds1337/vlessia/internal/profile"
)

func TestDiagnosticGuards(t *testing.T) {
	for _, tc := range []struct {
		blocked bool
		want    string
	}{{true, "blocked"}, {false, "no_server"}} {
		got := Diagnose(nil, nil, "tun", tc.blocked, false)
		if len(got) != 1 || got[0].Result != tc.want {
			t.Fatalf("%+v", got)
		}
	}
}

func TestDiagnosticServer(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	p, _ := strconv.Atoi(port)
	srv := &profile.Server{Address: "127.0.0.1", Port: p}
	got := Diagnose(srv, nil, "proxy", false, false)
	if len(got) != 2 || got[0].Result != "ok" || got[1].Result != "ok" {
		t.Fatalf("%+v", got)
	}
	listener.Close()
	got = Diagnose(srv, nil, "proxy", false, false)
	if got[1].Result != "failed" {
		t.Fatalf("%+v", got)
	}
	// A failed local listener must not fall back to a direct HTTPS request.
	got = Diagnose(srv, &url.URL{Scheme: "http", Host: net.JoinHostPort("127.0.0.1", port)}, "tun", false, false)
	if len(got) != 2 || got[0].Result != "failed" || got[1].Check != "local_proxy" || got[1].Result != "failed" {
		t.Fatalf("%+v", got)
	}
}
