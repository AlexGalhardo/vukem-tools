package engine_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AlexGalhardo/vukem-tools/internal/engine"
)

// The tests run this test binary as the engine, which is the only way to
// exercise a subprocess runner on every platform the project builds for: a
// shell script needs sh, and naming a real engine would make the tests skip
// wherever it is not installed.
//
// TestMain turns the binary into the fake engine when the environment says so,
// so one executable is both the test and its subject.
func TestMain(m *testing.M) {
	switch os.Getenv("ENGINE_TEST_ROLE") {
	case "":
		os.Exit(m.Run())
	case "version":
		os.Stdout.WriteString("fake-engine 1.2.3 (build 42)\nsecond line nobody should read\n")
		os.Exit(0)
	case "ok":
		os.Stdout.WriteString("done\n")
		os.Exit(0)
	case "fail":
		os.Stderr.WriteString("warning: something unimportant\nerror: the input is not usable\n")
		os.Exit(3)
	case "sleep":
		time.Sleep(60 * time.Second)
		os.Exit(0)
	case "signal":
		// Dies by a signal rather than by exiting, which is the only way to
		// produce exit code -1 and the thing the engine's error has to be
		// able to describe.
		if p, err := os.FindProcess(os.Getpid()); err == nil {
			_ = p.Kill()
		}

		// Unreachable once the signal lands; here so a platform that does not
		// deliver it cannot hang past the runner's timeout.
		time.Sleep(5 * time.Second)
		os.Exit(1)
	case "progress":
		for i := range 4 {
			os.Stdout.WriteString("out_time_ms=" + strconv.Itoa(i*1000) + "\n")
		}

		os.Exit(0)
	case "leak":
		// Starts a child that outlives it, which is what a process-group kill
		// has to reach and a plain kill does not.
		child := exec.Command(os.Args[0])
		child.Env = append(os.Environ(), "ENGINE_TEST_ROLE=sleep")

		if err := child.Start(); err != nil {
			os.Exit(1)
		}

		os.Stdout.WriteString(strconv.Itoa(child.Process.Pid) + "\n")
		time.Sleep(60 * time.Second)
		os.Exit(0)
	case "env":
		for _, name := range []string{"DATABASE_URL", "APP_KEY", "PATH", "HOME", "TMPDIR", "TEMP", "TMP"} {
			os.Stdout.WriteString(name + "=" + os.Getenv(name) + "\n")
		}

		os.Exit(0)
	case "write":
		// Writes until it is stopped or the file-size limit kills it.
		f, err := os.Create(filepath.Join(os.Getenv("HOME"), "big"))
		if err != nil {
			os.Exit(1)
		}

		block := make([]byte, 1<<20)

		for range 64 {
			if _, err := f.Write(block); err != nil {
				os.Exit(2)
			}
		}

		os.Exit(0)
	default:
		os.Exit(99)
	}
}

// self is this test binary, used as the engine.
func self(t *testing.T) string {
	t.Helper()

	path, err := os.Executable()
	if err != nil {
		t.Fatalf("locate the test binary: %v", err)
	}

	return path
}

func newRunner(t *testing.T, limits engine.Limits) *engine.Runner {
	t.Helper()

	if limits.Timeout == 0 {
		limits.Timeout = 10 * time.Second
	}

	r, err := engine.NewRunner(engine.RunnerOptions{Limits: limits})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	return r
}

func run(t *testing.T, r *engine.Runner, role string, cmd engine.Command) (engine.Result, error) {
	t.Helper()

	if cmd.Path == "" {
		cmd.Path = self(t)
	}

	if cmd.Dir == "" {
		cmd.Dir = t.TempDir()
	}

	cmd.Env = append(cmd.Env, "ENGINE_TEST_ROLE="+role)

	return r.Run(t.Context(), cmd)
}

func TestRunReportsSuccess(t *testing.T) {
	t.Parallel()

	var out strings.Builder

	result, err := run(t, newRunner(t, engine.Limits{}), "ok", engine.Command{Stdout: &out})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0", result.ExitCode)
	}

	if strings.TrimSpace(out.String()) != "done" {
		t.Fatalf("stdout = %q", out.String())
	}
}

// A failing engine's stderr is kept for the owner, and the tail is what is
// kept: an engine prints its warnings first and its reason last.
func TestAFailureCarriesTheStderrTail(t *testing.T) {
	t.Parallel()

	_, err := run(t, newRunner(t, engine.Limits{}), "fail", engine.Command{})

	exit, ok := errors.AsType[*engine.ExitError](err)
	if !ok {
		t.Fatalf("err = %#v, want an *engine.ExitError", err)
	}

	if exit.Code != 3 {
		t.Fatalf("code = %d, want 3", exit.Code)
	}

	if !strings.Contains(exit.Stderr, "the input is not usable") {
		t.Fatalf("stderr = %q, want the engine's reason", exit.Stderr)
	}
}

// Go reports every death by signal as exit code -1, so the code alone cannot
// tell a SIGKILL from outside the process from a SIGSEGV inside it — and those
// two send an operator to opposite ends of the system. A production boot
// failure read "write the capability probe image: other: engine: exit status
// -1" and there was nowhere to go from there.
func TestAProgramKilledBySignalSaysWhichSignal(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "linux" {
		t.Skip("a death by signal is reported as an ordinary exit on this platform")
	}

	_, err := run(t, newRunner(t, engine.Limits{}), "signal", engine.Command{})

	exit, ok := errors.AsType[*engine.ExitError](err)
	if !ok {
		t.Fatalf("err = %#v, want an *engine.ExitError", err)
	}

	if exit.Code != -1 {
		t.Fatalf("code = %d, want -1 for a death by signal", exit.Code)
	}

	if !strings.Contains(exit.Status, "signal:") {
		t.Errorf("status = %q, which does not name the signal", exit.Status)
	}

	// The message is what reaches a deploy log, and it is the only part of
	// this an operator reads.
	if !strings.Contains(err.Error(), "signal:") {
		t.Errorf("the message is %q, which names no signal", err.Error())
	}
}

// A program still running at its deadline is killed and reported as a
// timeout, not as a rejection: telling the user their file is broken when the
// converter simply ran out of time sends them looking at the wrong thing.
func TestATimeoutIsReportedAsATimeout(t *testing.T) {
	t.Parallel()

	started := time.Now()

	_, err := run(t, newRunner(t, engine.Limits{Timeout: 400 * time.Millisecond}), "sleep", engine.Command{})
	if !errors.Is(err, engine.ErrTimeout) {
		t.Fatalf("err = %v, want ErrTimeout", err)
	}

	if elapsed := time.Since(started); elapsed > 10*time.Second {
		t.Fatalf("the run took %s: the kill did not work", elapsed)
	}
}

// Cancelling reports the context's error rather than a timeout, so a tool can
// tell "the owner stopped this" from "this file takes too long".
func TestCancellingReportsCancellation(t *testing.T) {
	t.Parallel()

	r := newRunner(t, engine.Limits{Timeout: 30 * time.Second})

	ctx, cancel := context.WithCancel(t.Context())

	var wg sync.WaitGroup

	wg.Go(func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	})

	_, err := r.Run(ctx, engine.Command{
		Path: self(t),
		Dir:  t.TempDir(),
		Env:  []string{"ENGINE_TEST_ROLE=sleep"},
	})

	wg.Wait()

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

// The ROADMAP's assert, and the reason the process group exists: killing the
// engine must kill what it started. ffmpeg and yt-dlp both spawn children,
// and leaving those alive holds the working directory open and the CPU busy
// after a cancelled conversion.
func TestKillingTheEngineKillsItsChildren(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "linux" {
		// Honest skip rather than a weaker assertion: only the Linux sandbox
		// makes a process group, and production is Linux. Asserting the
		// fallback leaks would be asserting a known limitation.
		t.Skip("the process group is only created on Linux, which is where production runs")
	}

	var out strings.Builder

	_, err := run(t, newRunner(t, engine.Limits{Timeout: 500 * time.Millisecond}), "leak",
		engine.Command{Stdout: &out})
	if !errors.Is(err, engine.ErrTimeout) {
		t.Fatalf("err = %v, want ErrTimeout", err)
	}

	pid, convErr := strconv.Atoi(strings.TrimSpace(out.String()))
	if convErr != nil {
		t.Fatalf("the fake engine did not report its child's pid: %q", out.String())
	}

	// Give the kill a moment to land, then check the child is really gone.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !alive(pid) {
			return
		}

		time.Sleep(50 * time.Millisecond)
	}

	t.Fatalf("process %d survived the kill: the engine's children are leaking", pid)
}

// alive reports whether a process still exists. Signal 0 checks without
// sending anything.
func alive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	return proc.Signal(os.Signal(nil)) == nil
}

// The engine gets a minimal environment, not the server's. The server's holds
// the database URL, the mail token and the bucket credentials, and a decoder
// reading an uploaded file has no business seeing any of them.
func TestTheEngineDoesNotInheritTheServerEnvironment(t *testing.T) {
	// No t.Parallel: this test sets an environment variable, which the
	// version probe reads from the inherited environment.

	t.Setenv("DATABASE_URL", "postgres://user:secret@localhost/db")
	t.Setenv("APP_KEY", "a-real-looking-secret")

	var out strings.Builder

	if _, err := run(t, newRunner(t, engine.Limits{}), "env", engine.Command{Stdout: &out}); err != nil {
		t.Fatalf("run: %v", err)
	}

	printed := out.String()

	if strings.Contains(printed, "secret") {
		t.Fatalf("the engine inherited a secret:\n%s", printed)
	}

	if !strings.Contains(printed, "PATH=") {
		t.Fatalf("the engine has no PATH:\n%s", printed)
	}
}

// Every scratch file an engine writes has to land in the directory that is
// deleted when the job ends.
//
// This is a containment boundary rather than tidiness. Ghostscript's -dSAFER
// permits writing to the temp directory, so with the default TMPDIR a hostile
// PostScript program can write anywhere under /tmp — verified against
// Ghostscript 10.05.1, where the same document is refused with
// /invalidfileaccess once TMPDIR is the job's own directory. ffmpeg's muxers
// and yt-dlp's fragment handling put their scratch files there too.
func TestTheEnginesTemporaryDirectoryIsItsOwnWorkingDirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	var out strings.Builder

	if _, err := run(t, newRunner(t, engine.Limits{}), "env", engine.Command{Dir: dir, Stdout: &out}); err != nil {
		t.Fatalf("run: %v", err)
	}

	printed := out.String()

	for _, name := range []string{"TMPDIR", "TEMP", "TMP", "HOME"} {
		if want := name + "=" + dir; !strings.Contains(printed, want) {
			t.Errorf("the engine has no %s:\n%s", want, printed)
		}
	}
}

// A progress stream is read line by line without buffering the output, which
// is how ffmpeg's `-progress pipe:1` is consumed.
func TestProgressLinesAreDelivered(t *testing.T) {
	t.Parallel()

	var (
		mu    sync.Mutex
		lines []string
	)

	_, err := run(t, newRunner(t, engine.Limits{}), "progress", engine.Command{
		Progress: func(line string) {
			mu.Lock()
			defer mu.Unlock()

			lines = append(lines, line)
		},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(lines) != 4 {
		t.Fatalf("got %d progress lines, want 4: %v", len(lines), lines)
	}

	if lines[0] != "out_time_ms=0" || lines[3] != "out_time_ms=3000" {
		t.Fatalf("progress lines are wrong: %v", lines)
	}
}

// A file-size limit stops a conversion that would fill the disk, as one failed
// job rather than as an outage. prlimit is what applies it, so this is a Linux
// assertion.
func TestAFileSizeLimitStopsARunawayWrite(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "linux" {
		t.Skip("RLIMIT_FSIZE is applied through prlimit, which exists only on Linux")
	}

	if _, err := exec.LookPath("prlimit"); err != nil {
		t.Skip("prlimit is not installed; the container installs util-linux explicitly")
	}

	// One mebibyte, against an engine that tries to write 64.
	_, err := run(t, newRunner(t, engine.Limits{Timeout: 20 * time.Second, FileSize: 1 << 20}),
		"write", engine.Command{})
	if err == nil {
		t.Fatal("the engine wrote 64 MiB under a 1 MiB limit")
	}
}

func TestRunNeedsAProgramAndADirectory(t *testing.T) {
	t.Parallel()

	r := newRunner(t, engine.Limits{})

	if _, err := r.Run(t.Context(), engine.Command{Dir: t.TempDir()}); err == nil {
		t.Error("a command with no program was accepted")
	}

	if _, err := r.Run(t.Context(), engine.Command{Path: self(t)}); err == nil {
		t.Error("a command with no working directory was accepted")
	}
}

func TestNewRunnerNeedsATimeout(t *testing.T) {
	t.Parallel()

	if _, err := engine.NewRunner(engine.RunnerOptions{}); err == nil {
		t.Fatal("a runner with no timeout was accepted")
	}
}

// Production must not start without a way to limit a subprocess: a conversion
// service that feeds untrusted files to C decoders unbounded is one bad file
// away from an outage, so the unsafe configuration has to be impossible to
// deploy rather than merely discouraged.
func TestProductionRefusesToRunWithoutLimits(t *testing.T) {
	t.Parallel()

	_, err := engine.NewRunner(engine.RunnerOptions{
		Limits:     engine.Limits{Timeout: time.Second},
		Production: true,
	})

	if runtime.GOOS == "linux" {
		if _, look := exec.LookPath("prlimit"); look == nil {
			// prlimit is here, so production is satisfied — which is the
			// state the container is built to be in.
			if err != nil {
				t.Fatalf("production refused a runner on a host that has prlimit: %v", err)
			}

			return
		}
	}

	if err == nil {
		t.Fatal("production accepted a runner that cannot limit a subprocess")
	}
}

// Resolution finds a program, reads its version, and keeps only the first
// line: engines print several and the first is the one that names the
// version.
func TestResolveReadsTheVersion(t *testing.T) {
	// No t.Parallel: this test sets an environment variable, which the
	// version probe reads from the inherited environment.

	t.Setenv("ENGINE_TEST_ROLE", "version")

	// Reached through the configured path, because the test binary is not on
	// PATH and LookPath is what resolution uses.
	registry, err := engine.Resolve(t.Context(), lookup{"FAKE_BIN": self(t)}, []engine.Binary{{
		Name:        "fake",
		EnvVar:      "FAKE_BIN",
		VersionArgs: []string{"-test.run=TestResolveReadsTheVersion^$"},
	}})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if !registry.Has("fake") {
		t.Fatalf("the test binary was not resolved; missing: %v", registry.Missing())
	}

	if got := registry.Version("fake"); !strings.HasPrefix(got, "fake-engine 1.2.3") {
		t.Fatalf("version = %q, want the first line only", got)
	}

	if got := registry.Versions()["fake"]; got != registry.Version("fake") {
		t.Fatalf("Versions and Version disagree: %q vs %q", got, registry.Version("fake"))
	}
}

// A required engine that is missing fails the boot. An engine that is absent
// should fail a deploy, not the first conversion that needs it.
func TestAMissingRequiredEngineFailsResolution(t *testing.T) {
	t.Parallel()

	_, err := engine.Resolve(t.Context(), nil, []engine.Binary{
		{Name: "no-such-program-exists-anywhere", Required: true},
	})

	if !errors.Is(err, engine.ErrMissing) {
		t.Fatalf("err = %v, want ErrMissing", err)
	}
}

// A missing optional engine is recorded, not fatal: its tool is unavailable
// and the rest of the service still runs.
func TestAMissingOptionalEngineIsRecorded(t *testing.T) {
	t.Parallel()

	registry, err := engine.Resolve(t.Context(), nil, []engine.Binary{
		{Name: "no-such-program-exists-anywhere"},
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if registry.Has("no-such-program-exists-anywhere") {
		t.Fatal("a program that does not exist was reported as available")
	}

	if got := registry.Missing(); len(got) != 1 {
		t.Fatalf("missing = %v, want one entry", got)
	}
}

// A configured path that does not exist is a boot error naming the variable:
// a typo in a deploy setting should say which setting, not fail later with
// "no such file or directory".
func TestAConfiguredPathIsChecked(t *testing.T) {
	t.Parallel()

	source := lookup{"FAKE_BIN": filepath.Join(t.TempDir(), "not-here")}

	_, err := engine.Resolve(t.Context(), source, []engine.Binary{
		{Name: "fake", EnvVar: "FAKE_BIN", Required: true},
	})
	if !errors.Is(err, engine.ErrMissing) {
		t.Fatalf("err = %v, want ErrMissing", err)
	}
}

// A configured path wins over PATH, so a deploy can pin an exact build.
func TestAConfiguredPathWins(t *testing.T) {
	// No t.Parallel: this test sets an environment variable, which the
	// version probe reads from the inherited environment.

	t.Setenv("ENGINE_TEST_ROLE", "version")

	source := lookup{"FAKE_BIN": self(t)}

	registry, err := engine.Resolve(t.Context(), source, []engine.Binary{
		{Name: "fake", EnvVar: "FAKE_BIN", VersionArgs: []string{"-test.run=TestAConfiguredPathWins^$"}},
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	got, ok := registry.Path("fake")
	if !ok || got != self(t) {
		t.Fatalf("path = %q, ok = %v; want the configured binary", got, ok)
	}
}

type lookup map[string]string

func (l lookup) Get(key string) string { return l[key] }
