// Command genicon writes build/appicon.png, from which `wails build`
// generates the executable icon, and the Android launcher icons for
// Android 7 (newer versions use the adaptive vector icon).
package main

import (
	"fmt"
	"image/color"
	"os"
	"path/filepath"

	"github.com/feds1337/vlessia/internal/icon"
)

var teal = color.RGBA{0x0e, 0x7c, 0x86, 0xff}

func main() {
	write("build/appicon.png", icon.Shield(512, teal))
	for density, size := range map[string]int{"mdpi": 48, "hdpi": 72, "xhdpi": 96, "xxhdpi": 144, "xxxhdpi": 192} {
		write(filepath.Join("android/app/src/main/res", fmt.Sprintf("mipmap-%s", density), "ic_launcher.png"), icon.Shield(size, teal))
	}
}

func write(path string, data []byte) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		panic(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		panic(err)
	}
}
