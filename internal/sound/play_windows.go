//go:build windows

package sound

import (
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	winmm     = windows.NewLazySystemDLL("winmm.dll")
	playSound = winmm.NewProc("PlaySoundW")

	once sync.Once
	wav  []byte // must stay alive while PlaySound plays it asynchronously
)

const (
	sndAsync     = 0x0001
	sndNoDefault = 0x0002
	sndMemory    = 0x0004
)

// PlayConnected plays the connection sound without blocking.
func PlayConnected() {
	once.Do(func() { wav = Connected() })
	_, _, _ = playSound.Call(uintptr(unsafe.Pointer(&wav[0])), 0, sndMemory|sndAsync|sndNoDefault)
}
