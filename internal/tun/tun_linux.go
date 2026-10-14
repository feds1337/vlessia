//go:build linux

package tun

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"syscall"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

// AdapterName is the TUN device created by the helper.
const AdapterName = "vlessia0"

// DefaultRoute returns the interface and gateway currently used to reach
// the internet.
func DefaultRoute() (string, net.IP, error) {
	routes, err := netlink.RouteGet(net.ParseIP("1.1.1.1"))
	if err != nil || len(routes) == 0 {
		return "", nil, fmt.Errorf("не найден сетевой адаптер с выходом в интернет: %v", err)
	}
	link, err := netlink.LinkByIndex(routes[0].LinkIndex)
	if err != nil {
		return "", nil, err
	}
	name := link.Attrs().Name
	if name == AdapterName {
		return "", nil, errors.New("маршрут по умолчанию уже указывает в TUN — отключите другой VPN")
	}
	return name, routes[0].Gw, nil
}

// BindToInterface returns a net.Dialer Control func that pins the socket to
// the given adapter (SO_BINDTODEVICE), bypassing the TUN routes.
func BindToInterface(name string) func(network, address string, c syscall.RawConn) error {
	return func(network, address string, c syscall.RawConn) error {
		var serr error
		err := c.Control(func(fd uintptr) {
			serr = unix.BindToDevice(int(fd), name)
		})
		if err != nil {
			return err
		}
		if serr != nil && strings.Contains(serr.Error(), "operation not permitted") {
			return fmt.Errorf("привязка к адаптеру %s запрещена (нужно ядро Linux 5.7+): %w", name, serr)
		}
		return serr
	}
}
