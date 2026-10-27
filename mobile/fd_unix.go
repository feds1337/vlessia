//go:build unix

package mobile

import "syscall"

func closeFD(fd int) { _ = syscall.Close(fd) }
