package main

import (
	"context"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// hideOnClose: closing the window keeps the client running in the tray.
func hideOnClose(ctx context.Context) { runtime.WindowHide(ctx) }
