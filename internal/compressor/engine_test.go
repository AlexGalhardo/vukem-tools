package compressor_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AlexGalhardo/vukem-tools/internal/compressor"
	"github.com/AlexGalhardo/vukem-tools/internal/engine"
)

// The tests in this file run the real Ghostscript.
//
// Everything else in this package uses a fake runner, which proves the
// command line carries -dSAFER but cannot prove that Ghostscript honours it.
// These do, with documents written to attack it. They skip where Ghostscript
// is absent — it is in the production image and on no development machine
// here — so the rest of the suite still runs. See docs/deployment.md for how
// to run them inside the image, which is where the figures that count come
// from.

// realCompressor builds a compressor on the Ghostscript found on PATH.
func realCompressor(t *testing.T) *compressor.Compressor {
	t.Helper()

	gs, err := exec.LookPath("gs")
	if err != nil {
		t.Skip("Ghostscript is not installed; run this inside the image (docs/deployment.md)")
	}

	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not installed; run this inside the image (docs/deployment.md)")
	}

	runner, err := engine.NewRunner(engine.RunnerOptions{
		Limits: engine.Limits{
			// Short on purpose: one of the documents below never terminates,
			// and the assertion is that the timeout stops it.
			Timeout:      20 * time.Second,
			AddressSpace: 1 << 30,
			CPU:          10 * time.Second,
			FileSize:     64 << 20,
			Processes:    32,
			OpenFiles:    256,
		},
	})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	c, err := compressor.New(compressor.Options{Runner: runner, FFmpeg: ffmpeg, Ghostscript: gs})
	if err != nil {
		t.Fatalf("new compressor: %v", err)
	}

	return c
}

// hostilePostScript tries to write a file outside the job's directory.
//
// Ghostscript's input language can open and write files and run commands,
// which is the whole reason -dSAFER exists: a PDF is an untrusted document
// from a stranger, and this service's job is running strangers' documents
// through Ghostscript. The marker path is absolute and outside the working
// directory, so the assertion is simply that it does not appear.
const hostilePostScript = `%!PS
(%s) (w) file dup (escaped) writestring closefile
showpage
`

// endlessPostScript never terminates, so the only thing that can stop it is
// the runner's timeout.
const endlessPostScript = `%!PS
/forever-loop { { } loop } def
forever-loop
showpage
`

func TestAHostileDocumentCannotWriteOutsideTheJobDirectory(t *testing.T) {
	t.Parallel()

	c := realCompressor(t)

	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "escaped.txt")

	input := filepath.Join(dir, "input.ps")
	if err := os.WriteFile(input, []byte(strings.ReplaceAll(hostilePostScript, "%s", outside)), 0o600); err != nil {
		t.Fatal(err)
	}

	// It is expected to fail: -dSAFER refuses the file access, which is the
	// point. What matters is what is *not* on disk afterwards.
	_, err := c.PDF(t.Context(), compressor.Request{Dir: dir, Input: input, Output: "compressed.pdf"})
	t.Logf("Ghostscript reported: %v", err)

	if _, statErr := os.Stat(outside); statErr == nil {
		t.Fatalf("the document wrote %s, so it escaped the job's directory", outside)
	}

	// Nothing outside the job's own directory, and nothing left in it that
	// is not the input: a candidate that was not smaller is deleted, and a
	// failed run leaves no candidate at all.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	for _, entry := range entries {
		if entry.Name() != "input.ps" && entry.Name() != "compressed.pdf" {
			t.Errorf("the job directory holds an unexpected %q", entry.Name())
		}
	}
}

func TestAnEndlessDocumentIsStoppedByTheTimeout(t *testing.T) {
	t.Parallel()

	c := realCompressor(t)

	dir := t.TempDir()

	input := filepath.Join(dir, "input.ps")
	if err := os.WriteFile(input, []byte(endlessPostScript), 0o600); err != nil {
		t.Fatal(err)
	}

	started := time.Now()

	_, err := c.PDF(t.Context(), compressor.Request{Dir: dir, Input: input, Output: "compressed.pdf"})
	if err == nil {
		t.Fatal("a document that never terminates was compressed successfully")
	}

	took := time.Since(started)

	// The runner's timeout is 20 seconds, so anything much past that means
	// the engine outlived it.
	if took > 40*time.Second {
		t.Errorf("Ghostscript ran for %s, past its 20s limit", took)
	}

	// Classified rather than reported raw: a timeout is not "your file is
	// broken", and the user is told the right thing.
	failure := compressor.Classify(err)
	if failure == nil {
		t.Fatal("the failure was not classified")
	}

	t.Logf("took %s, classified as %s: %q", took.Round(time.Millisecond), failure.Reason, failure.Public)

	switch failure.Reason {
	case compressor.ReasonTimeout, compressor.ReasonOutOfMemory, compressor.ReasonEngine:
		// Any of the three is honest: the CPU limit, the address-space limit
		// and the wall clock all stop it, and which one wins first depends
		// on the machine.
	default:
		t.Errorf("classified as %s, want a timeout, an out-of-memory or an engine failure", failure.Reason)
	}
}

func TestARealPDFIsCompressedAndTheFiguresAreTheRealByteSizes(t *testing.T) {
	t.Parallel()

	c := realCompressor(t)

	gs, err := exec.LookPath("gs")
	if err != nil {
		t.Skip("Ghostscript is not installed")
	}

	dir := t.TempDir()
	input := filepath.Join(dir, "input.pdf")

	// A PDF generated by Ghostscript itself rather than a fixture in the
	// repository: a few hundred pages of text, which is the shape of the
	// document people actually upload.
	source := filepath.Join(dir, "source.ps")
	if err := os.WriteFile(source, []byte(manyPagePostScript()), 0o600); err != nil {
		t.Fatal(err)
	}

	build := exec.CommandContext(t.Context(), gs,
		"-sDEVICE=pdfwrite", "-dNOPAUSE", "-dBATCH", "-dQUIET", "-dSAFER",
		// No compression on the way in, so there is something to save.
		"-dAutoFilterColorImages=false", "-dEncodeColorImages=false", "-dCompressPages=false",
		"-sOutputFile="+input, source,
	)
	build.Dir = dir

	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("generate the source PDF: %v: %s", err, out)
	}

	before, err := os.Stat(input)
	if err != nil {
		t.Fatal(err)
	}

	result, err := c.PDF(t.Context(), compressor.Request{Dir: dir, Input: input, Output: "compressed.pdf"})
	if err != nil {
		t.Fatalf("compress: %v", err)
	}

	t.Logf("%d bytes to %d bytes, smaller=%v: %s", result.OriginalBytes, result.Bytes, result.Smaller, result.Summary())

	if result.OriginalBytes != before.Size() {
		t.Errorf("reported original %d, real %d", result.OriginalBytes, before.Size())
	}

	// The ROADMAP's assert: the reported figures are the real byte sizes of
	// the file that is actually delivered.
	delivered, err := os.Stat(result.Path)
	if err != nil {
		t.Fatal(err)
	}

	if result.Bytes != delivered.Size() {
		t.Errorf("reported %d bytes, the delivered file is %d", result.Bytes, delivered.Size())
	}

	if result.Smaller {
		if result.SavedBytes() != before.Size()-delivered.Size() {
			t.Errorf("reported a saving of %d, real %d", result.SavedBytes(), before.Size()-delivered.Size())
		}

		if result.Path == input {
			t.Error("reported smaller but delivered the original")
		}
	} else {
		// Not smaller means the original, unchanged, and no artefact left.
		if result.Path != input {
			t.Errorf("reported not smaller but delivered %q", result.Path)
		}

		if _, statErr := os.Stat(filepath.Join(dir, "compressed.pdf")); statErr == nil {
			t.Error("a candidate that was not smaller was left on disk")
		}
	}
}

// manyPagePostScript is a few hundred pages of text, as PostScript.
func manyPagePostScript() string {
	var b strings.Builder

	b.WriteString("%!PS\n")

	for page := range 200 {
		b.WriteString("/Helvetica findfont 12 scalefont setfont\n")

		for line := range 40 {
			b.WriteString("72 " + itoa(720-line*16) + " moveto\n")
			b.WriteString("(Page " + itoa(page+1) + " line " + itoa(line+1) +
				" — the quick brown fox jumps over the lazy dog, repeatedly.) show\n")
		}

		b.WriteString("showpage\n")
	}

	return b.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}

	negative := n < 0
	if negative {
		n = -n
	}

	var digits []byte

	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}

	if negative {
		return "-" + string(digits)
	}

	return string(digits)
}
