//go:build linux

package engine_test

import (
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/AlexGalhardo/vukem-tools/internal/engine"
	"github.com/AlexGalhardo/vukem-tools/internal/testenv"
)

// softLimits reads the soft column of /proc/<pid>/limits, keyed by the
// resource name the kernel prints ("Max address space"). "unlimited" is
// returned as -1.
func softLimits(t *testing.T, table string) map[string]int64 {
	t.Helper()

	out := map[string]int64{}

	for line := range strings.SplitSeq(table, "\n") {
		if !strings.HasPrefix(line, "Max ") {
			continue
		}

		// The name has spaces, the three columns after it do not.
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}

		for i, f := range fields {
			if f != "unlimited" && (f[0] < '0' || f[0] > '9') {
				continue
			}

			name := strings.Join(fields[:i], " ")

			if f == "unlimited" {
				out[name] = -1

				break
			}

			n, err := strconv.ParseInt(f, 10, 64)
			if err != nil {
				t.Fatalf("limit %q is not a number: %v", line, err)
			}

			out[name] = n

			break
		}
	}

	return out
}

// The runner promises setrlimit before exec for the four limits the engines
// rule names. Reading /proc/self/limits from inside the engine checks what the
// kernel applied, not what the runner meant to ask for.
func TestTheEngineRunsUnderTheLimitsTheRunnerWasGiven(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("prlimit"); err != nil {
		testenv.MissingEngine(t, "prlimit is not installed; the container installs util-linux explicitly")
	}

	limits := engine.Limits{
		Timeout:      20 * time.Second,
		AddressSpace: 4 << 30,
		CPU:          90 * time.Second,
		FileSize:     123 << 20,
		Processes:    64,
		OpenFiles:    512,
	}

	var out strings.Builder

	if _, err := run(t, newRunner(t, limits), "limits", engine.Command{Stdout: &out}); err != nil {
		t.Fatalf("run: %v", err)
	}

	got := softLimits(t, out.String())

	want := map[string]int64{
		"Max address space": limits.AddressSpace,
		"Max cpu time":      90,
		"Max file size":     limits.FileSize,
		"Max open files":    512,
	}

	for name, value := range want {
		if got[name] != value {
			t.Errorf("%s is %d, want %d\n%s", name, got[name], value, out.String())
		}
	}

	// RLIMIT_NPROC is headroom above what the UID already owns, so the exact
	// figure depends on the machine; it must be set and not below the headroom.
	if nproc := got["Max processes"]; nproc < int64(limits.Processes) {
		t.Errorf("Max processes is %d, want at least %d\n%s", nproc, limits.Processes, out.String())
	}
}

// A zero field means the limit is not applied; it must not turn into a limit
// of zero, which would forbid the engine everything.
func TestAnUnsetLimitLeavesTheKernelDefault(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("prlimit"); err != nil {
		testenv.MissingEngine(t, "prlimit is not installed; the container installs util-linux explicitly")
	}

	var out strings.Builder

	if _, err := run(
		t,
		newRunner(t, engine.Limits{FileSize: 1 << 30}),
		"limits",
		engine.Command{Stdout: &out},
	); err != nil {
		t.Fatalf("run: %v", err)
	}

	if got := softLimits(t, out.String()); got["Max address space"] != -1 {
		t.Errorf("an unset address-space limit became %d, want unlimited", got["Max address space"])
	}
}
