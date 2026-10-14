//go:build windows

// Package sysproxy toggles the Windows (WinINet) system proxy for the
// current user.
package sysproxy

import (
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const keyPath = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

// Bypass lists addresses that must never go through the proxy.
const Bypass = "localhost;127.*;10.*;172.16.*;172.17.*;172.18.*;172.19.*;172.20.*;172.21.*;172.22.*;" +
	"172.23.*;172.24.*;172.25.*;172.26.*;172.27.*;172.28.*;172.29.*;172.30.*;172.31.*;192.168.*;<local>"

// State is a snapshot of the proxy settings, used to restore them later.
type State struct {
	Enable   uint32 `json:"enable"`
	Server   string `json:"server"`
	Override string `json:"override"`
}

// Get reads the current proxy settings.
func Get() (State, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, keyPath, registry.QUERY_VALUE)
	if err != nil {
		return State{}, err
	}
	defer k.Close()
	var s State
	if v, _, err := k.GetIntegerValue("ProxyEnable"); err == nil {
		s.Enable = uint32(v)
	}
	s.Server, _, _ = k.GetStringValue("ProxyServer")
	s.Override, _, _ = k.GetStringValue("ProxyOverride")
	return s, nil
}

// Set writes proxy settings and notifies running applications.
func Set(s State) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, keyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if err := k.SetDWordValue("ProxyEnable", s.Enable); err != nil {
		return err
	}
	if err := k.SetStringValue("ProxyServer", s.Server); err != nil {
		return err
	}
	if err := k.SetStringValue("ProxyOverride", s.Override); err != nil {
		return err
	}
	notify()
	return nil
}

// Enable points the system proxy at a local HTTP proxy.
func Enable(hostPort string) error {
	return Set(State{Enable: 1, Server: hostPort, Override: Bypass})
}

var (
	wininet           = windows.NewLazySystemDLL("wininet.dll")
	internetSetOption = wininet.NewProc("InternetSetOptionW")
)

const (
	optionSettingsChanged = 39
	optionRefresh         = 37
)

func notify() {
	_, _, _ = internetSetOption.Call(0, optionSettingsChanged, 0, 0)
	_, _, _ = internetSetOption.Call(0, optionRefresh, 0, 0)
}
