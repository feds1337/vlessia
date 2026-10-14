//go:build linux

// Package sysproxy toggles the desktop's system proxy. Supported: GNOME and
// derivatives (gsettings) and KDE Plasma (kioslaverc). Other desktops have
// no common proxy setting; the user is asked to configure it manually.
package sysproxy

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
)

// State is a snapshot of the proxy settings.
type State struct {
	Desktop string            `json:"desktop"` // "gnome" | "kde"
	Values  map[string]string `json:"values"`
}

var ErrUnsupported = errors.New("в этом окружении рабочего стола системный прокси не настраивается автоматически: " +
	"укажите HTTP-прокси 127.0.0.1:10809 в настройках сети вручную или используйте режим «Весь трафик»")

func desktop() string {
	d := strings.ToLower(os.Getenv("XDG_CURRENT_DESKTOP") + ":" + os.Getenv("DESKTOP_SESSION"))
	switch {
	case strings.Contains(d, "kde"), strings.Contains(d, "plasma"):
		if kdeTool() != "" {
			return "kde"
		}
	}
	if _, err := exec.LookPath("gsettings"); err == nil {
		if out, err := exec.Command("gsettings", "get", "org.gnome.system.proxy", "mode").Output(); err == nil && len(out) > 0 {
			return "gnome"
		}
	}
	return ""
}

// ---------- GNOME ----------

var gnomeKeys = [][2]string{
	{"org.gnome.system.proxy", "mode"},
	{"org.gnome.system.proxy", "ignore-hosts"},
	{"org.gnome.system.proxy.http", "host"},
	{"org.gnome.system.proxy.http", "port"},
	{"org.gnome.system.proxy.https", "host"},
	{"org.gnome.system.proxy.https", "port"},
}

func gsettings(args ...string) (string, error) {
	out, err := exec.Command("gsettings", args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("gsettings %s: %v %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// ---------- KDE ----------

func kdeTool() string {
	for _, t := range []string{"kwriteconfig6", "kwriteconfig5"} {
		if p, err := exec.LookPath(t); err == nil {
			return p
		}
	}
	return ""
}

var kdeKeys = []string{"ProxyType", "httpProxy", "httpsProxy", "NoProxyFor"}

func kreadconfig(key string) string {
	tool := strings.Replace(kdeTool(), "kwrite", "kread", 1)
	out, _ := exec.Command(tool, "--file", "kioslaverc", "--group", "Proxy Settings", "--key", key).Output()
	return strings.TrimSpace(string(out))
}

func kwriteconfig(key, value string) error {
	out, err := exec.Command(kdeTool(), "--file", "kioslaverc", "--group", "Proxy Settings", "--key", key, value).CombinedOutput()
	if err != nil {
		return fmt.Errorf("kwriteconfig %s: %v %s", key, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// kdeNotify tells running KDE apps to reread the proxy settings.
func kdeNotify() {
	_ = exec.Command("dbus-send", "--type=signal", "/KIO/Scheduler", "org.kde.KIO.Scheduler.reparseSlaveConfiguration", "string:").Run()
}

// ---------- API ----------

// Get reads the current proxy settings.
func Get() (State, error) {
	switch desktop() {
	case "gnome":
		st := State{Desktop: "gnome", Values: map[string]string{}}
		for _, k := range gnomeKeys {
			v, err := gsettings("get", k[0], k[1])
			if err != nil {
				return State{}, err
			}
			st.Values[k[0]+" "+k[1]] = v
		}
		return st, nil
	case "kde":
		st := State{Desktop: "kde", Values: map[string]string{}}
		for _, k := range kdeKeys {
			st.Values[k] = kreadconfig(k)
		}
		return st, nil
	}
	return State{}, ErrUnsupported
}

// Set restores proxy settings.
func Set(st State) error {
	switch st.Desktop {
	case "gnome":
		for _, k := range gnomeKeys {
			if v, ok := st.Values[k[0]+" "+k[1]]; ok {
				if _, err := gsettings("set", k[0], k[1], v); err != nil {
					return err
				}
			}
		}
		return nil
	case "kde":
		for _, k := range kdeKeys {
			if err := kwriteconfig(k, st.Values[k]); err != nil {
				return err
			}
		}
		kdeNotify()
		return nil
	}
	return nil
}

// Enable points the HTTP and HTTPS proxies at hostPort.
func Enable(hostPort string) error {
	host, port, err := net.SplitHostPort(hostPort)
	if err != nil {
		return err
	}
	switch desktop() {
	case "gnome":
		for _, kv := range [][3]string{
			{"org.gnome.system.proxy.http", "host", host},
			{"org.gnome.system.proxy.http", "port", port},
			{"org.gnome.system.proxy.https", "host", host},
			{"org.gnome.system.proxy.https", "port", port},
			{"org.gnome.system.proxy", "ignore-hosts", "['localhost', '127.0.0.0/8', '::1', '10.0.0.0/8', '172.16.0.0/12', '192.168.0.0/16', '*.local']"},
			{"org.gnome.system.proxy", "mode", "manual"},
		} {
			if _, err := gsettings("set", kv[0], kv[1], kv[2]); err != nil {
				return err
			}
		}
		return nil
	case "kde":
		url := "http://" + host + " " + port
		for _, kv := range [][2]string{
			{"httpProxy", url}, {"httpsProxy", url},
			{"NoProxyFor", "localhost,127.0.0.0/8,::1,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,.local"},
			{"ProxyType", "1"},
		} {
			if err := kwriteconfig(kv[0], kv[1]); err != nil {
				return err
			}
		}
		kdeNotify()
		return nil
	}
	return ErrUnsupported
}
