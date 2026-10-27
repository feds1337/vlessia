//go:build !android

package qr

import (
	"errors"

	"github.com/kbinani/screenshot"
)

// FromScreen looks for a QR code on every display.
func FromScreen() (string, error) {
	n := screenshot.NumActiveDisplays()
	if n == 0 {
		return "", errors.New("не удалось сделать снимок экрана")
	}
	var captured bool
	for i := 0; i < n; i++ {
		img, err := screenshot.CaptureDisplay(i)
		if err != nil {
			continue
		}
		captured = true
		if text, err := Decode(img); err == nil {
			return text, nil
		}
	}
	if !captured {
		return "", errors.New("не удалось сделать снимок экрана")
	}
	return "", ErrNotFound
}
