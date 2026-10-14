package qr

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"testing"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
)

const link = "https://sub.example.com/api/sub/AbCdEf0123456789?name=%D0%A2%D0%B5%D1%81%D1%82"

func encode(t *testing.T, text string, size int) image.Image {
	t.Helper()
	m, err := qrcode.NewQRCodeWriter().Encode(text, gozxing.BarcodeFormat_QR_CODE, size, size, nil)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestDecodePNG(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, encode(t, link, 300)); err != nil {
		t.Fatal(err)
	}
	if !IsImage(buf.Bytes()) || IsImage([]byte("vless://x@1.2.3.4:443")) {
		t.Fatal("IsImage")
	}
	if got, err := DecodeBytes(buf.Bytes()); err != nil || got != link {
		t.Fatalf("DecodeBytes = %q, %v", got, err)
	}
}

// A small code somewhere on a large, busy "screenshot", and a light-on-dark
// one as in dark themes.
func TestDecodeScreen(t *testing.T) {
	screen := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	draw.Draw(screen, screen.Bounds(), &image.Uniform{color.RGBA{0x24, 0x2f, 0x3d, 0xff}}, image.Point{}, draw.Src)
	for x := 0; x < 1920; x += 7 {
		for y := 0; y < 1080; y += 13 {
			screen.Set(x, y, color.White)
		}
	}
	code := encode(t, link, 220)
	draw.Draw(screen, image.Rect(1500, 700, 1720, 920), code, image.Point{}, draw.Src)
	if got, err := Decode(screen); err != nil || got != link {
		t.Fatalf("on screen: %q, %v", got, err)
	}

	if got, err := Decode(inverted{encode(t, link, 300)}); err != nil || got != link {
		t.Fatalf("inverted: %q, %v", got, err)
	}
	if _, err := Decode(image.NewGray(image.Rect(0, 0, 200, 200))); err != ErrNotFound {
		t.Fatalf("blank image: %v", err)
	}
}
