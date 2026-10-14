// Package sound synthesises and plays the connection sound.
package sound

import (
	"bytes"
	"encoding/binary"
	"math"
	"math/rand"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

const sampleRate = 44100

// Fart renders the connection sound as a 16-bit mono WAV file. It is
// generated rather than shipped as a recording, and is deterministic (fixed
// seed), so it sounds the same every time.
func Fart() []byte {
	rng := rand.New(rand.NewSource(42))
	const dur = 1.15
	n := int(dur * sampleRate)
	out := make([]float64, n)

	var (
		phase    float64
		wobble   float64 // slow random walk of the pitch
		lp1, lp2 float64 // two one-pole low-pass stages ("muffled")
		noiseLP  float64
		flutter  = 1.0
		gate     = 1.0
		jitter   = 1.0
		dcIn     float64 // DC blocker state
		dcOut    float64
	)
	for i := 0; i < n; i++ {
		t := float64(i) / sampleRate
		u := t / dur

		// Pitch: quick rise, then a long sag towards the end.
		base := 95 + 25*math.Sin(math.Min(u/0.15, 1)*math.Pi/2) - 70*math.Pow(u, 1.6)
		wobble += rng.NormFloat64() * 0.0025
		wobble *= 0.9995
		f := math.Max(base*(1+wobble)*jitter, 38)

		phase += f / sampleRate
		if phase >= 1 {
			phase -= 1
			// Cycle-to-cycle irregularity of the vibrating "valve".
			flutter = 0.65 + 0.35*rng.Float64()
			jitter = 1 + rng.NormFloat64()*0.07 // rough, not a clean tone
			if u > 0.62 {
				// Sputtering tail: some cycles drop out.
				if rng.Float64() < (u-0.62)*1.8 {
					gate = 0.05
				} else {
					gate = 1
				}
			}
		}

		// Asymmetric pulse: fast opening, slower closing, then shut.
		var pulse float64
		const open = 0.42
		if phase < open {
			x := phase / open
			pulse = math.Sin(math.Pi*math.Pow(x, 0.7)) * math.Exp(-1.5*x)
		}
		pulse = pulse*2 - 0.35 // remove most of the DC

		// Air noise.
		noiseLP += (rng.Float64()*2 - 1 - noiseLP) * 0.08

		x := pulse*flutter*gate + noiseLP*0.35

		// Muffle: two cascaded low-pass filters (~700 Hz).
		const a = 0.095
		lp1 += a * (x - lp1)
		lp2 += a * (lp1 - lp2)

		// DC blocker (~25 Hz high-pass): the pulses are one-sided, and an
		// offset would click at the start and end.
		dcOut = lp2 - dcIn + 0.9965*dcOut
		dcIn = lp2

		// Envelope: soft attack, slight swell, fade out.
		env := math.Min(t/0.02, 1) * (1 + 0.25*math.Sin(u*math.Pi)) * math.Pow(1-u, 0.6)
		out[i] = dcOut * env
	}

	// Normalise to -2 dBFS.
	peak := 0.0
	for _, v := range out {
		peak = math.Max(peak, math.Abs(v))
	}
	gain := 0.8 / peak

	var b bytes.Buffer
	w := func(v any) { _ = binary.Write(&b, binary.LittleEndian, v) }
	b.WriteString("RIFF")
	w(uint32(36 + n*2))
	b.WriteString("WAVEfmt ")
	w(uint32(16))
	w(uint16(1)) // PCM
	w(uint16(1)) // mono
	w(uint32(sampleRate))
	w(uint32(sampleRate * 2))
	w(uint16(2))
	w(uint16(16))
	b.WriteString("data")
	w(uint32(n * 2))
	for _, v := range out {
		w(int16(math.Round(v * gain * 32767)))
	}
	return b.Bytes()
}

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
	once.Do(func() { wav = Fart() })
	_, _, _ = playSound.Call(uintptr(unsafe.Pointer(&wav[0])), 0, sndMemory|sndAsync|sndNoDefault)
}
