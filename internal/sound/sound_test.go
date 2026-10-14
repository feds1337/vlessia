package sound

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"testing"
)

func TestConnected(t *testing.T) {
	b := Connected()
	if !bytes.HasPrefix(b, []byte("RIFF")) || string(b[8:16]) != "WAVEfmt " || string(b[36:40]) != "data" {
		t.Fatal("not a PCM WAV file")
	}
	s := make([]int16, (len(b)-44)/2)
	if err := binary.Read(bytes.NewReader(b[44:]), binary.LittleEndian, s); err != nil {
		t.Fatal(err)
	}
	peak, jump := 0.0, 0.0
	for i, v := range s {
		peak = math.Max(peak, math.Abs(float64(v)))
		if i > 0 {
			jump = math.Max(jump, math.Abs(float64(v)-float64(s[i-1])))
		}
	}
	peak /= 32767
	if peak < 0.2 || peak > 0.23 {
		t.Errorf("peak %.3f, want a quiet sound around 0.22", peak)
	}
	// No clicks: smooth start and end, no sample-to-sample steps.
	if math.Abs(float64(s[0])) > 30 || math.Abs(float64(s[len(s)-1])) > 30 {
		t.Errorf("starts at %d, ends at %d", s[0], s[len(s)-1])
	}
	if jump/32767 > 0.08 {
		t.Errorf("largest step between samples %.3f", jump/32767)
	}
	if out := os.Getenv("SOUND_OUT"); out != "" {
		_ = os.WriteFile(out, b, 0o644)
	}
}
