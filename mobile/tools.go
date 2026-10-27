//go:build tools

// Pins golang.org/x/mobile, which gomobile bind needs in go.mod at the
// same version as the gomobile tool (see android/build-core.sh).
package mobile

import _ "golang.org/x/mobile/bind"
