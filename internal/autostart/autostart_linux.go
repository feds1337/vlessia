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

// parseCommand returns the executable from the Exec line.
func parseCommand(desktop string) string {
	for _, line := range strings.Split(desktop, "\n") {
		v, ok := strings.CutPrefix(line, "Exec=")
		if !ok {
			continue
		}
		if strings.HasPrefix(v, `"`) {
			end := strings.Index(v[1:], `" `)
			if end < 0 {
				end = len(v) - 2
			}
			return strings.NewReplacer(`\\`, `\`, `\"`, `"`, "\\`", "`", `\$`, "$").Replace(v[1 : end+1])
		}
		return strings.Fields(v)[0]
	}
	return ""
}
