package pdf_test

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/AlexGalhardo/vukem-tools/internal/pdf"
)

func TestAWatermarkReachesEveryPageAndKeepsTheirContent(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF, pdf.EnginePDFToText)
	dir := t.TempDir()
	ctx := context.Background()

	source := writeNamedFixture(t, dir, "source.pdf", []string{"Alpha page", "Beta page", "Gamma page"})
	out := filepath.Join(dir, "watermarked.pdf")

	stamped, err := engines.Watermark(ctx, pdf.WatermarkRequest{
		Dir: dir, Input: source, Output: out, Text: "DRAFT",
	})
	if err != nil {
		t.Fatalf("Watermark = %v", err)
	}

	if stamped != 3 {
		t.Errorf("Watermark stamped %d pages, want 3", stamped)
	}

	pages := bytes.Split(extractText(t, engines, dir, out), []byte{'\f'})
	if len(pages) < 3 {
		t.Fatalf("the watermarked document has %d pages of text", len(pages))
	}

	for i, original := range []string{"Alpha page", "Beta page", "Gamma page"} {
		if !bytes.Contains(pages[i], []byte(original)) {
			t.Errorf("page %d lost its own text %q", i+1, original)
		}

		if !bytes.Contains(pages[i], []byte("DRAFT")) {
			t.Errorf("page %d has no watermark", i+1)
		}
	}
}

// A diagonal watermark is the one most people want, and it is drawn with a
// rotated text matrix. It is tested on its own because of how the text comes
// back out: `pdftotext -layout` lays a rotated run out by each glyph's
// position on the page, so "DRAFT" at 45 degrees extracts as its letters on
// separate lines. The letters are all there, in order, which is what can
// honestly be asserted — looking for the word would fail on a watermark that
// is perfectly correct.
func TestADiagonalWatermarkIsDrawnRotated(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF, pdf.EnginePDFToText)
	dir := t.TempDir()

	source := writeNamedFixture(t, dir, "source.pdf", []string{"Alpha page"})
	flat := filepath.Join(dir, "flat.pdf")
	diagonal := filepath.Join(dir, "diagonal.pdf")
	ctx := context.Background()

	for _, c := range []struct {
		out    string
		rotate float64
	}{{flat, 0}, {diagonal, 45}} {
		if _, err := engines.Watermark(ctx, pdf.WatermarkRequest{
			Dir: dir, Input: source, Output: c.out, Text: "DRAFT", Rotate: c.rotate,
		}); err != nil {
			t.Fatalf("Watermark at %g degrees = %v", c.rotate, err)
		}
	}

	upright := extractText(t, engines, dir, flat)
	turned := extractText(t, engines, dir, diagonal)

	if !bytes.Contains(upright, []byte("DRAFT")) {
		t.Errorf("the upright watermark is missing: %q", upright)
	}

	// Rotated: every letter is on the page, but neither adjacent nor in
	// order. A 45-degree run climbs the page, and the extractor reports
	// glyphs top to bottom, so "DRAFT" comes back as its letters reversed
	// across several lines. What can honestly be asserted is that they are
	// all there.
	for _, letter := range "DRAFT" {
		if !bytes.ContainsRune(turned, letter) {
			t.Errorf("the diagonal watermark is missing %q: %q", letter, turned)
		}
	}

	// And it really is rotated, not merely present: the glyphs come back on
	// separate lines, which an upright run does not do.
	if bytes.Contains(turned, []byte("DRAFT")) {
		t.Errorf("the diagonal watermark extracted as one run (%q), so it was not rotated", turned)
	}
}

// A watermark on a page range stamps those pages and leaves the others alone,
// which needs the stamp document to keep a blank page where a page is
// skipped — otherwise the overlay slides out of alignment.
func TestAWatermarkOnAPageRangeLeavesTheOtherPagesAlone(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF, pdf.EnginePDFToText)
	dir := t.TempDir()

	source := writeNamedFixture(t, dir, "source.pdf", []string{"One", "Two", "Three", "Four"})
	out := filepath.Join(dir, "partial.pdf")

	stamped, err := engines.Watermark(context.Background(), pdf.WatermarkRequest{
		Dir: dir, Input: source, Output: out, Text: "COPY", Pages: []int{2, 3},
	})
	if err != nil {
		t.Fatalf("Watermark = %v", err)
	}

	if stamped != 2 {
		t.Errorf("Watermark stamped %d pages, want 2", stamped)
	}

	pages := bytes.Split(extractText(t, engines, dir, out), []byte{'\f'})
	if len(pages) < 4 {
		t.Fatalf("the document has %d pages of text, want 4", len(pages))
	}

	for _, page := range []int{2, 3} {
		if !bytes.Contains(pages[page-1], []byte("COPY")) {
			t.Errorf("page %d should be stamped: %q", page, pages[page-1])
		}
	}

	for _, page := range []int{1, 4} {
		if bytes.Contains(pages[page-1], []byte("COPY")) {
			t.Errorf("page %d should not be stamped: %q", page, pages[page-1])
		}
	}
}

func TestWatermarkRefusesTextItCannotDraw(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF)
	dir := t.TempDir()
	source := writeFixture(t, dir, "source.pdf", 1)

	cases := []struct {
		name string
		req  pdf.WatermarkRequest
	}{
		{"empty text", pdf.WatermarkRequest{Text: "   "}},
		{"an accented character", pdf.WatermarkRequest{Text: "RASCUNHO ção"}},
		{"an emoji", pdf.WatermarkRequest{Text: "DRAFT 🙂"}},
		{"a size below the bound", pdf.WatermarkRequest{Text: "DRAFT", Size: 1}},
		{"a size above the bound", pdf.WatermarkRequest{Text: "DRAFT", Size: pdf.MaxStampSize + 1}},
		{"an impossible opacity", pdf.WatermarkRequest{Text: "DRAFT", Opacity: 2}},
		{"a position that does not exist", pdf.WatermarkRequest{Text: "DRAFT", Position: "middle"}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			req := c.req
			req.Dir = dir
			req.Input = source
			req.Output = filepath.Join(dir, "out.pdf")

			_, err := engines.Watermark(context.Background(), req)

			option, ok := errors.AsType[*pdf.OptionError](err)
			if !ok {
				t.Fatalf("Watermark = %v, want *pdf.OptionError", err)
			}

			if option.Message() == "" {
				t.Error("the refusal needs a sentence to show the user")
			}
		})
	}
}

// ROADMAP S6.6, second assert, for the page-number tool: the number has to be
// text a reader can find, at the position it was put.
func TestPageNumbersAreTextOnThePageTheyBelongTo(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF, pdf.EnginePDFToText)
	dir := t.TempDir()

	source := writeFixture(t, dir, "source.pdf", 4)
	out := filepath.Join(dir, "numbered.pdf")

	stamped, err := engines.NumberPages(context.Background(), pdf.PageNumberRequest{
		Dir: dir, Input: source, Output: out, Format: "Page {n} of {total}", Position: pdf.BottomRight,
	})
	if err != nil {
		t.Fatalf("NumberPages = %v", err)
	}

	if stamped != 4 {
		t.Errorf("NumberPages stamped %d pages, want 4", stamped)
	}

	pages := bytes.Split(extractText(t, engines, dir, out), []byte{'\f'})

	for page := 1; page <= 4; page++ {
		want := "Page " + strconv.Itoa(page) + " of 4"
		if !bytes.Contains(pages[page-1], []byte(want)) {
			t.Errorf("page %d does not carry %q: %q", page, want, pages[page-1])
		}
	}
}

// Numbering can start at a number other than one, which is what a document
// with a cover page needs.
func TestPageNumbersCanStartAtAnyNumber(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF, pdf.EnginePDFToText)
	dir := t.TempDir()

	source := writeFixture(t, dir, "source.pdf", 3)
	out := filepath.Join(dir, "numbered.pdf")

	// The cover is page 1 and is left unnumbered; the rest start at 1.
	if _, err := engines.NumberPages(context.Background(), pdf.PageNumberRequest{
		Dir: dir, Input: source, Output: out, Format: "- {n} -", Pages: []int{2, 3}, StartAt: 1,
	}); err != nil {
		t.Fatalf("NumberPages = %v", err)
	}

	pages := bytes.Split(extractText(t, engines, dir, out), []byte{'\f'})

	if bytes.Contains(squeeze(pages[0]), []byte("-1-")) {
		t.Error("the cover page was numbered")
	}

	// Compared with the spaces removed. The label drawn is "- 1 -", and
	// pdftotext -layout reports it as "-1-": it reconstructs spacing from
	// glyph positions, and the gaps around a single digit are narrower than
	// the space it would need to emit one. Normalising is the honest
	// accommodation — the alternative would be asserting a spelling the
	// extractor does not promise.
	for i, want := range []string{"-1-", "-2-"} {
		if !bytes.Contains(squeeze(pages[i+1]), []byte(want)) {
			t.Errorf("page %d does not carry %q: %q", i+2, want, pages[i+1])
		}
	}
}

// ROADMAP S6.6, third assert: "A page range that names a page the document
// does not have is a validation error, not a silently skipped page".
func TestStampingRefusesAPageTheDocumentDoesNotHave(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF)
	dir := t.TempDir()
	source := writeFixture(t, dir, "source.pdf", 2)

	_, err := engines.Watermark(context.Background(), pdf.WatermarkRequest{
		Dir: dir, Input: source, Output: filepath.Join(dir, "o.pdf"), Text: "DRAFT", Pages: []int{1, 7},
	})

	rangeErr, ok := errors.AsType[*pdf.PageRangeError](err)
	if !ok {
		t.Fatalf("Watermark of page 7 of a 2-page document = %v, want *pdf.PageRangeError", err)
	}

	_, err = engines.NumberPages(context.Background(), pdf.PageNumberRequest{
		Dir: dir, Input: source, Output: filepath.Join(dir, "n.pdf"), Pages: []int{9},
	})

	if !errors.As(err, &rangeErr) {
		t.Fatalf("NumberPages of page 9 of a 2-page document = %v, want *pdf.PageRangeError", err)
	}
}

// squeeze removes the whitespace a text extractor reconstructs from glyph
// positions, so an assertion can be about the characters that were drawn
// rather than about the spacing between them.
func squeeze(body []byte) []byte {
	out := make([]byte, 0, len(body))

	for _, c := range body {
		if c != ' ' && c != '	' {
			out = append(out, c)
		}
	}

	return out
}
