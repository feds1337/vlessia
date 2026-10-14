// Package icon renders the application icon in code.
package icon

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
)

// Shield draws a shield with a keyhole in the given colour, as PNG.
func Shield(size int, c color.RGBA) []byte {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	s := float64(size)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			// Supersample 4x4 for smooth edges.
			hits := 0
			for sy := 0; sy < 4; sy++ {
				for sx := 0; sx < 4; sx++ {
					px := (float64(x) + (float64(sx)+0.5)/4) / s
					py := (float64(y) + (float64(sy)+0.5)/4) / s
					if inShield(px, py) {
						hits++
					}
				}
			}
			if hits == 0 {
				continue
			}
			col := c
			px, py := (float64(x)+0.5)/s, (float64(y)+0.5)/s
			if inKeyhole(px, py) {
				col = color.RGBA{0xff, 0xff, 0xff, 0xff}
			}
			col.A = uint8(int(col.A) * hits / 16)
			col.R = uint8(int(col.R) * hits / 16)
			col.G = uint8(int(col.G) * hits / 16)
			col.B = uint8(int(col.B) * hits / 16)
			img.SetRGBA(x, y, col)
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

// inShield tests a point in unit coordinates against a shield outline:
// straight upper half, lower half curving down to a point.
func inShield(x, y float64) bool {
	const top, bottom = 0.06, 0.96
	if y < top || y > bottom {
		return false
	}
	t := (y - top) / (bottom - top)
	var half float64
	if t < 0.5 {
		half = 0.42
	} else {
		u := (t - 0.5) / 0.5
		half = 0.42 * (1 - u*u)
	}
	return math.Abs(x-0.5) <= half
}

func inKeyhole(x, y float64) bool {
	dx, dy := x-0.5, y-0.40
	if dx*dx+dy*dy < 0.11*0.11 {
		return true
	}
	return math.Abs(dx) < 0.05 && y > 0.40 && y < 0.68
}

// ICO wraps PNG data into an ICO container (Vista+ accepts PNG entries).
func ICO(p []byte, size int) []byte {
	var b bytes.Buffer
	w := func(v any) { _ = binary.Write(&b, binary.LittleEndian, v) }
	w(uint16(0))      // reserved
	w(uint16(1))      // type: icon
	w(uint16(1))      // count
	dim := byte(size) // 0 means 256
	b.WriteByte(dim)
	b.WriteByte(dim)
	b.WriteByte(0) // palette
	b.WriteByte(0) // reserved
	w(uint16(1))   // planes
	w(uint16(32))  // bpp
	w(uint32(len(p)))
	w(uint32(6 + 16))
	b.Write(p)
	return b.Bytes()
}
