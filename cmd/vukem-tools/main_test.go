package main

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestTheVersionDefaultsToDev(t *testing.T) {
	t.Parallel()

	if version != "dev" {
		t.Fatalf("version = %q, want dev when not built with -X", version)
	}
}

// The release workflow stamps the tag with -X main.version; this builds the
// binary the same way and asks it.
func TestTheReleaseLdflagStampsTheVersion(t *testing.T) {
	t.Parallel()

	bin := filepath.Join(t.TempDir(), "vukem-tools")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}

	build := exec.CommandContext(t.Context(), "go", "build", "-ldflags", "-X main.version=v9.8.7", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	for _, arg := range []string{"version", "--version"} {
		out, err := exec.CommandContext(t.Context(), bin, arg).Output()
		if err != nil || string(out) != "v9.8.7\n" {
			t.Errorf("%s printed %q (%v), want v9.8.7 on one line", arg, out, err)
		}
	}
}
