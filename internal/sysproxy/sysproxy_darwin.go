//go:build darwin

// Package sysproxy toggles the system proxy. On macOS it is set per network
// service (Wi-Fi, Ethernet, ...) with networksetup.
package sysproxy

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
)

const networksetup = "/usr/sbin/networksetup"

// bypassDomains lists addresses that must never go through the proxy.
var bypassDomains = []string{"localhost", "127.0.0.1", "*.local", "169.254/16", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"}

type proxyEntry struct {
	Enabled bool   `json:"enabled"`
	Server  string `json:"server"`
	Port    int    `json:"port"`
}

type serviceState struct {
	Name   string     `json:"name"`
	Web    proxyEntry `json:"web"`
	Secure proxyEntry `json:"secure"`
	Socks  proxyEntry `json:"socks"`
	Bypass []string   `json:"bypass"`
}

// State is a snapshot of the proxy settings of all network services.
type State struct {
	Services []serviceState `json:"services"`
}

func run(args ...string) (string, error) {
	out, err := exec.Command(networksetup, args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("networksetup %s: %v %s", args[0], err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// services lists enabled network services.
func services() ([]string, error) {
	out, err := run("-listallnetworkservices")
	if err != nil {
		return nil, err
	}
	var names []string
	sc := bufio.NewScanner(strings.NewReader(out))
	for first := true; sc.Scan(); first = false {
		line := strings.TrimSpace(sc.Text())
		// The first line is an explanation; disabled services start with '*'.
		if first || line == "" || strings.HasPrefix(line, "*") {
			continue
		}
		names = append(names, line)
	}
	return names, nil
}

func getEntry(kind, service string) proxyEntry {
	out, err := run("-get"+kind, service)
	if err != nil {
		return proxyEntry{}
	}
	var e proxyEntry
	sc := bufio.NewScanner(bytes.NewReader([]byte(out)))
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch strings.TrimSpace(k) {
		case "Enabled":
			e.Enabled = v == "Yes"
		case "Server":
			e.Server = v
		case "Port":
			e.Port, _ = strconv.Atoi(v)
		}
	}
	return e
}

func setEntry(kind, service string, e proxyEntry) error {
	if e.Server != "" {
		if _, err := run("-set"+kind, service, e.Server, strconv.Itoa(e.Port)); err != nil {
			return err
		}
	}
	state := "off"
	if e.Enabled {
		state = "on"
	}
	_, err := run("-set"+kind+"state", service, state)
	return err
}

// Get reads the current proxy settings.
func Get() (State, error) {
	names, err := services()
	if err != nil {
		return State{}, err
	}
	var st State
	for _, n := range names {
		s := serviceState{Name: n, Web: getEntry("webproxy", n), Secure: getEntry("securewebproxy", n), Socks: getEntry("socksfirewallproxy", n)}
		if out, err := run("-getproxybypassdomains", n); err == nil && !strings.Contains(out, "There aren't any") {
			s.Bypass = strings.Fields(out)
		}
		st.Services = append(st.Services, s)
	}
	return st, nil
}

// Set restores proxy settings.
func Set(st State) error {
	var firstErr error
	for _, s := range st.Services {
		for kind, e := range map[string]proxyEntry{"webproxy": s.Web, "securewebproxy": s.Secure, "socksfirewallproxy": s.Socks} {
			if err := setEntry(kind, s.Name, e); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		bypass := s.Bypass
		if len(bypass) == 0 {
			bypass = []string{"Empty"}
		}
		if _, err := run(append([]string{"-setproxybypassdomains", s.Name}, bypass...)...); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Enable points the HTTP and HTTPS proxies of every service at hostPort.
func Enable(hostPort string) error {
	host, portStr, err := net.SplitHostPort(hostPort)
	if err != nil {
		return err
	}
	port, _ := strconv.Atoi(portStr)
	names, err := services()
	if err != nil {
		return err
	}
	for _, n := range names {
		e := proxyEntry{Enabled: true, Server: host, Port: port}
		if err := setEntry("webproxy", n, e); err != nil {
			return err
		}
		if err := setEntry("securewebproxy", n, e); err != nil {
			return err
		}
		if _, err := run(append([]string{"-setproxybypassdomains", n}, bypassDomains...)...); err != nil {
			return err
		}
	}
	return nil
}
