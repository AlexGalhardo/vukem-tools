package pdf_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/AlexGalhardo/vukem-tools/internal/engine"
	"github.com/AlexGalhardo/vukem-tools/internal/pdf"
)

// envLookup resolves an engine path from the environment, the same variables
// the app reads, so a developer with Ghostscript somewhere unusual can point
// the tests at it.
type envLookup struct{}

func (envLookup) Get(key string) string { return os.Getenv(key) }

// testBinaries mirrors the engine declarations in app/services/engines.go for
// the engines this package uses. It is duplicated rather than imported
// because app/services resolves them from the application's configuration,
// which a library test has none of.
var testBinaries = []engine.Binary{
	{Name: pdf.EngineQPDF, EnvVar: "QPDF_PATH", VersionArgs: []string{"--version"}},
	{Name: pdf.EngineGhostscript, EnvVar: "GHOSTSCRIPT_PATH", VersionArgs: []string{"--version"}},
	{Name: pdf.EnginePDFToPPM, EnvVar: "PDFTOPPM_PATH", VersionArgs: []string{"-v"}},
	{Name: pdf.EnginePDFToText, EnvVar: "PDFTOTEXT_PATH", VersionArgs: []string{"-v"}},
	{Name: pdf.EngineVips, EnvVar: "VIPS_PATH", VersionArgs: []string{"--version"}},
}

// testEngines returns an engine set, skipping the test when one of the
// engines it needs is not installed.
//
// It skips rather than fails, and says where the assertion does run. The
// engines live in the production image and the subprocess sandbox is
// Linux-only, so these tests are green on a development machine only because
// they did not run — docs/deployment.md § "Verifying the image" is how they
// are actually executed, and a skip that did not name it would read as a pass.
func testEngines(t *testing.T, need ...string) *pdf.Engines {
	t.Helper()

	registry, runner := testEngineParts(t, need...)

	engines, err := pdf.NewEngines(registry, runner)
	if err != nil {
		t.Fatalf("build the engine set: %v", err)
	}

	return engines
}

// testEngineParts returns the registry and the runner separately, so a test
// can wrap one of them. Counting the subprocess runner's calls is how the
// "one password attempt per request" assertion is proved rather than claimed.
func testEngineParts(t *testing.T, need ...string) (pdf.Registry, pdf.Subprocess) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	registry, err := engine.Resolve(ctx, envLookup{}, testBinaries)
	if err != nil {
		t.Fatalf("resolve the engines: %v", err)
	}

	for _, name := range need {
		if !registry.Has(name) {
			t.Skipf("%s is not installed. This assertion runs inside the production image: "+
				"see docs/deployment.md, \"Verifying the image\".", name)
		}
	}

	runner, err := engine.NewRunner(engine.RunnerOptions{
		Limits: engine.Limits{
			Timeout:      5 * time.Minute,
			AddressSpace: 3 << 30,
			CPU:          4 * time.Minute,
			FileSize:     2 << 30,
			Processes:    64,
			OpenFiles:    512,
		},
		// Not production: a development machine has no prlimit, and a test
		// that refused to run without the sandbox could never run on a laptop.
		// The limits themselves are proven by internal/engine's own tests,
		// which run inside the image.
		Log: slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatalf("build the engine runner: %v", err)
	}

	return registry, runner
}

func TestAnEngineSetNeedsBothItsParts(t *testing.T) {
	t.Parallel()

	if _, err := pdf.NewEngines(nil, nil); err == nil {
		t.Error("NewEngines with no registry should fail: an operation with no runner has no limits")
	}
}
