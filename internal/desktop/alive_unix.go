//go:build !windows

package desktop

import (
	"errors"
	"os"
	"syscall"
)

// processAlive reports whether pid is a running process: signal 0 only checks.
// EPERM means it exists and belongs to someone else.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}

	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	err = p.Signal(syscall.Signal(0))

	return err == nil || errors.Is(err, syscall.EPERM)
}
