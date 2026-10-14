//go:build linux

package autostart

import (
	"os"
	"path/filepath"
	"strings"
)

func entryPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		dir = filepath.Join(home(), ".config")
	}
	return filepath.Join(dir, "autostart", "vlessia.desktop")
}

// desktopQuote quotes an argument for the Exec key of a .desktop file.
func desktopQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "`", "\\`", "$", `\$`).Replace(s) + `"`
}

func entry(exe string) string {
	return "[Desktop Entry]\nType=Application\nName=Vlessia\nComment=VPN-клиент\n" +
		"Exec=" + desktopQuote(exe) + " " + Arg + "\nIcon=vlessia\nTerminal=false\nX-GNOME-Autostart-enabled=true\n"
}

// parseCommand returns the executable from the Exec line, undoing the
// quoting of desktopQuote.
func parseCommand(desktop string) string {
	for _, line := range strings.Split(desktop, "\n") {
		v, ok := strings.CutPrefix(line, "Exec=")
		if !ok {
			continue
		}
		if !strings.HasPrefix(v, `"`) {
			return strings.Fields(v)[0]
		}
		var b strings.Builder
		for i := 1; i < len(v); i++ {
			switch c := v[i]; {
			case c == '\\' && i+1 < len(v):
				i++
				b.WriteByte(v[i])
			case c == '"':
				return b.String()
			default:
				b.WriteByte(c)
			}
		}
		return b.String()
	}
	return ""
}
