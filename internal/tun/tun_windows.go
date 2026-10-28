//go:build windows

// Package tun prepares Windows for Xray's TUN inbound: ships wintun.dll,
// finds the physical uplink, configures the adapter and handles elevation.
package tun

import (
	"crypto/md5"
	_ "embed"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/text/encoding/charmap"
	"golang.zx2c4.com/wintun"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
)

// AdapterName is the name of the Wintun adapter created by Xray.
const AdapterName = "Vlessia"

const (
	adapterIPv4 = "172.19.0.1"
	adapterMask = "255.255.255.252"
	adapterIPv6 = "fdfe:dcba:9876::1/126"
)

//go:embed wintun.dll
var wintunDLL []byte

// EnsureWintun makes wintun.dll loadable. The wintun package only searches
// the application directory and System32, so the DLL is written next to the
// executable; if that directory is read-only it is preloaded from fallbackDir.
func EnsureWintun(fallbackDir string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	target := filepath.Join(filepath.Dir(exe), "wintun.dll")
	if sameFile(target) {
		return nil
	}
	if err := os.WriteFile(target, wintunDLL, 0o644); err == nil {
		return nil
	}
	alt := filepath.Join(fallbackDir, "wintun.dll")
	if !sameFile(alt) {
		if err := os.WriteFile(alt, wintunDLL, 0o644); err != nil {
			return fmt.Errorf("не удалось сохранить wintun.dll: %w", err)
		}
	}
	// A module already loaded under the same base name satisfies later loads.
	if _, err := windows.LoadLibrary(alt); err != nil {
		return fmt.Errorf("не удалось загрузить wintun.dll: %w", err)
	}
	return nil
}

func sameFile(path string) bool {
	b, err := os.ReadFile(path)
	return err == nil && len(b) == len(wintunDLL) && string(b) == string(wintunDLL)
}

// DefaultInterface returns the name of the adapter currently used to reach
// the internet. Must be called before the TUN routes are installed.
func DefaultInterface() (string, error) {
	sa := &windows.SockaddrInet4{Addr: [4]byte{1, 1, 1, 1}}
	cleaned := false
	deadline := time.Now().Add(2 * time.Second)
	for {
		var idx uint32
		if err := windows.GetBestInterfaceEx(sa, &idx); err != nil {
			return "", fmt.Errorf("не найден сетевой адаптер с выходом в интернет: %w", err)
		}
		inf, err := net.InterfaceByIndex(int(idx))
		if err != nil {
			return "", err
		}
		if inf.Name != AdapterName {
			return inf.Name, nil
		}
		if !cleaned {
			cleaned = true
			Cleanup()
			continue
		}
		if !time.Now().Before(deadline) {
			return "", errors.New("маршрут по умолчанию уже указывает в TUN — отключите другой VPN")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// Configure assigns addresses, DNS and routes to the TUN adapter once Xray
// has created it.
func Configure(dns []string, logf func(string, ...any)) error {
	if err := waitInterface(AdapterName, 10*time.Second); err != nil {
		return err
	}
	name := "name=" + AdapterName
	iface := "interface=" + AdapterName

	steps := [][]string{
		{"interface", "ipv4", "set", "address", name, "source=static", "address=" + adapterIPv4, "mask=" + adapterMask, "store=active"},
		{"interface", "ipv4", "set", "interface", iface, "metric=1"},
		{"interface", "ipv4", "add", "route", "prefix=0.0.0.0/1", iface, "nexthop=0.0.0.0", "metric=1", "store=active"},
		{"interface", "ipv4", "add", "route", "prefix=128.0.0.0/1", iface, "nexthop=0.0.0.0", "metric=1", "store=active"},
	}
	if len(dns) > 0 {
		steps = append(steps, []string{"interface", "ipv4", "set", "dnsservers", name, "source=static", "address=" + dns[0], "register=none", "validate=no"})
	}
	for _, args := range steps {
		if err := netsh(args...); err != nil {
			Deconfigure()
			return err
		}
	}

	// IPv6 is optional: without it v6 traffic would bypass the tunnel, so
	// route it into TUN too, but don't fail the connection if it can't be set.
	v6 := [][]string{
		{"interface", "ipv6", "add", "address", iface, "address=" + adapterIPv6, "store=active"},
		{"interface", "ipv6", "set", "interface", iface, "metric=1"},
		{"interface", "ipv6", "add", "route", "prefix=::/1", iface, "nexthop=::", "metric=1", "store=active"},
		{"interface", "ipv6", "add", "route", "prefix=8000::/1", iface, "nexthop=::", "metric=1", "store=active"},
	}
	for _, args := range v6 {
		if err := netsh(args...); err != nil {
			logf("IPv6: %v", err)
			break
		}
	}
	FlushDNS()
	return nil
}

// Deconfigure removes routes, IP addresses, DNS servers and metric overrides
// from the Vlessia TUN adapter so traffic returns to the physical interface
// immediately, even before the Wintun adapter is destroyed.
func Deconfigure() {
	for _, luid := range adapterLUIDs() {
		_ = luid.FlushRoutes(windows.AF_INET)
		_ = luid.FlushIPAddresses(windows.AF_INET)
		_ = luid.FlushDNS(windows.AF_INET)
		_ = luid.FlushRoutes(windows.AF_INET6)
		_ = luid.FlushIPAddresses(windows.AF_INET6)
		_ = luid.FlushDNS(windows.AF_INET6)
		for _, family := range []winipcfg.AddressFamily{windows.AF_INET, windows.AF_INET6} {
			if ipif, err := luid.IPInterface(family); err == nil {
				ipif.UseAutomaticMetric = true
				_ = ipif.Set()
			}
		}
	}
	name := "name=" + AdapterName
	iface := "interface=" + AdapterName
	for _, args := range [][]string{
		{"interface", "ipv4", "delete", "route", "prefix=0.0.0.0/1", iface, "store=active"},
		{"interface", "ipv4", "delete", "route", "prefix=128.0.0.0/1", iface, "store=active"},
		{"interface", "ipv6", "delete", "route", "prefix=::/1", iface, "store=active"},
		{"interface", "ipv6", "delete", "route", "prefix=8000::/1", iface, "store=active"},
		{"interface", "ipv4", "set", "dnsservers", name, "source=dhcp", "validate=no"},
	} {
		_ = hiddenCmd("netsh", args...).Run()
	}
}

// Cleanup removes TUN routes/DNS, deletes any leftover Wintun adapter and
// flushes the Windows DNS cache.
func Cleanup() {
	Deconfigure()
	if ad, err := wintun.OpenAdapter(AdapterName); err == nil {
		_ = ad.Close()
	}
	sa := &windows.SockaddrInet4{Addr: [4]byte{1, 1, 1, 1}}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		var idx uint32
		if windows.GetBestInterfaceEx(sa, &idx) != nil {
			break
		}
		inf, err := net.InterfaceByIndex(int(idx))
		if err != nil || inf.Name != AdapterName {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	FlushDNS()
}

func adapterLUIDs() []winipcfg.LUID {
	var luids []winipcfg.LUID
	add := func(l winipcfg.LUID) {
		if l == 0 {
			return
		}
		for _, existing := range luids {
			if existing == l {
				return
			}
		}
		luids = append(luids, l)
	}
	// Xray's WindowsTun derives the adapter GUID deterministically from MD5(name).
	id := md5.Sum([]byte(AdapterName))
	guid := (*windows.GUID)(unsafe.Pointer(&id[0]))
	if luid, err := winipcfg.LUIDFromGUID(guid); err == nil {
		add(luid)
	}
	if inf, err := net.InterfaceByName(AdapterName); err == nil {
		if luid, err := winipcfg.LUIDFromIndex(uint32(inf.Index)); err == nil {
			add(luid)
		}
	}
	return luids
}

// FlushDNS clears the Windows DNS cache so stale answers are not reused.
func FlushDNS() {
	_ = hiddenCmd("ipconfig", "/flushdns").Run()
}

func waitInterface(name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := net.InterfaceByName(name); err == nil {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("адаптер %s не появился", name)
}

func netsh(args ...string) error {
	out, err := hiddenCmd("netsh", args...).CombinedOutput()
	if err != nil {
		msg, _ := charmap.CodePage866.NewDecoder().Bytes(out)
		return fmt.Errorf("netsh %s: %s", strings.Join(args[:4], " "), strings.TrimSpace(string(msg)))
	}
	return nil
}

func hiddenCmd(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	return cmd
}

// IsAdmin reports whether the process runs elevated.
func IsAdmin() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}

// RelaunchElevated starts this executable again with administrator rights.
func RelaunchElevated(args string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	verb, _ := windows.UTF16PtrFromString("runas")
	file, _ := windows.UTF16PtrFromString(exe)
	params, _ := windows.UTF16PtrFromString(args)
	dir, _ := windows.UTF16PtrFromString(filepath.Dir(exe))
	return windows.ShellExecute(0, verb, file, params, dir, windows.SW_SHOWNORMAL)
}

// BindToInterface returns a net.Dialer Control func that pins the socket to
// the given adapter (IP_UNICAST_IF), bypassing the TUN routes.
func BindToInterface(name string) func(network, address string, c syscall.RawConn) error {
	return func(network, address string, c syscall.RawConn) error {
		inf, err := net.InterfaceByName(name)
		if err != nil {
			return err
		}
		var serr error
		err = c.Control(func(fd uintptr) {
			if strings.Contains(address, ".") {
				// IPv4 expects the index in network byte order.
				idx := uint32(inf.Index)
				be := idx>>24 | (idx>>8)&0xff00 | (idx<<8)&0xff0000 | idx<<24
				serr = syscall.SetsockoptInt(syscall.Handle(fd), syscall.IPPROTO_IP, 31, int(be))
			} else {
				serr = syscall.SetsockoptInt(syscall.Handle(fd), syscall.IPPROTO_IPV6, 31, inf.Index)
			}
		})
		if err != nil {
			return err
		}
		return serr
	}
}
