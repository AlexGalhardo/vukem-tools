package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

// Limits bound one run. A zero field means that limit is not applied, which
// is only right in development: Runner refuses to start in production
// without the ones that matter.
type Limits struct {
	// Timeout is the wall-clock bound. It is the only limit that works on
	// every platform, and the only one that stops a program that is waiting
	// rather than computing.
	Timeout time.Duration
	// AddressSpace caps the virtual memory the program may map (RLIMIT_AS),
	// in bytes. It is what stops a decompression bomb: a malformed image
	// claiming 60000x60000 pixels asks for the memory before it does any
	// work, so the allocation fails and the program exits instead of the
	// container being killed.
	AddressSpace int64
	// CPU caps processor time (RLIMIT_CPU), rounded up to a second. It
	// catches a program spinning inside a loop the timeout would also catch,
	// but sooner and with less wasted work.
	CPU time.Duration
	// FileSize caps any single file the program writes (RLIMIT_FSIZE), in
	// bytes. A conversion that would fill the disk fails as one job rather
	// than taking the worker and every other job with it.
	FileSize int64
	// Processes caps how many processes the user may have (RLIMIT_NPROC), so
	// an engine cannot fork its way out of its own limits.
	Processes int
	// OpenFiles caps descriptors (RLIMIT_NOFILE).
	OpenFiles int
}

// Command is one program to run.
type Command struct {
	// Path is the absolute path of the program, from a Registry.
	Path string
	Args []string
	// Dir is the working directory. It is required: every conversion gets its
	// own directory, and a program inheriting the server's working directory
	// is a program that can write next to the binary.
	Dir string
	// Stdin is optional. Most engines read a file path instead.
	Stdin io.Reader
	// Stdout receives the program's output. Nil discards it, which is right
	// for an engine that writes to a file.
	Stdout io.Writer
	// Progress receives the program's progress stream when it has one
	// (ffmpeg's `-progress pipe:1`), line by line. It is separate from
	// Stdout so a tool can read progress without also buffering output.
	Progress func(line string)
	// Env is added to a deliberately minimal environment. Nothing is
	// inherited: the server's environment holds database credentials and API
	// keys, and an engine has no business seeing them.
	Env []string
}

// Result is what a finished program reported.
type Result struct {
	ExitCode int
	// CPUTime is the user plus system time the program actually used, which
	// is the honest figure to charge a quota with.
	CPUTime time.Duration
	// MaxRSS is the peak resident memory in bytes, zero where the platform
	// does not report it.
	MaxRSS int64
	// Stderr is the tail of what the program wrote there. It goes in the
	// owner-only detail of a failure, never to a client: an engine's error
	// text routinely contains the input path and the whole command line.
	Stderr string
}

// stderrTail is how much of stderr is kept. Enough for the message and a
// little context, bounded so an engine printing a megabyte of warnings
// cannot grow a log line without limit.
const stderrTail = 8 << 10

// Runner runs engines under Limits.
type Runner struct {
	limits  Limits
	log     *slog.Logger
	sandbox sandbox
}

// RunnerOptions configures a Runner.
type RunnerOptions struct {
	Limits Limits
	// Production makes a missing sandbox a construction error rather than a
	// warning. A conversion service that runs untrusted files through C
	// decoders without limits is one bad file away from an outage, so the
	// unsafe configuration has to be impossible to deploy by accident.
	Production bool
	Log        *slog.Logger
}

// NewRunner returns a runner.
func NewRunner(o RunnerOptions) (*Runner, error) {
	if o.Limits.Timeout <= 0 {
		return nil, errors.New("engine: a runner needs a timeout")
	}

	if o.Log == nil {
		o.Log = slog.Default()
	}

	box, err := newSandbox()
	if err != nil {
		if o.Production {
			return nil, fmt.Errorf("engine: no way to limit a subprocess: %w", err)
		}

		o.Log.Warn("engine: subprocess limits are not enforced on this platform", "reason", err)

		box = nopSandbox{}
	}

	return &Runner{limits: o.Limits, log: o.Log, sandbox: box}, nil
}

// ErrTimeout means the program was still running at its deadline and was
// killed. It is separate from a context cancellation so a tool can tell "this
// file takes too long" from "the owner cancelled".
var ErrTimeout = errors.New("engine: the program exceeded its time limit")

// ExitError reports a program that ran and failed. The stderr tail is on it
// rather than in the message, so a caller decides what reaches a user.
type ExitError struct {
	Code   int
	Stderr string
}

// Error implements error.
func (e *ExitError) Error() string {
	if e.Stderr == "" {
		return fmt.Sprintf("engine: exit status %d", e.Code)
	}

	return fmt.Sprintf("engine: exit status %d: %s", e.Code, firstLine(e.Stderr))
}

// Run executes cmd and returns what it reported.
//
// The program gets its own process group, so a timeout or a cancellation kills
// the engine *and anything it spawned*: ffmpeg and yt-dlp both start children,
// and killing only the parent leaves those running, holding the working
// directory open and the CPU busy. That is the difference between a cancelled
// conversion and a leak.
func (r *Runner) Run(ctx context.Context, cmd Command) (Result, error) {
	if cmd.Path == "" {
		return Result{}, errors.New("engine: no program to run")
	}

	if cmd.Dir == "" {
		return Result{}, errors.New("engine: a program needs its own working directory")
	}

	ctx, cancel := context.WithTimeout(ctx, r.limits.Timeout)
	defer cancel()

	path, args := r.sandbox.wrap(cmd.Path, cmd.Args, r.limits)

	proc := exec.CommandContext(ctx, path, args...)
	// CommandContext kills only the process itself, which would leave
	// ffmpeg's and yt-dlp's children running — so Cancel replaces that with
	// a kill of the whole group. WaitDelay then bounds how long Wait waits
	// for the pipes to close after the kill, so a child holding stdout open
	// cannot hang the worker.
	proc.Cancel = func() error { return r.sandbox.kill(proc) }
	proc.WaitDelay = killGrace
	proc.Dir = cmd.Dir
	proc.Stdin = cmd.Stdin
	// A minimal environment, not the server's: that one holds the database
	// URL, the mail token and the bucket credentials.
	//
	// TMPDIR, TEMP and TMP point at the job's own directory, and that is a
	// containment boundary rather than tidiness. Ghostscript's -dSAFER
	// permits writing to the temp directory, so with the default TMPDIR a
	// hostile PostScript program can write anywhere under /tmp — verified
	// against Ghostscript 10.05.1, where the same document is refused with
	// /invalidfileaccess once TMPDIR is the job directory. ffmpeg's muxers
	// and yt-dlp's fragment handling put their scratch files here too, so
	// everything an engine writes now lands in the directory that is deleted
	// when the job ends.
	proc.Env = append([]string{
		"PATH=/usr/local/bin:/usr/bin:/bin",
		"HOME=" + cmd.Dir,
		"TMPDIR=" + cmd.Dir,
		"TEMP=" + cmd.Dir,
		"TMP=" + cmd.Dir,
	}, cmd.Env...)

	stderr := &tailWriter{limit: stderrTail}
	proc.Stderr = stderr

	progress, waitProgress, err := r.attachStdout(proc, cmd)
	if err != nil {
		return Result{}, err
	}

	r.sandbox.isolate(proc)

	if err := proc.Start(); err != nil {
		return Result{}, fmt.Errorf("engine: start %s: %w", cmd.Path, err)
	}

	waitErr := proc.Wait()

	if progress != nil {
		_ = progress.Close()
	}

	waitProgress()

	result := Result{Stderr: stderr.String()}
	if proc.ProcessState != nil {
		result.ExitCode = proc.ProcessState.ExitCode()
		result.CPUTime = proc.ProcessState.UserTime() + proc.ProcessState.SystemTime()
		result.MaxRSS = maxRSS(proc.ProcessState)
	}

	// The deadline is checked before the exit status: a killed program exits
	// non-zero, and reporting that as "the converter rejected your file"
	// would be wrong and would send the user looking at the file.
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return result, ErrTimeout
	}

	if ctx.Err() != nil {
		return result, ctx.Err()
	}

	if waitErr != nil {
		return result, &ExitError{Code: result.ExitCode, Stderr: result.Stderr}
	}

	return result, nil
}

// attachStdout wires Stdout and the progress reader, and returns a closer for
// the pipe plus a function that waits for the reader to finish.
func (r *Runner) attachStdout(proc *exec.Cmd, cmd Command) (io.Closer, func(), error) {
	if cmd.Progress == nil {
		proc.Stdout = cmd.Stdout

		return nil, func() {}, nil
	}

	pipe, err := proc.StdoutPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("engine: progress pipe: %w", err)
	}

	done := make(chan struct{})

	go func() {
		defer close(done)

		scanLines(pipe, cmd.Progress)
	}()

	return pipe, func() { <-done }, nil
}

// killGrace is how long Wait waits after the kill for the program's pipes to
// close. A child that inherited stdout and will not die must not hold the
// worker, and five seconds is long enough for an ordinary exit.
const killGrace = 5 * time.Second

// firstLine is the part of an engine's error worth putting in a message.
func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")

	return strings.TrimSpace(line)
}

// tailWriter keeps the last limit bytes written to it.
//
// The tail rather than the head: an engine prints its warnings first and its
// reason for failing last, so the end is the part worth keeping.
type tailWriter struct {
	limit int
	buf   []byte
}

func (w *tailWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)

	if len(w.buf) > w.limit {
		w.buf = w.buf[len(w.buf)-w.limit:]
	}

	return len(p), nil
}

func (w *tailWriter) String() string { return strings.TrimSpace(string(w.buf)) }

// scanLines calls fn for each line in r, without a line-length limit that a
// long progress line could trip.
func scanLines(r io.Reader, fn func(string)) {
	buf := make([]byte, 4096)

	var pending []byte

	for {
		n, err := r.Read(buf)

		if n > 0 {
			pending = append(pending, buf[:n]...)

			for {
				index := indexByte(pending, '\n')
				if index < 0 {
					break
				}

				fn(strings.TrimRight(string(pending[:index]), "\r"))
				pending = pending[index+1:]
			}
		}

		if err != nil {
			if len(pending) > 0 {
				fn(strings.TrimRight(string(pending), "\r"))
			}

			return
		}
	}
}

func indexByte(b []byte, c byte) int {
	for i := range b {
		if b[i] == c {
			return i
		}
	}

	return -1
}
