package desktop

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// deadPID returns the pid of a process that has just exited.
func deadPID(t *testing.T) int {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^$")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	return cmd.Process.Pid
}

// A run that died while booting leaves a lock with no address. Its pid says
// nobody is booting any more, so the next start must not wait out the grace
// period for it.
func TestALockWhoseHolderIsGoneIsTakenOverAtOnce(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	body := `{"pid":` + strconv.Itoa(deadPID(t)) + `,"origin":""}`

	if err := os.WriteFile(filepath.Join(dir, lockFile), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	started := time.Now()

	l, err := acquireLock(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(l.release)

	if took := time.Since(started); took > startingGrace/4 {
		t.Fatalf("taking over a dead holder's lock took %s", took)
	}
}

// The check that keeps a live, still-booting holder from being pushed aside.
func TestProcessAliveSeesThisProcess(t *testing.T) {
	t.Parallel()

	if !processAlive(os.Getpid()) {
		t.Fatal("processAlive says this very process is dead")
	}
}
