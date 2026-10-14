//go:build darwin || linux

package update

import "errors"

// Installed is false: on macOS and Linux updates are installed from the
// release page (the .dmg, .deb or archive), not in place.
func Installed() bool { return false }

// Install is not available outside Windows.
func Install(string, bool) error {
	return errors.New("автоматическая установка доступна только на Windows")
}
