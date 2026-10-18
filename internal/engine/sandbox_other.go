//go:build !linux

package engine

import (
	"errors"
	"os"
	"runtime"
)

// newSandbox reports that this platform has no equivalent of prlimit.
//
// There is no attempt to approximate it. A Job Object on Windows would cap
// memory but not CPU time or file size, and macOS has no prlimit at all; a
// partial sandbox that reads as a sandbox is worse than an honest absence,
// because it is the thing a deploy would trust. Production runs on Linux in
// the container the Dockerfile builds, and NewRunner refuses to start without
// limits there — this path exists so `vukem serve` works on a laptop.
func newSandbox() (sandbox, error) {
	return nil, errors.New("subprocess resource limits need prlimit, which exists only on Linux (this is " +
		runtime.GOOS + ")")
}

// maxRSS is unavailable here: os.ProcessState.SysUsage returns a
// platform-specific type that carries no peak memory outside Unix.
func maxRSS(*os.ProcessState) int64 { return 0 }
