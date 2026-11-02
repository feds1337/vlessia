//go:build darwin || linux

package main

import (
	"context"
	"os/exec"
	goruntime "runtime"

	"github.com/feds1337/vlessia/internal/profile"
	"github.com/feds1337/vlessia/internal/store"
	"github.com/feds1337/vlessia/internal/tun"
	"github.com/feds1337/vlessia/internal/tunhelper"
	"github.com/feds1337/vlessia/internal/xray"
)

func openFolder(path string) error {
	opener := "xdg-open"
	if goruntime.GOOS == "darwin" {
		opener = "open"
	}
	return exec.Command(opener, path).Start()
}

// prepareTUN: the client stays unprivileged. Its outbound sockets are bound
// to the physical adapter so they never loop into the tunnel, and the
// server address is pre-resolved (system DNS will go into the tunnel).
func (a *App) prepareTUN(srv *profile.Server, serverIP string, opts *xray.Options) error {
	iface, err := tun.DefaultInterface()
	if err != nil {
		return err
	}
	opts.Interface = iface
	opts.ServerAddress = serverIP
	a.log.Printf("TUN: физический адаптер %q, сервер %s -> %s", iface, srv.Address, serverIP)
	return nil
}

// activateTUN starts the privileged helper (the system asks for the
// administrator password), which routes all traffic into the client's
// SOCKS inbound. bypass are the tunnel endpoints that stay reachable
// around it.
func (a *App) activateTUN(bypass []string, settings store.Settings) (tunSession, error) {
	dns := "1.1.1.1"
	if len(settings.DNS) > 0 {
		dns = settings.DNS[0]
	}
	sess, err := tunhelper.Start(context.Background(), tunhelper.Config{
		SocksPort: settings.SocksPort,
		Bypass:    bypass,
		DNS:       dns,
		Logf:      a.log.Printf,
	})
	if err != nil {
		return nil, err // not a nil *Session wrapped in a non-nil interface
	}
	return sess, nil
}

// startBlockSession is the kill switch on macOS and Linux: the privileged
// helper stays up, but its Xray drops everything into a black hole instead
// of forwarding it to the client.
func (a *App) startBlockSession(settings store.Settings) (blockSession, error) {
	dns := "1.1.1.1"
	if len(settings.DNS) > 0 {
		dns = settings.DNS[0]
	}
	sess, err := tunhelper.Start(context.Background(), tunhelper.Config{
		SocksPort: settings.SocksPort,
		DNS:       dns,
		Block:     true,
		Logf:      a.log.Printf,
	})
	if err != nil {
		return nil, err
	}
	return sess, nil
}
