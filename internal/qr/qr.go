// Package qr reads QR codes — share links and subscription URLs — from
// images and, on desktop systems, from the screen.
package qr

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"strings"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
)

// ErrNotFound means the image has no readable QR code.
var ErrNotFound = errors.New("QR-код не найден")

// IsImage reports whether data looks like a picture rather than text.
func IsImage(data []byte) bool {
	return strings.HasPrefix(http.DetectContentType(data), "image/")
}

// DecodeBytes reads a QR code from a PNG, JPEG or GIF file.
func DecodeBytes(data []byte) (string, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", errors.New("не удалось открыть картинку: поддерживаются PNG, JPEG и GIF")
	}
	return Decode(img)
}

// Decode reads a QR code from img. Light-on-dark codes (dark themes) are
// read too.
func Decode(img image.Image) (string, error) {
	if text, err := decode(img); err == nil {
		return text, nil
	}
	if text, err := decode(inverted{img}); err == nil {
		return text, nil
	}
	return "", ErrNotFound
}

func decode(img image.Image) (string, error) {
	bmp, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		return "", err
	}
	res, err := qrcode.NewQRCodeReader().Decode(bmp, map[gozxing.DecodeHintType]interface{}{
		gozxing.DecodeHintType_TRY_HARDER: true,
	})
	if err != nil {
		return "", err
	}
	text := strings.TrimSpace(res.GetText())
	if text == "" {
		return "", ErrNotFound
	}
	return text, nil
}

// inverted swaps light and dark.
type inverted struct{ image.Image }

func (i inverted) At(x, y int) color.Color {
	r, g, b, a := i.Image.At(x, y).RGBA()
	return color.RGBA64{uint16(0xffff - r), uint16(0xffff - g), uint16(0xffff - b), uint16(a)}
}
