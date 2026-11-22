package engine_test

import (
	"bytes"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AlexGalhardo/vukem-tools/internal/engine"
)

// The one question asked after a deploy is which versions are running, and the
// answer has to be in the log.
func TestEveryResolvedEngineLogsItsVersionAtStartup(t *testing.T) {
	// No t.Parallel: the version probe reads the inherited environment.

	t.Setenv("ENGINE_TEST_ROLE", "version")

	registry, err := engine.Resolve(t.Context(), lookup{"FAKE_BIN": self(t)}, []engine.Binary{
		{
			Name:        "fake",
			EnvVar:      "FAKE_BIN",
			VersionArgs: []string{"-test.run=TestEveryResolvedEngineLogsItsVersionAtStartup^$"},
		},
		{Name: "no-such-program-exists-anywhere"},
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	var out bytes.Buffer

	registry.Log(t.Context(), slog.New(slog.NewTextHandler(&out, nil)))

	logged := out.String()

	if !strings.Contains(logged, "engine resolved") || !strings.Contains(logged, "engine=fake") ||
		!strings.Contains(logged, `version="fake-engine 1.2.3 (build 42)"`) {
		t.Errorf("the resolved engine and its version are not in the log:\n%s", logged)
	}

	if !strings.Contains(logged, "level=WARN") || !strings.Contains(logged, "no-such-program-exists-anywhere") {
		t.Errorf("the missing engine is not logged as a warning:\n%s", logged)
	}
}

// repoGoFiles lists the non-test Go files under the module's own code, not
// the generated clients, vendored trees or frontend dependencies.
func repoGoFiles(t *testing.T, roots ...string) []string {
	t.Helper()

	var files []string

	for _, root := range roots {
		err := filepath.WalkDir(filepath.Join("..", "..", root), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if d.IsDir() && (d.Name() == "node_modules" || d.Name() == "testdata") {
				return filepath.SkipDir
			}

			if !d.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
				files = append(files, path)
			}

			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}

	if len(files) == 0 {
		t.Fatal("found no Go files; the walk is out of step with the layout")
	}

	return files
}

// No cgo: the binary is built static with CGO_ENABLED=0, so a file importing
// "C" would break the image build, and one that slipped past would pull a C
// decoder into the server process.
func TestNothingUsesCgo(t *testing.T) {
	t.Parallel()

	for _, path := range repoGoFiles(t, "app", "internal", "cmd", "bootstrap", "routes", "database") {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}

		for line := range strings.SplitSeq(string(data), "\n") {
			if strings.TrimSpace(line) == `import "C"` {
				t.Errorf("%s imports \"C\"", path)
			}
		}
	}

	dockerfile, err := os.ReadFile(filepath.Join("..", "..", "Dockerfile"))
	if err != nil {
		t.Fatalf("read the Dockerfile: %v", err)
	}

	if !strings.Contains(string(dockerfile), "CGO_ENABLED=0") {
		t.Error("the Dockerfile does not build with CGO_ENABLED=0")
	}
}

// Every engine runs through the Runner, which is where the timeout, the
// process-group kill, the limits, the network namespace and the minimal
// environment live. A conversion package calling os/exec itself would bypass
// all of them.
func TestOnlyTheRunnerStartsEngines(t *testing.T) {
	t.Parallel()

	for _, path := range repoGoFiles(t, "app", "internal") {
		if strings.Contains(filepath.ToSlash(path), "internal/engine/") {
			continue
		}

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}

		for line := range strings.SplitSeq(string(data), "\n") {
			code := strings.TrimSpace(line)
			if strings.HasPrefix(code, "//") {
				continue
			}

			if strings.Contains(code, "exec.Command(") || strings.Contains(code, "exec.CommandContext(") {
				t.Errorf("%s starts a program without the engine runner: %s", path, code)
			}
		}
	}
}
