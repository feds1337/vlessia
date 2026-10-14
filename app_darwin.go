package main

import (
	"context"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// hideOnClose minimises to the Dock instead of hiding: there is no tray on
// macOS here, and a hidden Wails window cannot be reopened from the Dock.
func hideOnClose(ctx context.Context) { runtime.WindowMinimise(ctx) }
