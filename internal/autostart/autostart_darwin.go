//go:build darwin

package autostart

import (
	"path/filepath"
	"strings"
)

const label = "com.feds1337.vlessia"

func entryPath() string {
	return filepath.Join(home(), "Library", "LaunchAgents", label+".plist")
}

func entry(exe string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>` + label + `</string>
  <key>ProgramArguments</key>
  <array>
    <string>` + xmlEscape(exe) + `</string>
    <string>` + Arg + `</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>ProcessType</key><string>Interactive</string>
</dict>
</plist>
`
}

// parseCommand returns the executable: the first string in the
// ProgramArguments array (the label comes before it).
func parseCommand(plist string) string {
	_, args, ok := strings.Cut(plist, "<key>ProgramArguments</key>")
	if !ok {
		return ""
	}
	m := plistProgram.FindStringSubmatch(args)
	if m == nil {
		return ""
	}
	return xmlUnescape(m[1])
}
