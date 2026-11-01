package xray

import (
	"errors"
	"fmt"
	"os"
	"testing"
)

func TestAlreadyClosed(t *testing.T) {
	// The text Xray produced on Android when disconnecting.
	android := errors.New("core: failed to close all features > core: app/proxyman/inbound: failed to close all handlers > " +
		"app/proxyman/inbound: app/proxyman/inbound: failed to close all resources > multierr: multierr: close /dev/net/tun: file already closed | |")
	for _, tc := range []struct {
		err  error
		want bool
	}{
		{nil, false},
		{errors.New("boom"), false},
		{os.ErrClosed, true},
		{fmt.Errorf("close: %w", os.ErrClosed), true},
		{android, true},
	} {
		if got := alreadyClosed(tc.err); got != tc.want {
			t.Errorf("alreadyClosed(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}
