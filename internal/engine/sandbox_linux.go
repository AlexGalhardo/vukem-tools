//go:build linux

package engine

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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
		// Headroom above what the user already owns, not an absolute number.
		// RLIMIT_NPROC is checked against every task the real UID holds on
		// the machine rather than the ones this process started, so a fixed
		// figure budgets for the UID's other tenants as much as for the
		// engine: 64 is generous in a container where the binary is nearly
		// alone, and instantly fatal on a CI runner whose user already owns
		// hundreds of threads, where libvips dies with "glib: Error creating
		// thread: Resource temporarily unavailable" before it reads a pixel.
		out = append(out, "--nproc="+strconv.Itoa(uidTasks()+limits.Processes))
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

// uidTasks counts the tasks the real UID already owns, which is the number
// RLIMIT_NPROC is measured against.
//
// Zero when /proc cannot be read, which turns the headroom back into an
// absolute cap — the behaviour this replaced, and the safe direction to fail
// in. Inside a PID namespace it counts the container's own tasks, which is
// also what the kernel counts when the container has its own user namespace.
func uidTasks() int {
	entries, err := os.ReadDir(procfs)
	if err != nil {
		return 0
	}

	uid := strconv.Itoa(os.Getuid())
	total := 0

	for _, entry := range entries {
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}

		// A pid is free to disappear between the listing and the read, which
		// is ordinary rather than a failure.
		status, err := os.ReadFile(filepath.Join(procfs, entry.Name(), "status"))
		if err != nil {
			continue
		}

		if owner, threads := statusTasks(string(status)); owner == uid {
			total += threads
		}
	}

	return total
}

// procfs is where the task counts live.
const procfs = "/proc"

// statusTasks reads the real UID and the thread count out of a
// /proc/<pid>/status. A process whose status has no Threads: line counts as
// one task.
func statusTasks(status string) (uid string, threads int) {
	threads = 1

	for line := range strings.SplitSeq(status, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		switch fields[0] {
		// "Uid: real effective saved fs" — the real one is what the kernel
		// counts the tasks against.
		case "Uid:":
			uid = fields[1]
		case "Threads:":
			if n, err := strconv.Atoi(fields[1]); err == nil {
				threads = n
			}
		}
	}

	return uid, threads
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
