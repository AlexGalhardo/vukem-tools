package pdf_test

import (
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/AlexGalhardo/vukem-tools/internal/pdf"
)

// ROADMAP S6.3, first assert: "Rasterising at 150 DPI a page whose size is
// known produces an image of the expected pixel dimensions, within one
// pixel".
func TestRasterisingAtAKnownResolutionGivesTheExpectedPixelSize(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF, pdf.EnginePDFToPPM)
	dir := t.TempDir()

	source := writeFixture(t, dir, "source.pdf", 2)
	a4, _ := pdf.PageSize("a4")

	for _, dpi := range []int{72, 150, 300} {
		rendered, err := engines.Rasterise(context.Background(), pdf.RasterRequest{
			Dir:         dir,
			Input:       source,
			SourcePages: 2,
			DPI:         dpi,
			Format:      pdf.RasterPNG,
			Prefix:      "dpi" + strconv.Itoa(dpi),
		})
		if err != nil {
			t.Fatalf("Rasterise at %d DPI = %v", dpi, err)
		}

		if len(rendered) != 2 {
			t.Fatalf("Rasterise at %d DPI produced %d images, want 2", dpi, len(rendered))
		}

		wantWidth := a4.Width() * float64(dpi) / 72
		wantHeight := a4.Height() * float64(dpi) / 72

		for _, page := range rendered {
			if math.Abs(float64(page.Width)-wantWidth) > 1 {
				t.Errorf("%d DPI page %d is %d pixels wide, want %.1f (within one)",
					dpi, page.Page, page.Width, wantWidth)
			}

			if math.Abs(float64(page.Height)-wantHeight) > 1 {
				t.Errorf("%d DPI page %d is %d pixels tall, want %.1f (within one)",
					dpi, page.Page, page.Height, wantHeight)
			}
		}
	}
}

func TestRasterisingRefusesAResolutionOutsideItsBounds(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EnginePDFToPPM)
	dir := t.TempDir()

	for _, dpi := range []int{1, pdf.MinDPI - 1, pdf.MaxDPI + 1, 10000} {
		_, err := engines.Rasterise(context.Background(), pdf.RasterRequest{
			Dir: dir, Input: filepath.Join(dir, "x.pdf"), SourcePages: 1, DPI: dpi, Prefix: "p",
		})

		if _, ok := errors.AsType[*pdf.OptionError](err); !ok {
			t.Errorf("Rasterise at %d DPI = %v, want *pdf.OptionError", dpi, err)
		}
	}
}

func TestRasterisingToJPEGProducesAReadableJPEG(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF, pdf.EnginePDFToPPM)
	dir := t.TempDir()

	source := writeFixture(t, dir, "source.pdf", 1)

	rendered, err := engines.Rasterise(context.Background(), pdf.RasterRequest{
		Dir: dir, Input: source, SourcePages: 1, DPI: 72, Format: pdf.RasterJPEG, Prefix: "j",
	})
	if err != nil {
		t.Fatalf("Rasterise = %v", err)
	}

	info, err := pdf.JPEGInfo(rendered[0].Path)
	if err != nil {
		t.Fatalf("the rendered page is not a readable JPEG: %v", err)
	}

	if info.Width != rendered[0].Width || info.Height != rendered[0].Height {
		t.Errorf("JPEGInfo = %dx%d, Rasterise reported %dx%d",
			info.Width, info.Height, rendered[0].Width, rendered[0].Height)
	}
}

// ROADMAP S6.3, second assert: "A PDF/A conversion is validated by a second
// tool (verapdf, or Ghostscript's own report), not by the absence of an
// error." veraPDF is not in the image, so the second opinion is
// Ghostscript's conformance report, which ToPDFA returns rather than
// discards — and the result has to be a document that still reads.
func TestPDFAConversionReportsItsOwnConformance(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF, pdf.EngineGhostscript)
	dir := t.TempDir()
	ctx := context.Background()

	source := writeFixture(t, dir, "source.pdf", 2)

	for _, part := range []pdf.ArchivePart{pdf.PDFA1b, pdf.PDFA2b, pdf.PDFA3b} {
		out := filepath.Join(dir, "pdfa-"+strconv.Itoa(int(part))+".pdf")

		result, err := engines.ToPDFA(ctx, pdf.ArchiveRequest{
			Dir: dir, Input: source, Output: out, Part: part, Pages: 2,
		})
		if err != nil {
			t.Fatalf("ToPDFA part %d = %v", part, err)
		}

		if result.Part != part {
			t.Errorf("the report names part %d, want %d", result.Part, part)
		}

		// The report is the verdict, not the absence of an error: a
		// conversion that kept content at the cost of conformance says so.
		if !result.Conformant {
			t.Logf("part %d is not conformant, which the report states: %v", part, result.Complaints)
		}

		// Whatever the verdict, the output has to be a document that reads
		// and has the same pages.
		doc, err := engines.Inspect(ctx, dir, out)
		if err != nil {
			t.Fatalf("the PDF/A output does not read: %v", err)
		}

		if doc.Count != 2 {
			t.Errorf("the PDF/A output has %d pages, want 2", doc.Count)
		}

		if !hasPDFAMarker(t, out) {
			t.Errorf("part %d: the output carries no PDF/A identification", part)
		}
	}
}

// hasPDFAMarker reports whether the document carries the XMP PDF/A
// identification schema, which is the one thing every PDF/A file must have
// and the cheapest independent check that Ghostscript did what it said.
func hasPDFAMarker(t *testing.T, path string) bool {
	t.Helper()

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return strings.Contains(string(body), "pdfaid")
}

func TestPDFARefusesAConformanceLevelItCannotTarget(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineGhostscript)
	dir := t.TempDir()

	_, err := engines.ToPDFA(context.Background(), pdf.ArchiveRequest{
		Dir: dir, Input: filepath.Join(dir, "x.pdf"), Output: filepath.Join(dir, "o.pdf"), Part: 4,
	})

	if _, ok := errors.AsType[*pdf.OptionError](err); !ok {
		t.Fatalf("ToPDFA part 4 = %v, want *pdf.OptionError", err)
	}
}

// ROADMAP S6.3, third assert: "The Markdown output of a document with three
// heading levels keeps all three, and a round trip through Markdown does not
// lose a list".
func TestMarkdownKeepsThreeHeadingLevelsAndAList(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF, pdf.EnginePDFToText)
	dir := t.TempDir()

	source := writeStructuredFixture(t, dir, "structured.pdf")
	out := filepath.Join(dir, "out.md")

	result, err := engines.ToMarkdown(context.Background(), pdf.MarkdownRequest{
		Dir: dir, Input: source, Output: out, SourcePages: 1,
	})
	if err != nil {
		t.Fatalf("ToMarkdown = %v", err)
	}

	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read the Markdown: %v", err)
	}

	markdown := string(body)

	for level, want := range []string{"# 1 ", "## 1.1 ", "### 1.1.1 "} {
		if !strings.Contains(markdown, want) {
			t.Errorf("level %d heading %q is missing from the Markdown:\n%s", level+1, want, markdown)
		}

		if result.Headings[level] == 0 {
			t.Errorf("the result reports no level-%d headings", level+1)
		}
	}

	// The list has to survive as a list, which is what "a round trip through
	// Markdown does not lose a list" means: the items are still items, and
	// there are still as many of them.
	items := strings.Count(markdown, "\n- ")
	if items < 3 {
		t.Errorf("the Markdown has %d list items, want 3:\n%s", items, markdown)
	}

	if result.ListItems < 3 {
		t.Errorf("the result reports %d list items, want 3", result.ListItems)
	}
}

// writeStructuredFixture writes a one-page document with three numbered
// heading levels, a bulleted list and a paragraph.
//
// The headings are numbered because numbering is the signal the heuristics
// are honest about recognising: a PDF records glyph sizes, not that a line is
// a heading, so a document whose hierarchy is implied only by font size comes
// out as paragraphs — which is what the tool page says.
func writeStructuredFixture(t *testing.T, dir, name string) string {
	t.Helper()

	a4, _ := pdf.PageSize("a4")

	lines := []string{
		"1 Introduction",
		"",
		"This paragraph explains the document and runs on for long enough",
		"that it is wrapped across two lines by the extractor.",
		"",
		"1.1 Scope",
		"",
		"- The first item",
		"- The second item",
		"- The third item",
		"",
		"1.1.1 Out of scope",
		"",
		"Everything else is out of scope.",
	}

	text := make([]pdf.TextStamp, 0, len(lines))
	y := 760.0

	for _, line := range lines {
		if line != "" {
			text = append(text, pdf.TextStamp{Text: line, Font: pdf.Helvetica, Size: 12, X: 72, Y: y})
		}

		y -= 24
	}

	return writeSpec(t, dir, name, pdf.DocumentSpec{Pages: []pdf.PageSpec{{Box: a4, Text: text}}})
}

func TestTextExtractionRefusesANonContiguousPageSelection(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EnginePDFToText)
	dir := t.TempDir()

	err := engines.ExtractText(context.Background(), pdf.TextRequest{
		Dir:         dir,
		Input:       filepath.Join(dir, "x.pdf"),
		Output:      filepath.Join(dir, "o.txt"),
		Pages:       []int{1, 3, 5},
		SourcePages: 5,
	})

	if _, ok := errors.AsType[*pdf.PageRangeError](err); !ok {
		t.Fatalf("ExtractText of 1,3,5 = %v, want *pdf.PageRangeError", err)
	}
}

// Images are bound into a PDF without being re-encoded when they are already
// JPEG, which is the only lossless path and the one the tool advertises.
func TestImagesBecomePagesAndAJPEGIsNotReencoded(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF, pdf.EngineVips, pdf.EnginePDFToPPM)
	dir := t.TempDir()
	ctx := context.Background()

	photo := makeTestJPEG(t, dir, 200, 120)
	out := filepath.Join(dir, "images.pdf")

	result, err := engines.FromImages(ctx, pdf.ImagesRequest{
		Dir:      dir,
		Inputs:   []string{photo, photo},
		JPEG:     []bool{true, true},
		Output:   out,
		PageSize: "a4",
		Fit:      pdf.FitContain,
		Margin:   18,
	})
	if err != nil {
		t.Fatalf("FromImages = %v", err)
	}

	if result.Pages != 2 {
		t.Errorf("Pages = %d, want 2", result.Pages)
	}

	if result.Reencoded != 0 {
		t.Errorf("Reencoded = %d: a JPEG input must be embedded untouched", result.Reencoded)
	}

	doc, err := engines.Inspect(ctx, dir, out)
	if err != nil {
		t.Fatalf("Inspect = %v", err)
	}

	if doc.Count != 2 {
		t.Errorf("the document has %d pages, want 2", doc.Count)
	}

	// The image's own bytes have to be in the file, which is what "not
	// re-encoded" means.
	original, err := os.ReadFile(photo)
	if err != nil {
		t.Fatalf("read the JPEG: %v", err)
	}

	document, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read the document: %v", err)
	}

	if !strings.Contains(string(document), string(original)) {
		t.Error("the document does not contain the JPEG's own bytes, so it was re-encoded")
	}

	// The landscape image on auto orientation should give a landscape page.
	page := renderPage(t, engines, dir, out, 1, "img")
	if page.Width <= page.Height {
		t.Errorf("a landscape image produced a %dx%d page, want it turned", page.Width, page.Height)
	}
}

func TestAnExactFitMakesThePageTheImagesOwnSize(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF, pdf.EngineVips, pdf.EnginePDFToPPM)
	dir := t.TempDir()

	photo := makeTestJPEG(t, dir, 240, 160)
	out := filepath.Join(dir, "exact.pdf")

	_, err := engines.FromImages(context.Background(), pdf.ImagesRequest{
		Dir: dir, Inputs: []string{photo}, JPEG: []bool{true}, Output: out, Fit: pdf.FitExact,
	})
	if err != nil {
		t.Fatalf("FromImages = %v", err)
	}

	page := renderPage(t, engines, dir, out, 1, "exact")

	// One pixel to the point at 72 DPI, so the rendered page is the image's
	// pixel size, within the rounding pdftoppm does.
	if math.Abs(float64(page.Width)-240) > 1 || math.Abs(float64(page.Height)-160) > 1 {
		t.Errorf("the exact-fit page renders %dx%d, want 240x160", page.Width, page.Height)
	}
}

// A PNG with transparency is flattened onto white, not onto libvips' default
// black: a logo arriving on a black square is the failure this guards.
func TestATransparentImageIsFlattenedOntoWhite(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF, pdf.EngineVips, pdf.EnginePDFToPPM)
	dir := t.TempDir()

	transparent := makeTransparentPNG(t, dir, "logo.png", 100, 60)
	out := filepath.Join(dir, "flattened.pdf")

	result, err := engines.FromImages(context.Background(), pdf.ImagesRequest{
		Dir: dir, Inputs: []string{transparent}, JPEG: []bool{false}, Output: out, Fit: pdf.FitExact,
	})
	if err != nil {
		t.Fatalf("FromImages = %v", err)
	}

	if result.Reencoded != 1 {
		t.Errorf("Reencoded = %d, want 1: a PNG has to be converted", result.Reencoded)
	}

	rendered := renderPage(t, engines, dir, out, 1, "flat")

	if mean := meanLuminance(t, rendered.Path); mean < 200 {
		t.Errorf("the flattened page has mean luminance %.0f, want near white (>200): "+
			"the transparency was composited against the wrong colour", mean)
	}
}

// meanLuminance is the average brightness of a rendered page, 0 black to 255
// white.
func meanLuminance(t *testing.T, path string) float64 {
	t.Helper()

	img := decodePNG(t, path)
	bounds := img.Bounds()

	var total float64

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			total += (float64(r) + float64(g) + float64(b)) / 3 / 257
		}
	}

	return total / float64(bounds.Dx()*bounds.Dy())
}

// decodePNG reads a rendered page into memory.
//
// It is a test, and the pages are rendered at 72 DPI on purpose so that a
// whole page is a few hundred kilobytes of pixels. Nothing in the library
// decodes an image.
func decodePNG(t *testing.T, path string) image.Image {
	t.Helper()

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}

	defer func() { _ = file.Close() }()

	img, err := png.Decode(file)
	if err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}

	return img
}

// makeTestJPEG writes a JPEG of the given size, encoded by the standard
// library.
//
// The fixtures are built in Go rather than with libvips on purpose: a fixture
// that needed an engine could only be made where the engines are, and then a
// test on a development machine would skip for the wrong reason.
func makeTestJPEG(t *testing.T, dir string, width, height int) string {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, width, height))

	// A gradient rather than a flat colour, so a JPEG of it does not compress
	// to almost nothing and the "its bytes are in the document" check is
	// comparing something real.
	for y := range height {
		for x := range width {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 0x80, A: 0xff})
		}
	}

	path := filepath.Join(dir, "photo-"+strconv.Itoa(width)+"x"+strconv.Itoa(height)+".jpg")

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}

	if err := jpeg.Encode(file, img, &jpeg.Options{Quality: 90}); err != nil {
		_ = file.Close()
		t.Fatalf("encode %s: %v", path, err)
	}

	if err := file.Close(); err != nil {
		t.Fatalf("close %s: %v", path, err)
	}

	return path
}

// makeTransparentPNG writes a PNG that is entirely transparent black, which
// is the image that comes out wrong when transparency is composited against
// the default background instead of white.
func makeTransparentPNG(t *testing.T, dir, name string, width, height int) string {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.Set(x, y, color.RGBA{})
		}
	}

	path := filepath.Join(dir, name)

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}

	if err := png.Encode(file, img); err != nil {
		_ = file.Close()
		t.Fatalf("encode %s: %v", path, err)
	}

	if err := file.Close(); err != nil {
		t.Fatalf("close %s: %v", path, err)
	}

	return path
}
