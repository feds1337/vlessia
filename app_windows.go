package main

import (
	"context"
	"os/exec"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/feds1337/vlessia/internal/profile"
	"github.com/feds1337/vlessia/internal/store"
	"github.com/feds1337/vlessia/internal/tun"
	"github.com/feds1337/vlessia/internal/xray"
)

// hideOnClose: closing the window keeps the client running in the tray.
func hideOnClose(ctx context.Context) { runtime.WindowHide(ctx) }

func openFolder(path string) error { return exec.Command("explorer", path).Start() }

// prepareTUN: on Windows the client itself creates the Wintun adapter, so
// it must run elevated.
func (a *App) prepareTUN(srv *profile.Server, serverIP string, opts *xray.Options) error {
	if !tun.IsAdmin() {
		return ErrNeedAdmin
	}
	if err := tun.EnsureWintun(a.store.Dir()); err != nil {
		return err
	}
	iface, err := tun.DefaultInterface()
	if err != nil {
		return err
	}
	opts.TUN = true
	opts.TunName = tun.AdapterName
	opts.Interface = iface
	opts.ServerAddress = serverIP
	a.log.Printf("TUN: физический адаптер %q, сервер %s -> %s", iface, srv.Address, serverIP)
	return nil
}

type winTunSession struct{}

func (winTunSession) Close()              { tun.Deconfigure() }
func (winTunSession) Done() <-chan struct{} { return nil }

// activateTUN configures the adapter Xray has created and returns a session
// whose Close removes the TUN routes and DNS settings before Xray stops.
func (a *App) activateTUN(_ string, settings store.Settings) (tunSession, error) {
	if err := tun.Configure(settings.DNS, a.log.Printf); err != nil {
		return nil, err
	}
	return winTunSession{}, nil
}
