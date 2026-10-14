package core

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/feds1337/vlessia/internal/awg"
	"github.com/feds1337/vlessia/internal/hysteria"
	"github.com/feds1337/vlessia/internal/openvpn"
	"github.com/feds1337/vlessia/internal/profile"
	"github.com/feds1337/vlessia/internal/store"
	"github.com/feds1337/vlessia/internal/tun"
	"github.com/feds1337/vlessia/internal/xray"
)

// Bridge is a full-tunnel protocol running in userspace and exposed to
// Xray as a local SOCKS5 server.
type Bridge interface {
	Port() int
	Close()
}

// Engine is a running connection: Xray and, for WireGuard, AmneziaWG and
// OpenVPN, the userspace tunnel behind it.
type Engine struct {
	Inst     *xray.Instance
	Bridge   Bridge
	ServerIP string // resolved server address, "" if not needed
	// Interface is the physical adapter outbound sockets are bound to in
	// TUN mode on desktop systems.
	Interface string
	release   func()
}

// Hooks are the platform-specific steps of starting an engine.
type Hooks struct {
	// PrepareTUN runs in TUN mode once the server address is resolved and
	// before anything connects; it sets the TUN options and, on desktop
	// systems, binds outbound sockets to the physical adapter.
	PrepareTUN func(serverIP string, opts *xray.Options) error
	// BeforeXray runs right before Xray starts, e.g. to hand over a TUN
	// device. release is called after Xray has stopped.
	BeforeXray func(opts *xray.Options) (release func(), err error)
}

// EffectiveMode returns the mode a server actually runs in. WireGuard,
// AmneziaWG and OpenVPN are full tunnels by nature, so they always use TUN.
func EffectiveMode(srv *profile.Server, mode string) string {
	if srv.IsFullTunnel() {
		return "tun"
	}
	return mode
}

// StartEngine launches everything needed for srv in settings.Mode: the
// WireGuard/AmneziaWG/OpenVPN bridge if any, then Xray. dir receives
// xray.log and xray-config.json. srv is a copy and may be modified.
func StartEngine(lib *Library, srv *profile.Server, settings store.Settings, dir string, hooks Hooks) (*Engine, error) {
	logger := lib.Log
	opts := xray.Options{
		SocksPort: settings.SocksPort,
		HTTPPort:  settings.HTTPPort,
		BypassRU:  settings.BypassRU,
		DNS:       settings.DNS,
		LogFile:   filepath.Join(dir, "xray.log"),
		LogLevel:  settings.LogLevel,
	}

	// Ask for the OpenVPN login before touching the network.
	if srv.OVPN != nil && srv.OVPN.NeedAuth && srv.OVPN.Username == "" {
		return nil, openvpn.ErrNeedAuth
	}
	if srv.AccessKeyURL != "" {
		lib.RefreshOutlineKey(srv)
	}
	if srv.Prefix != "" {
		logger.Printf("Outline: маскировка prefix не поддерживается Xray, подключение без неё")
	}

	// Resolve the server now: once TUN is up, system DNS would go into the
	// tunnel that needs this very address to work. WireGuard and the
	// Hysteria probe need an IP anyway.
	e := &Engine{}
	if settings.Mode == "tun" || srv.IsFullTunnel() || srv.Protocol == "hysteria2" {
		ip, err := Resolve(srv.Address)
		if err != nil {
			return nil, fmt.Errorf("не удалось найти IP сервера %s: %w", srv.Address, err)
		}
		e.ServerIP = ip
	}

	if settings.Mode == "tun" && hooks.PrepareTUN != nil {
		if err := hooks.PrepareTUN(e.ServerIP, &opts); err != nil {
			return nil, err
		}
		e.Interface = opts.Interface
	}

	if srv.Protocol == "hysteria2" && srv.Insecure && srv.PinnedSHA == "" {
		// Runs before TUN is up, so no adapter binding is needed.
		res, err := hysteria.Probe(context.Background(), srv, e.ServerIP, nil)
		if err != nil {
			return nil, fmt.Errorf("Hysteria: сервер не отвечает: %w", err)
		}
		srv.PinnedSHA = res.CertSHA256
		logger.Printf("Hysteria: ссылка с insecure=1, закреплён текущий сертификат сервера %s", res.CertSHA256)
	}

	if srv.OVPN != nil {
		var control func(string, string, syscall.RawConn) error
		if opts.Interface != "" {
			control = tun.BindToInterface(opts.Interface)
		}
		t, err := openvpn.Start(srv.OVPN, e.ServerIP, control, logger.Printf)
		if err != nil {
			return nil, err
		}
		e.Bridge = t
		opts.BridgePort = t.Port()
		logger.Printf("openvpn: туннель к %s:%d, локальный мост 127.0.0.1:%d", e.ServerIP, srv.Port, t.Port())
	}
	if srv.WG != nil {
		awg.Verbose = settings.LogLevel == "debug"
		endpoint, err := netip.ParseAddrPort(net.JoinHostPort(e.ServerIP, strconv.Itoa(srv.Port)))
		if err != nil {
			return nil, err
		}
		bindIdx := 0
		if opts.Interface != "" {
			if inf, err := net.InterfaceByName(opts.Interface); err == nil {
				bindIdx = inf.Index
			}
		}
		t, err := awg.Start(srv.WG, endpoint, bindIdx, logger.Printf)
		if err != nil {
			return nil, fmt.Errorf("AmneziaWG: %w", err)
		}
		e.Bridge = t
		opts.BridgePort = t.Port()
		logger.Printf("%s: туннель к %s, локальный мост 127.0.0.1:%d", srv.Protocol, endpoint, t.Port())
	}

	cfg, err := xray.BuildConfig(srv, opts)
	if err != nil {
		e.Close()
		return nil, err
	}
	// Xray appends to its log; keep only the current session.
	_ = os.Truncate(opts.LogFile, 0)
	_ = os.WriteFile(filepath.Join(dir, "xray-config.json"), cfg, 0o600)

	if hooks.BeforeXray != nil {
		release, err := hooks.BeforeXray(&opts)
		if err != nil {
			e.Close()
			return nil, err
		}
		e.release = release
	}
	inst, err := xray.Start(cfg)
	if err != nil {
		e.Close()
		return nil, err
	}
	e.Inst = inst
	return e, nil
}

// CloseXray stops Xray and releases what BeforeXray handed over.
func (e *Engine) CloseXray() error {
	var err error
	if e.Inst != nil {
		err = e.Inst.Close()
	}
	if e.release != nil {
		e.release()
		e.release = nil
	}
	return err
}

// CloseBridge stops the userspace tunnel, if any.
func (e *Engine) CloseBridge() {
	if e.Bridge != nil {
		e.Bridge.Close()
	}
}

// Close stops everything.
func (e *Engine) Close() error {
	err := e.CloseXray()
	e.CloseBridge()
	return err
}
