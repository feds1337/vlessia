//go:build windows

package autostart

import (
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// TestTaskRoundTrip registers a real scheduler task (or Run entry when not
// elevated) for a dummy path, reads it back and removes it. It changes
// system state, so it only runs with AUTOSTART_TEST=1.
func TestTaskRoundTrip(t *testing.T) {
	if os.Getenv("AUTOSTART_TEST") == "" {
		t.Skip("set AUTOSTART_TEST=1")
	}
	if Get().Mode != ModeOff {
		t.Skip("autostart is already configured on this machine; not touching it")
	}
	exe := `C:\Program Files\Vlessia Test & Co\Vlessia.exe`
	if err := Enable(exe); err != nil {
		t.Fatal(err)
	}
	defer func() {
		Disable()
		if st := Get(); st.Mode != ModeOff {
			t.Errorf("not removed: %+v", st)
		}
	}()

	st := Get()
	want := ModeRun
	if windows.GetCurrentProcessToken().IsElevated() {
		want = ModeTask
	}
	if st.Mode != want || st.Command != exe {
		t.Fatalf("got %+v, want mode %s command %q", st, want, exe)
	}
	if st.Mode == ModeTask {
		out, err := schtasks("/query", "/tn", name, "/xml")
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range []string{
			"<DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>",
			"<ExecutionTimeLimit>PT0S</ExecutionTimeLimit>",
			"<RunLevel>HighestAvailable</RunLevel>",
			"<Arguments>" + Arg + "</Arguments>",
		} {
			if !strings.Contains(out, s) {
				t.Errorf("task is missing %s", s)
			}
		}
	}
	t.Logf("registered as %s: %s", st.Mode, st.Command)
}
