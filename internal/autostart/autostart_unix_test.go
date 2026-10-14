//go:build darwin || linux

package autostart

import "testing"

// TestRoundTrip writes the entry into a temporary home directory.
func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir+"/config")
	exe := `/Applications/My "Apps" & Co/Vlessia $x.app/Contents/MacOS/Vlessia`
	if err := Enable(exe); err != nil {
		t.Fatal(err)
	}
	if st := Get(); st.Mode != ModeRun || st.Command != exe {
		t.Fatalf("got %+v", st)
	}
	Disable()
	if st := Get(); st.Mode != ModeOff {
		t.Fatalf("not removed: %+v", st)
	}
}
