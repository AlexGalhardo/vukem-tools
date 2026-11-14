package engine

import (
	"errors"
	"os/exec"
)

var errNoSandbox = errors.New("there is no sandbox on this platform")

// sandbox applies the platform's process limits. There is one implementation
// per platform (see sandbox_linux.go and sandbox_other.go) plus nopSandbox,
// which is shared because every platform needs the unlimited fallback: on
// Linux it is reached when prlimit is not installed.
type sandbox interface {
	// wrap returns the program and arguments to actually execute, which on
	// Linux means going through prlimit.
	wrap(path string, args []string, limits Limits) (string, []string)
	// isolate sets the process attributes before Start. network keeps the
	// container's network; false puts the program in a namespace of its own
	// where it has none, when the platform can.
	isolate(proc *exec.Cmd, network bool)
	// networkIsolation is nil when isolate can take the network away, and
	// otherwise says why it cannot.
	networkIsolation() error
	// kill ends the program and everything it started.
	kill(proc *exec.Cmd) error
}

// nopSandbox runs the program with a timeout and a best-effort kill, and
// nothing else.
//
// NewRunner accepts it only outside production, and logs that it did. It
// exists so `vukem serve` works on a laptop and so a Linux host without
// util-linux degrades loudly rather than failing to boot in development.
type nopSandbox struct{}

// wrap runs the program directly: there is nothing to wrap it in.
func (nopSandbox) wrap(path string, args []string, _ Limits) (string, []string) {
	return path, args
}

// isolate does nothing, so a child of the program survives a kill. That is
// one more reason this is not a production path.
func (nopSandbox) isolate(*exec.Cmd, bool) {}

// networkIsolation reports that nothing is isolated.
func (nopSandbox) networkIsolation() error { return errNoSandbox }

// kill ends the process itself, not its group.
func (nopSandbox) kill(proc *exec.Cmd) error {
	if proc.Process == nil {
		return nil
	}

	return proc.Process.Kill()
}
