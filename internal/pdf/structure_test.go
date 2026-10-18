package pdf_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/AlexGalhardo/vukem-tools/internal/pdf"
)

// ROADMAP S6.1, first assert: "Merging two files produces a page count equal
// to the sum, and page 1 of the second input is byte-identical as rendered to
// page n+1 of the output".
func TestMergingTwoDocumentsSumsThePagesAndKeepsThemUnchanged(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF, pdf.EnginePDFToPPM)
	dir := t.TempDir()
	ctx := context.Background()

	first := writeFixture(t, dir, "first.pdf", 3)
	second := writeNamedFixture(t, dir, "second.pdf", []string{"Second one", "Second two"})
	out := filepath.Join(dir, "merged.pdf")

	result, err := engines.Merge(ctx, pdf.MergeRequest{Dir: dir, Inputs: []string{first, second}, Output: out})
	if err != nil {
		t.Fatalf("Merge = %v", err)
	}

	if result.Pages != 5 {
		t.Errorf("Pages = %d, want 5 (3 + 2)", result.Pages)
	}

	if !slices.Equal(result.InputPages, []int{3, 2}) {
		t.Errorf("InputPages = %v, want [3 2]", result.InputPages)
	}

	doc, err := engines.Inspect(ctx, dir, out)
	if err != nil {
		t.Fatalf("Inspect the merged document = %v", err)
	}

	if doc.Count != 5 {
		t.Errorf("the merged document has %d pages, want 5", doc.Count)
	}

	// Page 1 of the second input must render identically to page 4 of the
	// output. Rendered rather than compared as PDF objects, because qpdf
	// legitimately renumbers objects and rewrites the file: what must not
	// change is what the page looks like.
	original := renderOne(t, engines, dir, second, 1, "orig")
	merged := renderOne(t, engines, dir, out, 4, "merged")

	if !bytes.Equal(original, merged) {
		t.Errorf("page 1 of the second input renders to %d bytes, page 4 of the output to %d: "+
			"a merge must not change a page", len(original), len(merged))
	}
}

// ROADMAP S6.1, second assert: "Splitting then merging the parts returns a
// document whose rendered pages match the original page for page".
func TestSplittingThenMergingReturnsTheSameDocumentPageForPage(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF, pdf.EnginePDFToPPM)
	dir := t.TempDir()
	ctx := context.Background()

	source := writeFixture(t, dir, "source.pdf", 5)

	parts, err := engines.Split(ctx, pdf.SplitRequest{
		Dir:         dir,
		Input:       source,
		SourcePages: 5,
		Groups:      pdf.GroupEvery(5, 1),
		Prefix:      "part",
	})
	if err != nil {
		t.Fatalf("Split = %v", err)
	}

	if len(parts) != 5 {
		t.Fatalf("Split produced %d parts, want 5", len(parts))
	}

	paths := make([]string, 0, len(parts))
	for _, part := range parts {
		paths = append(paths, part.Path)
	}

	rejoined := filepath.Join(dir, "rejoined.pdf")

	if _, err := engines.Merge(ctx, pdf.MergeRequest{Dir: dir, Inputs: paths, Output: rejoined}); err != nil {
		t.Fatalf("Merge the parts = %v", err)
	}

	for page := 1; page <= 5; page++ {
		before := renderOne(t, engines, dir, source, page, "before")
		after := renderOne(t, engines, dir, rejoined, page, "after")

		if !bytes.Equal(before, after) {
			t.Errorf("page %d differs after a split and a merge", page)
		}
	}
}

// ROADMAP S6.1, third assert: an operation either preserves the outline or
// says it does not. These are the four answers, each proved rather than
// claimed — qpdf's --empty drops an outline and its base-document form keeps
// one, so which form each operation uses is the whole behaviour.
func TestWhichOperationsKeepTheOutlineIsWhatTheToolsClaim(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF)
	dir := t.TempDir()
	ctx := context.Background()

	source := writeOutlinedFixture(t, engines, dir, "outlined.pdf", 4)

	titles, err := engines.OutlineTitles(ctx, dir, source)
	if err != nil {
		t.Fatalf("OutlineTitles = %v", err)
	}

	if len(titles) == 0 {
		t.Skip("the fixture has no outline, so there is nothing to preserve; " +
			"writeOutlinedFixture needs an engine that can add one")
	}

	t.Run("rotation keeps it", func(t *testing.T) {
		t.Parallel()

		out := filepath.Join(dir, "rotated.pdf")

		err := engines.Rotate(ctx, pdf.RotateRequest{
			Dir: dir, Input: source, Output: out, Degrees: 90, SourcePages: 4,
		})
		if err != nil {
			t.Fatalf("Rotate = %v", err)
		}

		assertOutline(t, engines, dir, out, len(titles))
	})

	t.Run("reordering every page keeps it", func(t *testing.T) {
		t.Parallel()

		out := filepath.Join(dir, "reordered.pdf")
		pages := []int{4, 3, 2, 1}

		if !pdf.KeepsOutline(pages, 4) {
			t.Fatal("KeepsOutline says a reordering loses the outline; it does not")
		}

		err := engines.Select(ctx, pdf.SelectRequest{
			Dir: dir, Input: source, Output: out, Pages: pages, SourcePages: 4,
		})
		if err != nil {
			t.Fatalf("Select = %v", err)
		}

		assertOutline(t, engines, dir, out, len(titles))
	})

	t.Run("extracting a subset drops it, and says so", func(t *testing.T) {
		t.Parallel()

		out := filepath.Join(dir, "extracted.pdf")
		pages := []int{1, 2}

		if pdf.KeepsOutline(pages, 4) {
			t.Fatal("KeepsOutline says an extraction keeps the outline; it cannot")
		}

		err := engines.Select(ctx, pdf.SelectRequest{
			Dir: dir, Input: source, Output: out, Pages: pages, SourcePages: 4,
		})
		if err != nil {
			t.Fatalf("Select = %v", err)
		}

		// Dropped, not dangling. An outline whose entries point at pages that
		// are no longer in the document is the regression the assert names.
		assertOutline(t, engines, dir, out, 0)
	})

	t.Run("a merge keeps the first document's", func(t *testing.T) {
		t.Parallel()

		plain := writeFixture(t, dir, "plain-for-merge.pdf", 2)
		out := filepath.Join(dir, "merged-outline.pdf")

		result, err := engines.Merge(ctx, pdf.MergeRequest{
			Dir: dir, Inputs: []string{source, plain}, Output: out,
		})
		if err != nil {
			t.Fatalf("Merge = %v", err)
		}

		if !result.OutlineKept {
			t.Error("OutlineKept = false, but the first document had an outline")
		}

		assertOutline(t, engines, dir, out, len(titles))
	})
}

// The other half of ROADMAP S6.1's third assert: the internal links. Unlike
// the outline, these do survive every page operation, because a link
// annotation belongs to the page rather than to the document — so the claim
// in the tool descriptions is that links are kept, and this is what makes it
// a tested claim rather than a hopeful one.
func TestInternalLinksSurviveEveryPageOperation(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF)
	dir := t.TempDir()
	ctx := context.Background()

	source := writeOutlinedFixture(t, engines, dir, "linked.pdf", 4)

	before := countLinks(t, source)
	if before == 0 {
		t.Fatal("the fixture has no link annotation, so there is nothing to preserve")
	}

	plain := writeFixture(t, dir, "plain.pdf", 2)

	cases := []struct {
		name string
		run  func(out string) error
	}{
		{"merge", func(out string) error {
			_, err := engines.Merge(ctx, pdf.MergeRequest{
				Dir: dir, Inputs: []string{source, plain}, Output: out,
			})

			return err
		}},
		{"extract", func(out string) error {
			return engines.Select(ctx, pdf.SelectRequest{
				Dir: dir, Input: source, Output: out, Pages: []int{1, 4}, SourcePages: 4,
			})
		}},
		{"reorder", func(out string) error {
			return engines.Select(ctx, pdf.SelectRequest{
				Dir: dir, Input: source, Output: out, Pages: []int{4, 3, 2, 1}, SourcePages: 4,
			})
		}},
		{"rotate", func(out string) error {
			return engines.Rotate(ctx, pdf.RotateRequest{
				Dir: dir, Input: source, Output: out, Degrees: 180, SourcePages: 4,
			})
		}},
		{"crop", func(out string) error {
			_, err := engines.Crop(ctx, pdf.CropRequest{
				Dir: dir, Input: source, Output: out, Margins: pdf.Margins{Top: 24},
			})

			return err
		}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			out := filepath.Join(dir, "linked-"+c.name+".pdf")

			if err := c.run(out); err != nil {
				t.Fatalf("%s = %v", c.name, err)
			}

			if got := countLinks(t, out); got < before {
				t.Errorf("%s left %d link annotations, want at least %d", c.name, got, before)
			}
		})
	}
}

// countLinks counts a document's link annotations by reading its bytes.
//
// Read rather than asked of an engine: qpdf's JSON has no key for annotations,
// and the thing being tested is that the annotation is still in the file.
func countLinks(t *testing.T, path string) int {
	t.Helper()

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return bytes.Count(body, []byte("/Link"))
}

// assertOutline checks how many top-level bookmarks a document has.
func assertOutline(t *testing.T, engines *pdf.Engines, dir, path string, want int) {
	t.Helper()

	titles, err := engines.OutlineTitles(context.Background(), dir, path)
	if err != nil {
		t.Fatalf("OutlineTitles(%s) = %v", filepath.Base(path), err)
	}

	if len(titles) != want {
		t.Errorf("%s has %d bookmarks, want %d", filepath.Base(path), len(titles), want)
	}
}

func TestSelectRefusesASelectionThatWouldEmptyTheDocument(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF)
	dir := t.TempDir()

	source := writeFixture(t, dir, "source.pdf", 3)
	out := filepath.Join(dir, "out.pdf")

	err := engines.Select(context.Background(), pdf.SelectRequest{
		Dir: dir, Input: source, Output: out, Pages: nil, SourcePages: 3,
	})

	if _, ok := errors.AsType[*pdf.OptionError](err); !ok {
		t.Fatalf("Select with no pages = %v, want *pdf.OptionError", err)
	}

	if _, statErr := os.Stat(out); statErr == nil {
		t.Error("a refused selection wrote an output file")
	}
}

func TestRemovingPagesIsSelectingTheOthers(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF, pdf.EnginePDFToText)
	dir := t.TempDir()
	ctx := context.Background()

	source := writeNamedFixture(t, dir, "source.pdf", []string{"Alpha", "Beta", "Gamma", "Delta"})
	out := filepath.Join(dir, "removed.pdf")

	keep := pdf.Complement([]int{2, 3}, 4)

	err := engines.Select(ctx, pdf.SelectRequest{
		Dir: dir, Input: source, Output: out, Pages: keep, SourcePages: 4,
	})
	if err != nil {
		t.Fatalf("Select = %v", err)
	}

	text := extractText(t, engines, dir, out)

	for _, gone := range []string{"Beta", "Gamma"} {
		if bytes.Contains(text, []byte(gone)) {
			t.Errorf("%q is still in the document after being removed", gone)
		}
	}

	for _, kept := range []string{"Alpha", "Delta"} {
		if !bytes.Contains(text, []byte(kept)) {
			t.Errorf("%q was removed but should have been kept", kept)
		}
	}
}

func TestOrganiseReordersPagesAsAsked(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF, pdf.EnginePDFToText)
	dir := t.TempDir()
	ctx := context.Background()

	source := writeNamedFixture(t, dir, "source.pdf", []string{"Alpha", "Beta", "Gamma"})
	out := filepath.Join(dir, "organised.pdf")

	err := engines.Select(ctx, pdf.SelectRequest{
		Dir: dir, Input: source, Output: out, Pages: []int{3, 1, 2}, SourcePages: 3,
	})
	if err != nil {
		t.Fatalf("Select = %v", err)
	}

	pages := bytes.Split(extractText(t, engines, dir, out), []byte{'\f'})
	if len(pages) < 3 {
		t.Fatalf("the organised document has %d pages of text", len(pages))
	}

	for i, want := range []string{"Gamma", "Alpha", "Beta"} {
		if !bytes.Contains(pages[i], []byte(want)) {
			t.Errorf("page %d is %q, want it to hold %q", i+1, pages[i], want)
		}
	}
}

func TestSplitRefusesToProduceMoreFilesThanTheResultLifetimeAllows(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF)
	dir := t.TempDir()

	groups := make([][]int, pdf.MaxSplitParts+1)
	for i := range groups {
		groups[i] = []int{1}
	}

	_, err := engines.Split(context.Background(), pdf.SplitRequest{
		Dir: dir, Input: filepath.Join(dir, "missing.pdf"), SourcePages: 1, Groups: groups, Prefix: "p",
	})

	if _, ok := errors.AsType[*pdf.OptionError](err); !ok {
		t.Fatalf("Split with %d parts = %v, want *pdf.OptionError", len(groups), err)
	}
}

func TestMergeRefusesASingleDocument(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF)
	dir := t.TempDir()

	_, err := engines.Merge(context.Background(), pdf.MergeRequest{
		Dir: dir, Inputs: []string{writeFixture(t, dir, "one.pdf", 1)}, Output: filepath.Join(dir, "o.pdf"),
	})

	if _, ok := errors.AsType[*pdf.OptionError](err); !ok {
		t.Fatalf("Merge of one document = %v, want *pdf.OptionError", err)
	}
}

func TestRotationIsAMultipleOfNinety(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF)
	dir := t.TempDir()
	source := writeFixture(t, dir, "source.pdf", 1)

	for _, degrees := range []int{0, 45, 100, 360, -90} {
		err := engines.Rotate(context.Background(), pdf.RotateRequest{
			Dir: dir, Input: source, Output: filepath.Join(dir, "r.pdf"), Degrees: degrees, SourcePages: 1,
		})

		if _, ok := errors.AsType[*pdf.OptionError](err); !ok {
			t.Errorf("Rotate by %d = %v, want *pdf.OptionError", degrees, err)
		}
	}
}

// Cropping sets the crop box and leaves the content, the outline and the
// links alone, which is the point of doing it through qpdf's object editing
// rather than through Ghostscript.
func TestCroppingChangesThePageBoxAndNothingElse(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF, pdf.EnginePDFToPPM)
	dir := t.TempDir()
	ctx := context.Background()

	source := writeFixture(t, dir, "source.pdf", 2)
	out := filepath.Join(dir, "cropped.pdf")

	result, err := engines.Crop(ctx, pdf.CropRequest{
		Dir:     dir,
		Input:   source,
		Output:  out,
		Margins: pdf.Margins{Left: 36, Bottom: 36, Right: 36, Top: 36},
	})
	if err != nil {
		t.Fatalf("Crop = %v", err)
	}

	if result.Cropped != 2 {
		t.Errorf("Cropped = %d, want 2", result.Cropped)
	}

	a4, _ := pdf.PageSize("a4")

	for i, box := range result.Boxes {
		if got := box.Width(); got >= a4.Width() {
			t.Errorf("page %d is %g points wide, want narrower than %g", i+1, got, a4.Width())
		}
	}

	// Rendered at a fixed resolution, the cropped page must be smaller than
	// the original by the margins: 72 points at 72 DPI is 72 pixels off each
	// dimension.
	before := renderPage(t, engines, dir, source, 1, "crop-before")
	after := renderPage(t, engines, dir, out, 1, "crop-after")

	if after.Width >= before.Width || after.Height >= before.Height {
		t.Errorf("cropped page renders %dx%d, original %dx%d: the crop box was not applied",
			after.Width, after.Height, before.Width, before.Height)
	}
}

func TestCropRefusesMarginsThatLeaveNothing(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF)
	dir := t.TempDir()

	_, err := engines.Crop(context.Background(), pdf.CropRequest{
		Dir:     dir,
		Input:   writeFixture(t, dir, "source.pdf", 1),
		Output:  filepath.Join(dir, "out.pdf"),
		Margins: pdf.Margins{Left: 400, Right: 400},
	})

	if _, ok := errors.AsType[*pdf.OptionError](err); !ok {
		t.Fatalf("Crop with impossible margins = %v, want *pdf.OptionError", err)
	}
}

func TestCropNeedsSomethingToDo(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF)
	dir := t.TempDir()

	_, err := engines.Crop(context.Background(), pdf.CropRequest{
		Dir:    dir,
		Input:  writeFixture(t, dir, "source.pdf", 1),
		Output: filepath.Join(dir, "out.pdf"),
	})

	if _, ok := errors.AsType[*pdf.OptionError](err); !ok {
		t.Fatalf("Crop with no margins and no box = %v, want *pdf.OptionError", err)
	}
}
