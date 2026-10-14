//go:build darwin

package tun

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// AdapterName is the utun device created by the helper (xray requires the
// utunN form; a high number avoids clashing with system tunnels).
const AdapterName = "utun99"

// DefaultRoute returns the interface and gateway currently used to reach
// the internet, from `route -n get`.
func DefaultRoute() (string, net.IP, error) {
	out, err := exec.Command("/sbin/route", "-n", "get", "1.1.1.1").Output()
	if err != nil {
		return "", nil, fmt.Errorf("не найден сетевой адаптер с выходом в интернет: %w", err)
	}
	var iface string
	var gw net.IP
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		k, v, ok := strings.Cut(strings.TrimSpace(sc.Text()), ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(k) {
		case "interface":
			iface = strings.TrimSpace(v)
		case "gateway":
			gw = net.ParseIP(strings.TrimSpace(v))
		}
	}
	if iface == "" {
		return "", nil, errors.New("не найден сетевой адаптер с выходом в интернет")
	}
	if iface == AdapterName {
		return "", nil, errors.New("маршрут по умолчанию уже указывает в TUN — отключите другой VPN")
	}
	return iface, gw, nil
}

// BindToInterface returns a net.Dialer Control func that pins the socket to
// the given adapter (IP_BOUND_IF / IPV6_BOUND_IF), bypassing the TUN routes.
func BindToInterface(name string) func(network, address string, c syscall.RawConn) error {
	return func(network, address string, c syscall.RawConn) error {
		inf, err := net.InterfaceByName(name)
		if err != nil {
			return err
		}
		var serr error
		err = c.Control(func(fd uintptr) {
			if strings.Contains(address, ".") || strings.HasSuffix(network, "4") {
				serr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_BOUND_IF, inf.Index)
			} else {
				serr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IPV6, unix.IPV6_BOUND_IF, inf.Index)
			}
		})
		if err != nil {
			return err
		}
		return serr
	}
}
