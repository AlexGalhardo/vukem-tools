package desktop

import "os"

// processAlive reports whether pid is a running process. On Windows
// FindProcess opens the process and fails when there is none.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}

	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	_ = p.Release()

	return true
}
