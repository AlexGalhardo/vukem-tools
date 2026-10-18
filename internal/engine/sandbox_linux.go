//go:build linux

package engine

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

// linuxSandbox limits a child through prlimit(1) from util-linux.
//
// Go cannot do this itself, and the reason is worth writing down: syscall.
// Setrlimit applies to the *calling* process, and Go exposes no hook that
// runs in the child between fork and exec — SysProcAttr has no such field,
// and doing it by hand would mean raw ForkExec in a runtime that may have
// several threads, which is exactly the thing Go tells you not to do.
// prlimit sets the limits and then execs the real program, which is the
// documented way to get this right from a language without a pre-exec hook.
//
// util-linux is in the Debian and Alpine base images, and the Dockerfile
// installs it explicitly rather than relying on that.
type linuxSandbox struct{ prlimit string }

// newSandbox finds prlimit, or reports why limits cannot be applied.
func newSandbox() (sandbox, error) {
	path, err := exec.LookPath("prlimit")
	if err != nil {
		return nil, fmt.Errorf("prlimit (util-linux) is not installed: %w", err)
	}

	return linuxSandbox{prlimit: path}, nil
}

// wrap builds the prlimit command line.
//
// Only the limits that were set are passed: prlimit leaves a resource alone
// when its option is absent, so a zero field means "inherit" rather than
// "zero", which would make every unset limit forbid everything.
func (s linuxSandbox) wrap(path string, args []string, limits Limits) (string, []string) {
	out := make([]string, 0, len(args)+8)

	if limits.AddressSpace > 0 {
		out = append(out, "--as="+strconv.FormatInt(limits.AddressSpace, 10))
	}

	if limits.CPU > 0 {
		// RLIMIT_CPU is whole seconds, and rounding down would give zero for
		// anything under a second — a limit that kills the program
		// immediately. Rounding up is the only safe direction.
		seconds := int64(limits.CPU.Seconds())
		if limits.CPU.Seconds() > float64(seconds) {
			seconds++
		}

		out = append(out, "--cpu="+strconv.FormatInt(max(seconds, 1), 10))
	}

	if limits.FileSize > 0 {
		out = append(out, "--fsize="+strconv.FormatInt(limits.FileSize, 10))
	}

	if limits.Processes > 0 {
		out = append(out, "--nproc="+strconv.Itoa(limits.Processes))
	}

	if limits.OpenFiles > 0 {
		out = append(out, "--nofile="+strconv.Itoa(limits.OpenFiles))
	}

	// Nothing to limit: exec the program directly rather than add a process
	// to the tree for no reason.
	if len(out) == 0 {
		return path, args
	}

	out = append(out, "--", path)

	return s.prlimit, append(out, args...)
}

// isolate puts the program in its own process group, which is what makes
// killing its children possible.
func (linuxSandbox) isolate(proc *exec.Cmd) {
	proc.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// kill ends the whole group.
//
// The negative pid is the point: kill(-pgid) signals every process in the
// group, so ffmpeg's and yt-dlp's children die with their parent. Killing
// proc.Process alone would leave them running, holding the working directory
// open and the CPU busy long after the conversion was cancelled.
//
// SIGKILL rather than SIGTERM: these are converters with no state to flush,
// and a cancelled conversion should stop now rather than when the engine feels
// like it.
func (linuxSandbox) kill(proc *exec.Cmd) error {
	if proc.Process == nil {
		return nil
	}

	pgid, err := syscall.Getpgid(proc.Process.Pid)
	if err != nil {
		// The process is already gone, which is the common case in a race
		// between exiting and being killed.
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}

		// The group cannot be found, so fall back to the process itself:
		// killing one thing beats killing nothing.
		return proc.Process.Kill()
	}

	if err := syscall.Kill(-pgid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("kill process group %d: %w", pgid, err)
	}

	return nil
}

// maxRSS reads the peak resident memory the kernel reported, in bytes.
// ru_maxrss is in kilobytes on Linux.
func maxRSS(state *os.ProcessState) int64 {
	usage, ok := state.SysUsage().(*syscall.Rusage)
	if !ok {
		return 0
	}

	return usage.Maxrss * 1024
}
