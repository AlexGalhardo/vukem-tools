package pdf_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/AlexGalhardo/vukem-tools/internal/pdf"
)

// writeNamedFixture writes a PDF whose pages each carry a distinct line of
// text, so a test can tell which page ended up where.
func writeNamedFixture(t *testing.T, dir, name string, lines []string) string {
	t.Helper()

	a4, _ := pdf.PageSize("a4")

	specs := make([]pdf.PageSpec, 0, len(lines))

	for _, line := range lines {
		specs = append(specs, pdf.PageSpec{
			Box:  a4,
			Text: []pdf.TextStamp{{Text: line, Font: pdf.Helvetica, Size: 28, X: 72, Y: 700}},
		})
	}

	return writeSpec(t, dir, name, pdf.DocumentSpec{Pages: specs})
}

// writeOutlinedFixture writes a PDF with a bookmark per page and an internal
// link from page 1 to the last page.
//
// It takes the engine set so the signature matches the other fixtures, which
// keeps the tests reading the same; it does not need an engine, which is the
// point — an outline fixture made with Ghostscript could only be built where
// Ghostscript is.
func writeOutlinedFixture(t *testing.T, _ *pdf.Engines, dir, name string, pages int) string {
	t.Helper()

	a4, _ := pdf.PageSize("a4")

	specs := make([]pdf.PageSpec, 0, pages)
	outline := make([]pdf.OutlineEntry, 0, pages)

	for i := 1; i <= pages; i++ {
		page := pdf.PageSpec{
			Box: a4,
			Text: []pdf.TextStamp{{
				Text: "Chapter " + strconv.Itoa(i),
				Font: pdf.HelveticaBold,
				Size: 28,
				X:    72,
				Y:    700,
			}},
		}

		if i == 1 && pages > 1 {
			page.Links = []pdf.Link{{
				Box:        pdf.Box{LLX: 72, LLY: 600, URX: 300, URY: 630},
				TargetPage: pages,
			}}
		}

		specs = append(specs, page)
		outline = append(outline, pdf.OutlineEntry{Title: "Chapter " + strconv.Itoa(i), TargetPage: i})
	}

	return writeSpec(t, dir, name, pdf.DocumentSpec{Pages: specs, Outline: outline})
}

// writeSpec writes a document spec to a file in dir.
func writeSpec(t *testing.T, dir, name string, spec pdf.DocumentSpec) string {
	t.Helper()

	path := filepath.Join(dir, name)

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}

	if err := pdf.WriteDocument(file, spec); err != nil {
		_ = file.Close()
		t.Fatalf("write %s: %v", path, err)
	}

	if err := file.Close(); err != nil {
		t.Fatalf("close %s: %v", path, err)
	}

	return path
}

// renderDPI is the resolution the comparison tests render at.
//
// 72 DPI, one pixel to the point, which makes the expected pixel dimensions
// the page's own dimensions and keeps the images small enough to compare in
// a test.
const renderDPI = 72

// renderPage rasterises one page and returns what it produced.
func renderPage(t *testing.T, engines *pdf.Engines, dir, path string, page int, prefix string) pdf.RasterPage {
	t.Helper()

	rendered, err := engines.Rasterise(context.Background(), pdf.RasterRequest{
		Dir:         dir,
		Input:       path,
		Pages:       []int{page},
		SourcePages: page,
		DPI:         renderDPI,
		Format:      pdf.RasterPNG,
		Prefix:      prefix,
	})
	if err != nil {
		t.Fatalf("Rasterise(%s, page %d) = %v", filepath.Base(path), page, err)
	}

	if len(rendered) != 1 {
		t.Fatalf("Rasterise returned %d pages, want 1", len(rendered))
	}

	return rendered[0]
}

// renderOne rasterises one page and returns the image's bytes, for the
// comparisons that must prove a page did not change.
func renderOne(t *testing.T, engines *pdf.Engines, dir, path string, page int, prefix string) []byte {
	t.Helper()

	rendered := renderPage(t, engines, dir, path, page, prefix)

	body, err := os.ReadFile(rendered.Path)
	if err != nil {
		t.Fatalf("read the rendered page: %v", err)
	}

	return body
}

// extractText returns a document's text, with pages separated by a form feed
// the way pdftotext writes them.
func extractText(t *testing.T, engines *pdf.Engines, dir, path string) []byte {
	t.Helper()

	doc, err := engines.Inspect(context.Background(), dir, path)
	if err != nil {
		t.Fatalf("Inspect(%s) = %v", filepath.Base(path), err)
	}

	out := filepath.Join(dir, "text-"+filepath.Base(path)+".txt")

	err = engines.ExtractText(context.Background(), pdf.TextRequest{
		Dir:         dir,
		Input:       path,
		Output:      out,
		Layout:      true,
		SourcePages: doc.Count,
	})
	if err != nil {
		t.Fatalf("ExtractText(%s) = %v", filepath.Base(path), err)
	}

	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read the extracted text: %v", err)
	}

	return body
}
