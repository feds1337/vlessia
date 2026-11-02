package core

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"sync"
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
	bridge2  Bridge // the relay's (via) tunnel, when it is a full tunnel
	ServerIP string // resolved server address, "" if not needed
	// Interface is the physical adapter outbound sockets are bound to in
	// TUN mode on desktop systems.
	Interface string
	// Bypass lists the IPs of every tunnel endpoint (the server, the relay,
	// the balanced servers) that must stay reachable around the TUN device.
	Bypass  []string
	release func()
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
		SocksPort:   settings.SocksPort,
		HTTPPort:    settings.HTTPPort,
		BypassRU:    settings.BypassRU,
		DirectRules: settings.DirectRules,
		ProxyRules:  settings.ProxyRules,
		DNS:         settings.DNS,
		LogFile:     filepath.Join(dir, "xray.log"),
		LogLevel:    settings.LogLevel,
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

	// Multi-tunnel: the relay the connection to srv goes through.
	var (
		via       *profile.Server
		viaBridge Bridge
		viaIP     string
	)
	if srv.Via != "" {
		via = lib.Store.Server(srv.Via)
		if via == nil || via.ID == srv.ID {
			logger.Printf("цепочка: сервер «%s» не найден, подключаюсь напрямую", srv.Via)
			srv.Via = ""
			via = nil
		}
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
		e.Bypass = append(e.Bypass, ip)
	}

	if via != nil {
		if settings.Mode == "tun" || via.IsFullTunnel() || via.Protocol == "hysteria2" {
			ip, err := Resolve(via.Address)
			if err != nil {
				return nil, fmt.Errorf("не удалось найти IP реле %s: %w", via.Address, err)
			}
			viaIP = ip
			e.Bypass = append(e.Bypass, ip)
		}
		if via.Protocol == "hysteria2" && via.Insecure && via.PinnedSHA == "" {
			// Runs before TUN is up, so no adapter binding is needed.
			res, err := hysteria.Probe(context.Background(), via, viaIP, nil)
			if err != nil {
				return nil, fmt.Errorf("Hysteria: релей не отвечает: %w", err)
			}
			via.PinnedSHA = res.CertSHA256
			logger.Printf("Hysteria: релей с insecure=1, закреплён его сертификат %s", via.PinnedSHA)
		}
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

	// The relay's own tunnel, when it is a full-tunnel protocol.
	if via != nil {
		if via.OVPN != nil {
			var control func(string, string, syscall.RawConn) error
			if opts.Interface != "" {
				control = tun.BindToInterface(opts.Interface)
			}
			t, err := openvpn.Start(via.OVPN, viaIP, control, logger.Printf)
			if err != nil {
				e.Close()
				return nil, err
			}
			viaBridge = t
			opts.ViaBridgePort = t.Port()
			logger.Printf("openvpn: релей к %s:%d, локальный мост 127.0.0.1:%d", viaIP, via.Port, t.Port())
		}
		if via.WG != nil {
			awg.Verbose = settings.LogLevel == "debug"
			endpoint, err := netip.ParseAddrPort(net.JoinHostPort(viaIP, strconv.Itoa(via.Port)))
			if err != nil {
				e.Close()
				return nil, err
			}
			bindIdx := 0
			if opts.Interface != "" {
				if inf, err := net.InterfaceByName(opts.Interface); err == nil {
					bindIdx = inf.Index
				}
			}
			t, err := awg.Start(via.WG, endpoint, bindIdx, logger.Printf)
			if err != nil {
				e.Close()
				return nil, fmt.Errorf("AmneziaWG: релей: %w", err)
			}
			viaBridge = t
			opts.ViaBridgePort = t.Port()
			logger.Printf("%s: релей к %s, локальный мост 127.0.0.1:%d", via.Protocol, endpoint, t.Port())
		}
		opts.Via = via
		opts.ViaAddr = viaIP
		e.bridge2 = viaBridge
		logger.Printf("цепочка: %s -> %s", srv.Name, via.Name)
	}

	// Balance spreads the connections across the servers of the selected
	// one's subscription. Full tunnels cannot be balanced: each needs its
	// own bridge, so the group is restricted to Xray-native protocols.
	if settings.Balance && !srv.IsFullTunnel() {
		var group []*profile.Server
		for _, s := range lib.AllServers() {
			if s.SubID == srv.SubID && !s.IsFullTunnel() {
				group = append(group, s)
			}
		}
		if len(group) < 2 {
			logger.Printf("балансировка: в группе меньше двух серверов, подключаю напрямую")
		} else {
			var addrs []string
			if settings.Mode == "tun" {
				// Once TUN is up the system DNS goes into the tunnel, so
				// every balanced server is resolved beforehand, in parallel.
				addrs = make([]string, len(group))
				var (
					mu sync.Mutex
					f  error
					wg sync.WaitGroup
				)
				for i, s := range group {
					wg.Add(1)
					go func(i int, s *profile.Server) {
						defer wg.Done()
						ip, err := Resolve(s.Address)
						if err != nil {
							mu.Lock()
							f = errors.Join(f, fmt.Errorf("%s: %w", s.Address, err))
							mu.Unlock()
							return
						}
						addrs[i] = ip
					}(i, s)
				}
				wg.Wait()
				if f != nil {
					logger.Printf("балансировка: не удалось разрешить адреса (%v), подключаю напрямую", f)
					addrs = nil
				}
			}
			if addrs != nil {
				opts.Balance = group
				opts.BalanceAddrs = addrs
				e.Bypass = append(e.Bypass, addrs...)
			}
		}
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

// CloseBridge stops the userspace tunnels, if any.
func (e *Engine) CloseBridge() {
	if e.Bridge != nil {
		e.Bridge.Close()
		e.Bridge = nil
	}
	if e.bridge2 != nil {
		e.bridge2.Close()
		e.bridge2 = nil
	}
}

// Close stops everything.
func (e *Engine) Close() error {
	err := e.CloseXray()
	e.CloseBridge()
	return err
}
