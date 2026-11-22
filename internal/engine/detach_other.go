//go:build !windows

package engine

import "os/exec"

// detach is a no-op outside Windows: Linux isolates through the sandbox, and
// macOS has no console to detach from.
func detach(*exec.Cmd) {}
