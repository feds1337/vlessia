//go:build windows

package update

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// Installed reports whether the running executable was installed by the
// setup program (portable copies have no uninstaller next to them).
func Installed() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(filepath.Dir(exe), "uninstall.exe"))
	return err == nil
}

// Install starts the downloaded installer silently over the current
// installation; it closes the running client and starts the new version
// (with --connect when reconnect is set). ShellExecute is used because the
// installer requires elevation, which CreateProcess cannot request.
func Install(setup string, reconnect bool) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	args := []string{"/S", "--relaunch"}
	if reconnect {
		args = append(args, "--connect")
	}
	// NSIS requires /D to be last and unquoted, even with spaces.
	args = append(args, "/D="+filepath.Dir(exe))
	verb, _ := windows.UTF16PtrFromString("open")
	file, _ := windows.UTF16PtrFromString(setup)
	params, _ := windows.UTF16PtrFromString(strings.Join(args, " "))
	dir, _ := windows.UTF16PtrFromString(filepath.Dir(setup))
	return windows.ShellExecute(0, verb, file, params, dir, windows.SW_SHOWNORMAL)
}
