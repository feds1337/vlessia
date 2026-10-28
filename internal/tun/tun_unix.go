//go:build darwin || linux

package tun

import (
	"errors"
	"os"
)

// IsAdmin reports whether the process runs as root. On macOS and Linux the
// client itself never needs it: TUN mode is handled by a privileged helper
// (internal/tunhelper) started on demand.
func IsAdmin() bool { return os.Geteuid() == 0 }

// RelaunchElevated is Windows-only; other systems use the TUN helper.
func RelaunchElevated(string) error {
	return errors.New("перезапуск с правами администратора не нужен на этой системе")
}

// FlushDNS is a no-op: the helper's routes and DNS changes disappear with
// the TUN device.
func FlushDNS() {}

// Cleanup is a no-op on Unix: the privileged helper cleans up routes and DNS.
func Cleanup() {}

// DefaultInterface returns the name of the adapter used to reach the
// internet.
func DefaultInterface() (string, error) {
	iface, _, err := DefaultRoute()
	return iface, err
}
