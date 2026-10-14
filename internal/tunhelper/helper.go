//go:build darwin || linux

// Package tunhelper implements "Весь трафик" (TUN) mode on macOS and Linux.
//
// The GUI client runs as the user. For TUN mode it starts its own
// executable with --tun-helper as root (osascript on macOS, pkexec on
// Linux). The helper creates the TUN device with Xray, routes all traffic
// into it and forwards it to the client's local SOCKS inbound, so the
// client keeps doing the actual proxying. Routes to the VPN servers go
// around the tunnel via the physical gateway.
//
// The helper connects back to a loopback control port opened by the client
// and proves itself with a one-time token. It reports "log …", then
// "ready" or "error …" lines; when the client closes the connection (or
// dies) the helper removes its routes and exits.
package tunhelper

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/feds1337/vlessia/internal/tun"
	"github.com/feds1337/vlessia/internal/xray"
)

// Arg selects helper mode on the command line.
const Arg = "--tun-helper"

// Run is the helper's main; it returns the process exit code.
func Run(args []string) int {
	fs := flag.NewFlagSet("tun-helper", flag.ContinueOnError)
	control := fs.String("control", "", "client control address")
	token := fs.String("token", "", "one-time token")
	socks := fs.Int("socks", 0, "client SOCKS port")
	bypass := fs.String("bypass", "", "comma-separated server IPs routed around the tunnel")
	dns := fs.String("dns", "1.1.1.1", "DNS server resolved through the tunnel")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *control == "" || *token == "" || *socks == 0 {
		fmt.Fprintln(os.Stderr, "tun-helper: missing arguments")
		return 2
	}
	if host, _, err := net.SplitHostPort(*control); err != nil || !net.ParseIP(host).IsLoopback() {
		fmt.Fprintln(os.Stderr, "tun-helper: control address must be loopback")
		return 2
	}

	conn, err := net.DialTimeout("tcp", *control, 10*time.Second)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tun-helper:", err)
		return 1
	}
	defer conn.Close()
	send := func(format string, a ...any) { fmt.Fprintf(conn, format+"\n", a...) }
	send("%s", *token)
	logf := func(format string, a ...any) { send("log "+format, a...) }

	cleanup, err := start(*socks, splitIPs(*bypass), *dns, logf)
	if err != nil {
		send("error %s", strings.ReplaceAll(err.Error(), "\n", " "))
		return 1
	}
	send("ready")

	// Run until the client hangs up or the helper is told to stop.
	done := make(chan struct{})
	go func() {
		_, _ = bufio.NewReader(conn).ReadString('\n')
		close(done)
	}()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	select {
	case <-done:
	case <-sig:
	}
	cleanup()
	return 0
}

func splitIPs(s string) []net.IP {
	var out []net.IP
	for _, p := range strings.Split(s, ",") {
		if ip := net.ParseIP(strings.TrimSpace(p)); ip != nil {
			out = append(out, ip)
		}
	}
	return out
}

// start brings TUN mode up and returns the function that tears it down.
func start(socksPort int, bypass []net.IP, dns string, logf func(string, ...any)) (func(), error) {
	iface, gw, err := tun.DefaultRoute()
	if err != nil {
		return nil, err
	}
	logf("физический адаптер %s, шлюз %v", iface, gw)

	var undo []func()
	cleanup := func() {
		for i := len(undo) - 1; i >= 0; i-- {
			undo[i]()
		}
	}

	// VPN servers stay reachable around the tunnel.
	for _, ip := range bypass {
		del, err := addHostRouteViaGateway(ip, gw, iface)
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("маршрут к серверу %s: %w", ip, err)
		}
		undo = append(undo, del)
	}

	inst, err := xray.Start(helperConfig(socksPort, dns))
	if err != nil {
		cleanup()
		return nil, err
	}
	undo = append(undo, func() { _ = inst.Close() })

	if err := waitInterface(tun.AdapterName, 10*time.Second); err != nil {
		cleanup()
		return nil, err
	}
	del, err := routeAllThrough(tun.AdapterName, logf)
	if err != nil {
		cleanup()
		return nil, err
	}
	undo = append(undo, del)

	// System DNS servers (often the router, on the local network) would be
	// reached directly; route them into the tunnel, where port 53 is
	// answered by Xray's DNS through the VPN.
	undo = append(undo, routeDNSThrough(tun.AdapterName, dns, logf))
	return cleanup, nil
}

// helperConfig: TUN inbound -> client's SOCKS inbound; plain DNS from the
// tunnel is answered by Xray's DNS, whose queries also go to the client.
func helperConfig(socksPort int, dns string) []byte {
	type obj = map[string]any
	cfg := obj{
		"log": obj{"loglevel": "warning", "access": "none"},
		"dns": obj{"servers": []string{dns}, "queryStrategy": "UseIPv4"},
		"inbounds": []obj{{
			"tag": "tun-in", "port": 0, "protocol": "tun",
			"settings": obj{"name": tun.AdapterName, "MTU": 1500},
		}},
		"outbounds": []obj{
			{"tag": "client", "protocol": "socks", "settings": obj{
				"servers": []obj{{"address": "127.0.0.1", "port": socksPort}},
			}},
			{"tag": "dns-out", "protocol": "dns"},
		},
		"routing": obj{"rules": []obj{
			{"type": "field", "inboundTag": []string{"tun-in"}, "port": "53", "outboundTag": "dns-out"},
		}},
	}
	b, _ := json.Marshal(cfg)
	return b
}

func waitInterface(name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := net.InterfaceByName(name); err == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("интерфейс %s не появился", name)
}

// ---------- client side ----------

// Session is a running helper, seen from the client.
type Session struct {
	conn net.Conn
	cmd  *exec.Cmd
	done chan struct{}
	err  error
}

// Config of a helper session.
type Config struct {
	SocksPort int
	Bypass    []string // VPN server IPs
	DNS       string
	Logf      func(string, ...any)
}

// ErrCancelled means the user dismissed the password prompt.
var ErrCancelled = errors.New("запрос пароля отменён")

// Start launches the helper with administrator rights (the system shows a
// password prompt) and waits until TUN mode is up.
func Start(ctx context.Context, cfg Config) (*Session, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	defer ln.Close()
	token := randomToken()

	args := []string{Arg,
		"--control", ln.Addr().String(),
		"--token", token,
		"--socks", fmt.Sprint(cfg.SocksPort),
		"--bypass", strings.Join(cfg.Bypass, ","),
		"--dns", cfg.DNS,
	}
	cmd, err := elevate(exe, args)
	if err != nil {
		return nil, err
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	// The user may take a while to type the password.
	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err == nil {
			accepted <- c
		}
	}()
	var conn net.Conn
	select {
	case conn = <-accepted:
	case err := <-exited:
		if err != nil {
			return nil, ErrCancelled
		}
		return nil, errors.New("помощник TUN завершился, не подключившись")
	case <-time.After(3 * time.Minute):
		return nil, errors.New("истекло время ожидания пароля")
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	r := bufio.NewReader(conn)
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	if line, err := r.ReadString('\n'); err != nil || strings.TrimSpace(line) != token {
		conn.Close()
		return nil, errors.New("помощник TUN не прошёл проверку")
	}
	// Bringing the tunnel up includes starting Xray; allow some time.
	_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
wait:
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("помощник TUN: %w", err)
		}
		line = strings.TrimSpace(line)
		switch {
		case line == "ready":
			break wait
		case strings.HasPrefix(line, "error "):
			conn.Close()
			return nil, errors.New(strings.TrimPrefix(line, "error "))
		case strings.HasPrefix(line, "log "):
			cfg.Logf("tun-helper: %s", strings.TrimPrefix(line, "log "))
		}
	}
	_ = conn.SetReadDeadline(time.Time{})

	s := &Session{conn: conn, cmd: cmd, done: make(chan struct{})}
	go func() {
		// Further lines are logs; EOF means the helper is gone.
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				s.err = err
				close(s.done)
				return
			}
			cfg.Logf("tun-helper: %s", strings.TrimPrefix(strings.TrimSpace(line), "log "))
		}
	}()
	return s, nil
}

// Done is closed when the helper exits on its own.
func (s *Session) Done() <-chan struct{} { return s.done }

// Close tells the helper to tear TUN mode down and waits for it to exit.
func (s *Session) Close() {
	_ = s.conn.Close()
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
	}
}

func randomToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
