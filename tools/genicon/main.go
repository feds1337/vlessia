// Command genicon writes build/appicon.png, from which `wails build`
// generates the executable icon.
package main

import (
	"image/color"
	"os"

	"github.com/feds1337/vlessia/internal/icon"
)

func main() {
	if err := os.MkdirAll("build", 0o755); err != nil {
		panic(err)
	}
	png := icon.Shield(512, color.RGBA{0x0e, 0x7c, 0x86, 0xff})
	if err := os.WriteFile("build/appicon.png", png, 0o644); err != nil {
		panic(err)
	}
}
