//go:build linux

package engine

import (
	"strconv"
	"strings"
	"testing"
)

func TestStatusTasksReadsTheRealUIDAndTheThreadCount(t *testing.T) {
	status := "Name:\tvips\nState:\tS (sleeping)\nUid:\t10001\t10001\t10001\t10001\nThreads:\t9\n"

	uid, threads := statusTasks(status)
	if uid != "10001" {
		t.Errorf("real uid read as %q, want 10001", uid)
	}

	if threads != 9 {
		t.Errorf("thread count read as %d, want 9", threads)
	}
}

func TestAProcessWithNoThreadLineCountsAsOne(t *testing.T) {
	// Counting it as zero would make a kernel thread or a truncated read
	// lower the headroom the engine gets, which is the wrong direction.
	_, threads := statusTasks("Uid:\t0\t0\t0\t0\n")
	if threads != 1 {
		t.Errorf("a status with no Threads: line counted %d tasks, want 1", threads)
	}
}

func TestTheProcessLimitIsHeadroomAboveWhatTheUIDAlreadyOwns(t *testing.T) {
	// RLIMIT_NPROC is measured against every task of the real UID, so passing
	// the configured figure straight through spends the engine's budget on
	// whatever else that user is running. On a CI runner whose user owns
	// hundreds of threads it leaves nothing, and libvips dies with "glib:
	// Error creating thread: Resource temporarily unavailable".
	const want = 64

	_, args := linuxSandbox{prlimit: "/usr/bin/prlimit"}.
		wrap("/usr/bin/vips", []string{"--version"}, Limits{Processes: want})

	nproc := 0

	for _, arg := range args {
		after, ok := strings.CutPrefix(arg, "--nproc=")
		if !ok {
			continue
		}

		n, err := strconv.Atoi(after)
		if err != nil {
			t.Fatalf("--nproc is not a number: %q", arg)
		}

		nproc = n
	}

	// This test binary is itself a task the UID owns, so the headroom is
	// always above the configured figure on a machine with a readable /proc.
	if nproc <= want {
		t.Errorf("--nproc is %d, which is the configured %d or less: the limit is still an "+
			"absolute ceiling rather than headroom above the UID's own tasks", nproc, want)
	}
}
