//go:build darwin || linux

// Package autostart starts the client when the user logs in: a LaunchAgent
// on macOS, an XDG autostart entry on Linux. TUN mode needs no special
// rights at login there: the helper asks for the password when connecting.
package autostart

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Arg marks a launch made by autostart.
const Arg = "--autostart"

const (
	ModeOff  = ""
	ModeTask = "task" // Windows only
	ModeRun  = "run"
)

type Status struct {
	Mode    string `json:"mode"`
	Command string `json:"command"`
}

// Get reports the current registration.
func Get() Status {
	b, err := os.ReadFile(entryPath())
	if err != nil {
		return Status{}
	}
	return Status{Mode: ModeRun, Command: parseCommand(string(b))}
}

// Enable registers exe to start at login.
func Enable(exe string) error {
	path := entryPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(entry(exe)), 0o644)
}

// Disable removes the registration.
func Disable() {
	_ = os.Remove(entryPath())
}

func home() string {
	h, _ := os.UserHomeDir()
	return h
}

var plistProgram = regexp.MustCompile(`<string>([^<]*)</string>`)

func xmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

func xmlUnescape(s string) string {
	return strings.NewReplacer("&lt;", "<", "&gt;", ">", "&amp;", "&").Replace(s)
}
