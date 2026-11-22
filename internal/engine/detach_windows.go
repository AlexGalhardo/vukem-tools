//go:build windows

package engine

import (
	"os/exec"
	"syscall"
)

const (
	createNoWindow        = 0x08000000
	createNewProcessGroup = 0x00000200
)

// detach runs an engine without a console of its own and outside the app's
// process group.
//
// The desktop app is a GUI process with no console. A console program started
// from it, or the launcher a package manager puts in front of it (a scoop
// shim), is given a fresh console, and a Ctrl+C or close event aimed at that
// console ends it with 0xc000013a (STATUS_CONTROL_C_EXIT), which boot reported
// as "could not start". CREATE_NO_WINDOW gives it a hidden console and
// CREATE_NEW_PROCESS_GROUP makes it ignore Ctrl+C.
func detach(proc *exec.Cmd) {
	proc.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow | createNewProcessGroup,
	}
}
