//go:build darwin || linux

package main

import (
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/feds1337/vlessia/internal/tunhelper"
)

const helperArg = tunhelper.Arg

func runHelper(args []string) int { return tunhelper.Run(args) }

// waitForExit polls until the process is gone.
func waitForExit(pid uint32, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if syscall.Kill(int(pid), 0) != nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "Vlessia:", msg)
	os.Exit(1)
}
