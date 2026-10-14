//go:build darwin || linux

package sound

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
)

var (
	once sync.Once
	path string // the WAV file, written once per run
)

// PlayConnected plays the connection sound without blocking, using the
// system player (afplay on macOS; PulseAudio/PipeWire or ALSA on Linux).
func PlayConnected() {
	once.Do(func() {
		p := filepath.Join(os.TempDir(), "vlessia-connected.wav")
		if os.WriteFile(p, Connected(), 0o644) == nil {
			path = p
		}
	})
	if path == "" {
		return
	}
	players := [][]string{{"paplay"}, {"pw-play"}, {"aplay", "-q"}}
	if runtime.GOOS == "darwin" {
		players = [][]string{{"/usr/bin/afplay"}}
	}
	for _, p := range players {
		if bin, err := exec.LookPath(p[0]); err == nil {
			cmd := exec.Command(bin, append(p[1:], path)...)
			if cmd.Start() == nil {
				go func() { _ = cmd.Wait() }()
			}
			return
		}
	}
}
