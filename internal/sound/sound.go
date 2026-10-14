// Package sound synthesises and plays the connection sound.
package sound

import (
	"bytes"
	"encoding/binary"
	"math"
)

const sampleRate = 44100

// note is one soft bell-like strike.
type note struct {
	at   float64 // start, seconds
	freq float64 // Hz
	gain float64
}

// Connected renders the connection sound as a 16-bit mono WAV file: two
// quiet, rounded strikes a fifth apart (E5, then B5), like a small
// marimba or a soft glass bell, with a short room around them. It is
// generated rather than shipped as a recording, so it is the same on every
// system.
func Connected() []byte {
	const dur = 1.6
	n := int(dur * sampleRate)
	dry := make([]float64, n)
	for _, nt := range []note{{0, 659.25, 1}, {0.115, 987.77, 0.8}} {
		strike(dry, nt)
	}

	// The room: a few damped echoes (Schroeder comb filters), mixed low.
	wet := make([]float64, n)
	for _, c := range []struct {
		delay    float64
		feedback float64
	}{{0.0297, 0.62}, {0.0371, 0.58}, {0.0411, 0.55}, {0.0437, 0.52}} {
		d := int(c.delay * sampleRate)
		buf := make([]float64, n)
		lp := 0.0
		for i := range buf {
			in := dry[i]
			if i >= d {
				// A low-pass in the loop makes the echoes darker, as in a room.
				lp = 0.7*buf[i-d] + 0.3*lp
				in += c.feedback * lp
			}
			buf[i] = in
			wet[i] += buf[i] / 4
		}
	}

	out := make([]float64, n)
	soft := 0.0
	for i := range out {
		v := 0.86*dry[i] + 0.22*wet[i]
		// Gentle low-pass: nothing sharp at the top.
		soft += 0.55 * (v - soft)
		out[i] = soft
		if t := float64(n-i) / sampleRate; t < 0.25 { // fade the tail out
			out[i] *= t / 0.25
		}
	}

	// Quiet: peak at about -13 dBFS.
	peak := 0.0
	for _, v := range out {
		peak = math.Max(peak, math.Abs(v))
	}
	gain := 0.22 / peak
	return encodeWAV(out, gain)
}

// strike adds one note: a sine with a short FM shimmer at the start (the
// "touch"), a quieter octave that fades sooner, and a long smooth decay.
func strike(dst []float64, nt note) {
	start := int(nt.at * sampleRate)
	for i := start; i < len(dst); i++ {
		t := float64(i-start) / sampleRate
		attack := 1.0
		if t < 0.008 { // a rounded 8 ms attack, no click
			attack = 0.5 - 0.5*math.Cos(math.Pi*t/0.008)
		}
		index := 1.1 * math.Exp(-t/0.06) // brightness of the touch, gone in ~0.1 s
		mod := index * math.Sin(2*math.Pi*nt.freq*2*t)
		body := math.Sin(2*math.Pi*nt.freq*t+mod) * math.Exp(-t/0.42)
		octave := 0.16 * math.Sin(2*math.Pi*nt.freq*2*t) * math.Exp(-t/0.16)
		fifth := 0.04 * math.Sin(2*math.Pi*nt.freq*3*t) * math.Exp(-t/0.07)
		dst[i] += nt.gain * attack * (body + octave + fifth)
	}
}

func encodeWAV(samples []float64, gain float64) []byte {
	var b bytes.Buffer
	w := func(v any) { _ = binary.Write(&b, binary.LittleEndian, v) }
	n := len(samples)
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
	for _, v := range samples {
		w(int16(math.Round(v * gain * 32767)))
	}
	return b.Bytes()
}
