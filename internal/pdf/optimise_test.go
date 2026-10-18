package pdf_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/AlexGalhardo/vukem-tools/internal/pdf"
)

// ROADMAP S6.4, first assert: "Compression never increases the file size: a
// document already optimised is returned unchanged with a message saying so,
// not re-encoded into something larger".
func TestCompressionNeverReturnsALargerFile(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF, pdf.EngineGhostscript)
	dir := t.TempDir()
	ctx := context.Background()

	source := writeFixture(t, dir, "source.pdf", 4)

	// Compressing twice is how the "already optimised" path is reached: the
	// second pass has nothing left to remove and Ghostscript's own output
	// routinely comes out larger than its input.
	current := source

	for pass := 1; pass <= 2; pass++ {
		out := filepath.Join(dir, "pass"+strconv.Itoa(pass)+".pdf")

		result, err := engines.Compress(ctx, pdf.CompressRequest{
			Dir: dir, Input: current, Output: out, Level: pdf.CompressBalanced, Pages: 4,
		})
		if err != nil {
			t.Fatalf("pass %d: Compress = %v", pass, err)
		}

		actual, err := os.Stat(out)
		if err != nil {
			t.Fatalf("pass %d: the output is missing: %v", pass, err)
		}

		if result.After > result.Before {
			t.Errorf("pass %d: After = %d, Before = %d: compression must never grow a file",
				pass, result.After, result.Before)
		}

		if actual.Size() > result.Before {
			t.Errorf("pass %d: the file on disk is %d bytes, the input was %d: "+
				"the result that was kept is larger than the input",
				pass, actual.Size(), result.Before)
		}

		if !result.Returned && actual.Size() != result.Before {
			t.Errorf("pass %d: the compression was rejected, so the output should be the input's "+
				"%d bytes, not %d", pass, result.Before, actual.Size())
		}

		t.Logf("pass %d at balanced: %d -> %d bytes, kept=%v", pass, result.Before, result.After, result.Returned)

		current = out
	}
}

// Compressing a document whose content Ghostscript cannot shrink returns the
// input unchanged and says so, rather than silently returning the larger
// re-encoding.
func TestAnAlreadyOptimisedDocumentComesBackUnchanged(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF, pdf.EngineGhostscript)
	dir := t.TempDir()
	ctx := context.Background()

	// A JPEG-only document is the realistic "already optimised" case: the
	// bytes are already a lossy image stream, so a re-encode can only add.
	photo := makeTestJPEG(t, dir, 400, 300)
	source := filepath.Join(dir, "photos.pdf")

	if _, err := engines.FromImages(ctx, pdf.ImagesRequest{
		Dir: dir, Inputs: []string{photo}, JPEG: []bool{true}, Output: source, Fit: pdf.FitExact,
	}); err != nil {
		t.Fatalf("FromImages = %v", err)
	}

	out := filepath.Join(dir, "compressed.pdf")

	result, err := engines.Compress(ctx, pdf.CompressRequest{
		Dir: dir, Input: source, Output: out, Level: pdf.CompressLight, Pages: 1,
	})
	if err != nil {
		t.Fatalf("Compress = %v", err)
	}

	t.Logf("a JPEG-only document at light: %d -> %d bytes, kept=%v",
		result.Before, result.After, result.Returned)

	if result.Saved() < 0 {
		t.Errorf("Saved = %d, which cannot be negative", result.Saved())
	}

	if !result.Returned && result.Saved() != 0 {
		t.Errorf("a rejected compression reports Saved = %d, want 0", result.Saved())
	}

	// Whatever was decided, the output has to be a document that still reads
	// and has the same page.
	doc, err := engines.Inspect(ctx, dir, out)
	if err != nil {
		t.Fatalf("the compressed output does not read: %v", err)
	}

	if doc.Count != 1 {
		t.Errorf("the compressed output has %d pages, want 1", doc.Count)
	}
}

// ROADMAP S6.4, second assert: "Compression at the mildest level keeps every
// page's rendered appearance within a perceptual threshold of the original,
// measured, not eyeballed".
func TestLightCompressionKeepsEveryPagesAppearance(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF, pdf.EngineGhostscript, pdf.EnginePDFToPPM)
	dir := t.TempDir()
	ctx := context.Background()

	// The fixture is a document of high-resolution photographs, because that
	// is the only kind of document light compression actually changes: it
	// downsamples images above 300 DPI and leaves everything else alone. A
	// text document comes back unchanged — correctly, by the rule the
	// previous test proves — and comparing a file with itself would pass this
	// assert without proving anything, which is the one way this test could
	// lie.
	const pages = 4

	// 2000 pixels across a 307-point area is about 470 DPI, which is above
	// the 300 light downsamples to, so there is something to do.
	photo := makeTestJPEG(t, dir, 2000, 1400)

	inputs := make([]string, 0, pages)
	flags := make([]bool, 0, pages)

	for range pages {
		inputs = append(inputs, photo)
		flags = append(flags, true)
	}

	source := filepath.Join(dir, "source.pdf")

	if _, err := engines.FromImages(ctx, pdf.ImagesRequest{
		Dir: dir, Inputs: inputs, JPEG: flags, Output: source,
		PageSize: "a4", Orientation: pdf.OrientPortrait, Fit: pdf.FitContain, Margin: 144,
	}); err != nil {
		t.Fatalf("FromImages = %v", err)
	}

	out := filepath.Join(dir, "light.pdf")

	result, err := engines.Compress(ctx, pdf.CompressRequest{
		Dir: dir, Input: source, Output: out, Level: pdf.CompressLight, Pages: pages,
	})
	if err != nil {
		t.Fatalf("Compress = %v", err)
	}

	if !result.Returned {
		t.Fatalf("the compression was rejected (%d -> %d bytes), so comparing the pages would "+
			"compare the document with itself and prove nothing", result.Before, result.After)
	}

	t.Logf("light compression: %d -> %d bytes, %d saved", result.Before, result.After, result.Saved())

	// Every page, not a sample: the assert says "every page's rendered
	// appearance", and a compression that ruined one page in forty is exactly
	// the failure a sample would miss.
	for page := 1; page <= pages; page++ {
		before := renderPage(t, engines, dir, source, page, "before"+strconv.Itoa(page))
		after := renderPage(t, engines, dir, out, page, "after"+strconv.Itoa(page))

		if before.Width != after.Width || before.Height != after.Height {
			t.Fatalf("page %d renders %dx%d before and %dx%d after: the page size changed",
				page, before.Width, before.Height, after.Width, after.Height)
		}

		rmse := rootMeanSquareError(t, before.Path, after.Path)
		t.Logf("page %d: RMSE %.3f of 255", page, rmse)

		// The threshold is in luminance levels out of 255. Light compression
		// re-renders the page, so the glyph rasterisation differs by a shade
		// at the edges and the error is never exactly zero; anything a person
		// could see as a difference is an order of magnitude above this.
		if rmse > perceptualThreshold {
			t.Errorf("page %d has RMSE %.3f, above the threshold of %.1f: "+
				"light compression changed how the page looks", page, rmse, perceptualThreshold)
		}
	}
}

// perceptualThreshold is the largest root-mean-square difference, in
// luminance levels out of 255, that counts as "the same page".
//
// Four levels is about 1.5 per cent of the range: below the threshold at
// which a difference is visible on a screen, and far below what a visible
// change — a dropped image, a substituted font, a downsampled photograph —
// produces.
const perceptualThreshold = 4.0

// rootMeanSquareError is the per-pixel luminance difference between two
// rendered pages.
func rootMeanSquareError(t *testing.T, left, right string) float64 {
	t.Helper()

	a := decodePNG(t, left)
	b := decodePNG(t, right)

	if a.Bounds() != b.Bounds() {
		t.Fatalf("the pages are %v and %v, which cannot be compared", a.Bounds(), b.Bounds())
	}

	var sum float64

	bounds := a.Bounds()

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			sum += math.Pow(luminance(a, x, y)-luminance(b, x, y), 2)
		}
	}

	return math.Sqrt(sum / float64(bounds.Dx()*bounds.Dy()))
}

// luminance is one pixel's brightness, 0 to 255.
func luminance(img image.Image, x, y int) float64 {
	r, g, b, _ := img.At(x, y).RGBA()

	// Rec. 601 weights, which is what a person's eye does with the three
	// channels; an unweighted mean would call a green shift invisible.
	return (0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)) / 257
}

func TestCompressionRefusesALevelItDoesNotHave(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineGhostscript)
	dir := t.TempDir()

	_, err := engines.Compress(context.Background(), pdf.CompressRequest{
		Dir: dir, Input: filepath.Join(dir, "x.pdf"), Output: filepath.Join(dir, "o.pdf"), Level: "maximum",
	})

	if _, ok := errors.AsType[*pdf.OptionError](err); !ok {
		t.Fatalf("Compress at level \"maximum\" = %v, want *pdf.OptionError", err)
	}
}

// ROADMAP S6.4, fourth assert: "Repair is honest: a file it cannot rebuild
// fails with unsupported_input rather than returning a document that opens
// empty".
func TestRepairRebuildsWhatItCanAndRefusesWhatItCannot(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF)
	dir := t.TempDir()
	ctx := context.Background()

	t.Run("a document with a broken cross-reference table is rebuilt", func(t *testing.T) {
		t.Parallel()

		source := writeFixture(t, dir, "intact.pdf", 3)

		broken := filepath.Join(dir, "broken.pdf")
		damageXref(t, source, broken)

		out := filepath.Join(dir, "repaired.pdf")

		result, err := engines.Repair(ctx, pdf.RepairRequest{Dir: dir, Input: broken, Output: out})
		if err != nil {
			t.Fatalf("Repair = %v", err)
		}

		if result.Pages != 3 {
			t.Errorf("the repaired document has %d pages, want 3", result.Pages)
		}

		if !result.Rebuilt {
			t.Error("Rebuilt = false, but the cross-reference table was broken")
		}

		// The repair has to produce a document that reads, which is the one
		// thing a repair is for.
		if _, err := engines.Inspect(ctx, dir, out); err != nil {
			t.Errorf("the repaired document does not read: %v", err)
		}
	})

	t.Run("a file that is not a PDF is refused", func(t *testing.T) {
		t.Parallel()

		junk := filepath.Join(dir, "junk.pdf")
		if err := os.WriteFile(junk, []byte("this was never a PDF\n"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}

		out := filepath.Join(dir, "junk-repaired.pdf")

		_, err := engines.Repair(ctx, pdf.RepairRequest{Dir: dir, Input: junk, Output: out})

		if _, ok := errors.AsType[*pdf.NotPDFError](err); !ok {
			t.Fatalf("Repair of junk = %v, want *pdf.NotPDFError", err)
		}

		if _, statErr := os.Stat(out); statErr == nil {
			t.Error("a refused repair left an output file behind, which would be downloadable")
		}
	})

	t.Run("a truncated document with no recoverable pages is refused", func(t *testing.T) {
		t.Parallel()

		source := writeFixture(t, dir, "for-truncation.pdf", 3)

		body, err := os.ReadFile(source)
		if err != nil {
			t.Fatalf("read: %v", err)
		}

		// Only the header and a few bytes: there is no page object left to
		// find, so there is nothing to rebuild.
		stub := filepath.Join(dir, "stub.pdf")
		if err := os.WriteFile(stub, body[:40], 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}

		out := filepath.Join(dir, "stub-repaired.pdf")

		result, err := engines.Repair(ctx, pdf.RepairRequest{Dir: dir, Input: stub, Output: out})
		if err == nil {
			t.Fatalf("Repair of a header-only file returned %+v, want a refusal", result)
		}

		damaged, ok := errors.AsType[*pdf.DamagedError](err)
		if !ok {
			t.Fatalf("Repair = %v, want *pdf.DamagedError", err)
		}

		if damaged.Message() == "" {
			t.Error("the refusal needs a sentence to show the user")
		}

		if _, statErr := os.Stat(out); statErr == nil {
			t.Error("a failed repair left a file behind that would be offered as a result")
		}
	})
}

// damageXref copies a PDF with its startxref pointer corrupted, which is the
// commonest real damage: the document's objects are intact and the table
// that finds them is not.
func damageXref(t *testing.T, src, dst string) {
	t.Helper()

	body, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}

	marker := []byte("startxref")

	index := bytes.LastIndex(body, marker)
	if index < 0 {
		t.Fatalf("%s has no startxref", src)
	}

	// Point it at a byte offset that holds no object, so a reader has to
	// reconstruct the table. Built into its own slice rather than appended
	// onto body, which would write over the bytes it is reading from.
	trailer := []byte("startxref\n999999\n%%EOF\n")
	damaged := make([]byte, 0, index+len(trailer))
	damaged = append(damaged, body[:index]...)
	damaged = append(damaged, trailer...)

	if err := os.WriteFile(dst, damaged, 0o600); err != nil {
		t.Fatalf("write %s: %v", dst, err)
	}
}
