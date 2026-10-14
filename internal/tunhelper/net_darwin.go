//go:build darwin

package tunhelper

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"os/exec"
	"strings"
)

const routeBin = "/sbin/route"

func route(args ...string) error {
	out, err := exec.Command(routeBin, append([]string{"-n"}, args...)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("route %s: %v %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func family(ip net.IP) []string {
	if ip.To4() == nil {
		return []string{"-inet6"}
	}
	return nil
}

// addHostRouteViaGateway routes ip through the physical adapter.
func addHostRouteViaGateway(ip, gw net.IP, iface string) (func(), error) {
	args := append([]string{"add"}, family(ip)...)
	args = append(args, "-host", ip.String())
	if gw != nil && (gw.To4() != nil) == (ip.To4() != nil) {
		args = append(args, gw.String())
	} else {
		args = append(args, "-interface", iface)
	}
	if err := route(args...); err != nil {
		if strings.Contains(err.Error(), "File exists") {
			return func() {}, nil // already routed; not ours to remove
		}
		return nil, err
	}
	del := append(append([]string{"delete"}, family(ip)...), "-host", ip.String())
	return func() { _ = route(del...) }, nil
}

// routeAllThrough sends all IPv4 and IPv6 traffic into dev. Xray has
// already assigned the utun addresses; two halves (0/1 + 128/1) override
// the default route without replacing it.
func routeAllThrough(dev string, logf func(string, ...any)) (func(), error) {
	type r struct{ fam, net string }
	var added []r
	undo := func() {
		for _, x := range added {
			args := []string{"delete"}
			if x.fam != "" {
				args = append(args, x.fam)
			}
			_ = route(append(args, "-net", x.net, "-interface", dev)...)
		}
	}
	for _, x := range []r{{"", "0.0.0.0/1"}, {"", "128.0.0.0/1"}, {"-inet6", "::/1"}, {"-inet6", "8000::/1"}} {
		args := []string{"add"}
		if x.fam != "" {
			args = append(args, x.fam)
		}
		if err := route(append(args, "-net", x.net, "-interface", dev)...); err != nil {
			if x.fam != "" {
				logf("IPv6: %v", err) // optional
				continue
			}
			undo()
			return nil, err
		}
		added = append(added, x)
	}
	return undo, nil
}

// routeDNSThrough routes the system DNS servers (from scutil --dns) into
// the tunnel, where port 53 is answered through the VPN.
func routeDNSThrough(dev, _ string, logf func(string, ...any)) func() {
	out, err := exec.Command("/usr/sbin/scutil", "--dns").Output()
	if err != nil {
		logf("scutil --dns: %v", err)
		return func() {}
	}
	seen := map[string]bool{}
	var added []string
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		k, v, ok := strings.Cut(strings.TrimSpace(sc.Text()), ":")
		if !ok || !strings.HasPrefix(strings.TrimSpace(k), "nameserver[") {
			continue
		}
		ip := net.ParseIP(strings.TrimSpace(v))
		if ip == nil || ip.To4() == nil || ip.IsLoopback() || seen[ip.String()] {
			continue
		}
		seen[ip.String()] = true
		if err := route("add", "-host", ip.String(), "-interface", dev); err != nil {
			logf("DNS %s: %v", ip, err)
			continue
		}
		added = append(added, ip.String())
	}
	return func() {
		for _, ip := range added {
			_ = route("delete", "-host", ip)
		}
	}
}

// elevate runs the helper as root through osascript, which shows the
// standard macOS administrator password prompt.
func elevate(exe string, args []string) (*exec.Cmd, error) {
	quoted := []string{shellQuote(exe)}
	for _, a := range args {
		quoted = append(quoted, shellQuote(a))
	}
	script := fmt.Sprintf(`do shell script "%s" with prompt "Vlessia включает режим «Весь трафик»." with administrator privileges`,
		appleScriptEscape(strings.Join(quoted, " ")))
	cmd := exec.Command("/usr/bin/osascript", "-e", script)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func appleScriptEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
}
