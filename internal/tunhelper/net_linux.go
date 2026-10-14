//go:build linux

package tunhelper

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/vishvananda/netlink"
)

func hostMask(ip net.IP) net.IPMask {
	if ip.To4() != nil {
		return net.CIDRMask(32, 32)
	}
	return net.CIDRMask(128, 128)
}

// addHostRouteViaGateway routes ip through the physical adapter.
func addHostRouteViaGateway(ip, gw net.IP, iface string) (func(), error) {
	link, err := netlink.LinkByName(iface)
	if err != nil {
		return nil, err
	}
	r := &netlink.Route{LinkIndex: link.Attrs().Index, Dst: &net.IPNet{IP: ip, Mask: hostMask(ip)}}
	if gw != nil && (gw.To4() != nil) == (ip.To4() != nil) {
		r.Gw = gw
	}
	if err := netlink.RouteAdd(r); err != nil {
		if errors.Is(err, syscall.EEXIST) {
			return func() {}, nil // already routed; not ours to remove
		}
		return nil, err
	}
	return func() { _ = netlink.RouteDel(r) }, nil
}

// routeAllThrough sends all IPv4 and IPv6 traffic into dev. Two halves
// (0/1 + 128/1) override the default route without replacing it.
func routeAllThrough(dev string, logf func(string, ...any)) (func(), error) {
	link, err := netlink.LinkByName(dev)
	if err != nil {
		return nil, err
	}
	if err := netlink.LinkSetUp(link); err != nil {
		return nil, err
	}
	addr4, _ := netlink.ParseAddr("172.19.0.1/30")
	if err := netlink.AddrAdd(link, addr4); err != nil && !errors.Is(err, syscall.EEXIST) {
		return nil, fmt.Errorf("адрес интерфейса: %w", err)
	}

	var added []*netlink.Route
	add := func(cidr string) error {
		_, dst, _ := net.ParseCIDR(cidr)
		r := &netlink.Route{LinkIndex: link.Attrs().Index, Dst: dst, Scope: netlink.SCOPE_LINK}
		if err := netlink.RouteAdd(r); err != nil {
			return err
		}
		added = append(added, r)
		return nil
	}
	undo := func() {
		for _, r := range added {
			_ = netlink.RouteDel(r)
		}
	}
	for _, c := range []string{"0.0.0.0/1", "128.0.0.0/1"} {
		if err := add(c); err != nil {
			undo()
			return nil, fmt.Errorf("маршрут %s: %w", c, err)
		}
	}
	// IPv6 is optional, but without it v6 traffic would bypass the VPN.
	if addr6, err := netlink.ParseAddr("fdfe:dcba:9876::1/126"); err == nil {
		if err := netlink.AddrAdd(link, addr6); err == nil || errors.Is(err, syscall.EEXIST) {
			for _, c := range []string{"::/1", "8000::/1"} {
				if err := add(c); err != nil {
					logf("IPv6: %v", err)
					break
				}
			}
		} else {
			logf("IPv6: %v", err)
		}
	}
	return undo, nil
}

// routeDNSThrough makes system DNS go into the tunnel: with
// systemd-resolved the link becomes the default DNS route; otherwise the
// nameservers from /etc/resolv.conf are routed into dev.
func routeDNSThrough(dev, dns string, logf func(string, ...any)) func() {
	if path, err := exec.LookPath("resolvectl"); err == nil {
		steps := [][]string{{"dns", dev, dns}, {"domain", dev, "~."}, {"default-route", dev, "yes"}}
		ok := true
		for _, s := range steps {
			if out, err := exec.Command(path, s...).CombinedOutput(); err != nil {
				logf("resolvectl %s: %v %s", s[0], err, strings.TrimSpace(string(out)))
				ok = false
				break
			}
		}
		if ok {
			return func() { _ = exec.Command(path, "revert", dev).Run() }
		}
	}

	link, err := netlink.LinkByName(dev)
	if err != nil {
		return func() {}
	}
	var added []*netlink.Route
	for _, ip := range resolvConfServers() {
		if ip.IsLoopback() || ip.To4() == nil {
			continue
		}
		r := &netlink.Route{LinkIndex: link.Attrs().Index, Dst: &net.IPNet{IP: ip, Mask: hostMask(ip)}, Scope: netlink.SCOPE_LINK}
		if err := netlink.RouteAdd(r); err != nil {
			logf("DNS %s: %v", ip, err)
			continue
		}
		added = append(added, r)
	}
	return func() {
		for _, r := range added {
			_ = netlink.RouteDel(r)
		}
	}
}

func resolvConfServers() []net.IP {
	f, err := os.Open("/etc/resolv.conf")
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []net.IP
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) >= 2 && fields[0] == "nameserver" {
			if ip := net.ParseIP(fields[1]); ip != nil {
				out = append(out, ip)
			}
		}
	}
	return out
}

// elevate runs the helper as root through polkit (pkexec), which shows the
// desktop's password prompt.
func elevate(exe string, args []string) (*exec.Cmd, error) {
	pkexec, err := exec.LookPath("pkexec")
	if err != nil {
		return nil, errors.New("для режима «Весь трафик» нужен pkexec (пакет polkit)")
	}
	cmd := exec.Command(pkexec, append([]string{exe}, args...)...)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}
