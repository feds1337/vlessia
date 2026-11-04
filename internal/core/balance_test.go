package core

import (
	"io"
	"log"
	"net"
	"path/filepath"
	"testing"

	"github.com/feds1337/vlessia/internal/profile"
	"github.com/feds1337/vlessia/internal/store"
)

// livePort returns a loopback port that accepts connections, so the health
// check of the balancing group sees the servers as alive.
func livePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

// deadPort returns a loopback port nothing listens on.
func deadPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

// balanceLibrary builds a Library over a temporary store holding servers.
func balanceLibrary(t *testing.T, servers ...*profile.Server) (*Library, *store.Store) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "data")
	t.Setenv("VLESSIA_DATA", dir)
	st, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Update(func(d *store.Data) { d.Servers = servers }); err != nil {
		t.Fatal(err)
	}
	lib := &Library{Store: st, Log: log.New(io.Discard, "", 0)}
	return lib, st
}

func setBalance(t *testing.T, st *store.Store, balance bool, mode string) store.Settings {
	t.Helper()
	var s store.Settings
	if err := st.Update(func(d *store.Data) {
		d.Settings.Balance, d.Settings.Mode = balance, mode
		s = d.Settings
	}); err != nil {
		t.Fatal(err)
	}
	return s
}

// TestBalanceGroup: the group is the selected server's subscription without
// the full tunnels and without the servers that do not answer; proxy mode
// needs no resolved addresses — the bug that turned balancing off in the
// default mode.
func TestBalanceGroup(t *testing.T) {
	port := livePort(t)
	vless := func(id, sub string, port int) *profile.Server {
		return &profile.Server{ID: id, Name: id, Protocol: "vless", Address: "127.0.0.1", Port: port, SubID: sub}
	}
	dead := deadPort(t)
	lib, st := balanceLibrary(t,
		vless("a", "sub", port), vless("b", "sub", port), vless("dead", "sub", dead),
		vless("other", "other", port),
		&profile.Server{ID: "wg", Name: "wg", Protocol: "wireguard", Address: "127.0.0.1", Port: port, SubID: "sub", WG: &profile.WireGuard{}},
		vless("manual", "", port),
	)
	srv := lib.Store.Server("a")
	logger := log.New(io.Discard, "", 0)

	t.Run("off", func(t *testing.T) {
		settings := setBalance(t, st, false, "proxy")
		if _, _, ok := balanceGroup(lib, srv, settings, logger); ok {
			t.Fatal("balancing must stay off")
		}
	})
	t.Run("proxy mode", func(t *testing.T) {
		settings := setBalance(t, st, true, "proxy")
		group, addrs, ok := balanceGroup(lib, srv, settings, logger)
		if !ok {
			t.Fatal("balancing must work in proxy mode")
		}
		if len(group) != 2 {
			t.Fatalf("group = %d, want the two alive vless servers of the subscription", len(group))
		}
		if len(addrs) != 0 {
			t.Fatalf("proxy mode must not pre-resolve: %v", addrs)
		}
	})
	t.Run("dead member is dropped", func(t *testing.T) {
		settings := setBalance(t, st, true, "proxy")
		group, _, ok := balanceGroup(lib, srv, settings, logger)
		if !ok {
			t.Fatal("balancing must work")
		}
		for _, s := range group {
			if s.ID == "dead" {
				t.Fatal("a server that does not answer must not take a share of the connections")
			}
		}
	})
	t.Run("tun mode", func(t *testing.T) {
		settings := setBalance(t, st, true, "tun")
		group, addrs, ok := balanceGroup(lib, srv, settings, logger)
		if !ok || len(group) != 2 {
			t.Fatalf("group = %d ok=%v", len(group), ok)
		}
		if len(addrs) != 2 || addrs[0] != "127.0.0.1" || addrs[1] != "127.0.0.1" {
			t.Fatalf("addresses = %v", addrs)
		}
	})
	t.Run("full tunnel", func(t *testing.T) {
		settings := setBalance(t, st, true, "tun")
		if _, _, ok := balanceGroup(lib, lib.Store.Server("wg"), settings, logger); ok {
			t.Fatal("a full tunnel cannot be balanced")
		}
	})
	t.Run("manual servers", func(t *testing.T) {
		settings := setBalance(t, st, true, "proxy")
		if _, _, ok := balanceGroup(lib, lib.Store.Server("manual"), settings, logger); ok {
			t.Fatal("a single manual server has nothing to balance with")
		}
	})
	t.Run("single server in the subscription", func(t *testing.T) {
		settings := setBalance(t, st, true, "proxy")
		if _, _, ok := balanceGroup(lib, lib.Store.Server("other"), settings, logger); ok {
			t.Fatal("one server is not a group")
		}
	})
}
