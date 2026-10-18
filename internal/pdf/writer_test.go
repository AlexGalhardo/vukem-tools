package pdf_test

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/AlexGalhardo/vukem-tools/internal/pdf"
)

func TestAWrittenDocumentHasTheStructureAReaderNeeds(t *testing.T) {
	t.Parallel()

	a4, _ := pdf.PageSize("a4")

	var out bytes.Buffer
	if err := pdf.Write(&out, []pdf.PageSpec{
		{Box: a4, Text: []pdf.TextStamp{{Text: "Page one", Font: pdf.Helvetica, Size: 12, X: 72, Y: 720}}},
		{Box: a4, Text: []pdf.TextStamp{{Text: "Page two", Font: pdf.Helvetica, Size: 12, X: 72, Y: 720}}},
	}); err != nil {
		t.Fatalf("Write = %v", err)
	}

	body := out.String()

	for _, want := range []string{
		"%PDF-1.7\n",
		"/Type /Catalog",
		"/Type /Pages /Count 2",
		"/BaseFont /Helvetica",
		"/Encoding /WinAnsiEncoding",
		"(Page one) Tj",
		"(Page two) Tj",
		"xref\n",
		"trailer\n",
		"startxref\n",
		"%%EOF\n",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the document is missing %q", want)
		}
	}
}

// Every offset in the cross-reference table must actually be the first byte
// of the object it claims. A reader that trusts the table and finds something
// else there reports the file as damaged, and the only way to be sure is to
// check each one.
func TestTheCrossReferenceTableOffsetsPointAtTheirObjects(t *testing.T) {
	t.Parallel()

	a4, _ := pdf.PageSize("a4")

	var out bytes.Buffer
	if err := pdf.Write(&out, []pdf.PageSpec{
		{Box: a4, Text: []pdf.TextStamp{{Text: "One", Font: pdf.Helvetica, Size: 24, X: 10, Y: 10, Opacity: 0.4}}},
		{Box: a4},
	}); err != nil {
		t.Fatalf("Write = %v", err)
	}

	raw := out.Bytes()

	start := startxrefOf(t, raw)
	if !bytes.HasPrefix(raw[start:], []byte("xref\n")) {
		t.Fatalf("startxref points at %q, not at the table", raw[start:min(start+16, len(raw))])
	}

	for obj, offset := range xrefOffsets(t, raw[start:]) {
		if offset == 0 {
			continue // object 0 is the free-list head
		}

		want := strconv.Itoa(obj) + " 0 obj"
		if !bytes.HasPrefix(raw[offset:], []byte(want)) {
			t.Errorf("object %d: offset %d holds %q, want %q",
				obj, offset, raw[offset:min(offset+len(want), len(raw))], want)
		}
	}
}

// startxrefOf reads the trailer's pointer to the cross-reference table.
func startxrefOf(t *testing.T, raw []byte) int {
	t.Helper()

	match := regexp.MustCompile(`startxref\n(\d+)\n`).FindSubmatch(raw)
	if match == nil {
		t.Fatal("the document has no startxref")
	}

	n, err := strconv.Atoi(string(match[1]))
	if err != nil {
		t.Fatalf("startxref is not a number: %v", err)
	}

	return n
}

// xrefOffsets reads the table's entries, indexed by object number.
func xrefOffsets(t *testing.T, table []byte) []int {
	t.Helper()

	entries := regexp.MustCompile(`(?m)^(\d{10}) \d{5} [nf] $`).FindAllSubmatch(table, -1)
	if len(entries) < 2 {
		t.Fatalf("the table has %d entries", len(entries))
	}

	out := make([]int, 0, len(entries))

	for _, entry := range entries {
		n, err := strconv.Atoi(string(entry[1]))
		if err != nil {
			t.Fatalf("offset %q is not a number: %v", entry[1], err)
		}

		out = append(out, n)
	}

	return out
}

// A literal string that is not escaped is how user text rewrites the file
// around it: a watermark reading ") Tj ET Q" would otherwise close the string
// and then be executed as operators.
func TestTextIsEscapedSoItCannotEscapeItsOwnString(t *testing.T) {
	t.Parallel()

	a4, _ := pdf.PageSize("a4")

	var out bytes.Buffer
	if err := pdf.Write(&out, []pdf.PageSpec{{
		Box:  a4,
		Text: []pdf.TextStamp{{Text: `a) Tj ET Q 0 0 1 rg (b\c(`, Font: pdf.Helvetica, Size: 12, X: 1, Y: 1}},
	}}); err != nil {
		t.Fatalf("Write = %v", err)
	}

	body := out.String()

	want := `(a\) Tj ET Q 0 0 1 rg \(b\\c\() Tj`
	if !strings.Contains(body, want) {
		t.Errorf("the escaped string is missing.\nwant %s\ngot  %s", want, body)
	}

	// One Tj operator, so nothing in the text became an operator of its own.
	// The text itself contains the three characters ") Tj", which is the
	// point: after escaping they read as `\) Tj` and close nothing, so only
	// an unescaped `)` before Tj counts as an operator.
	operators := regexp.MustCompile(`[^\\]\) Tj`).FindAll(out.Bytes(), -1)
	if len(operators) != 1 {
		t.Errorf("the document has %d Tj operators, want 1", len(operators))
	}
}

func TestAnEmptyDocumentIsRefused(t *testing.T) {
	t.Parallel()

	if err := pdf.Write(&bytes.Buffer{}, nil); err == nil {
		t.Error("Write with no pages should fail: a PDF with no pages does not open")
	}
}

// A PDF number may not carry an exponent, so a very small coordinate has to
// come out as a decimal.
func TestSmallCoordinatesAreNotWrittenInExponentNotation(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	if err := pdf.Write(&out, []pdf.PageSpec{{
		Box:  pdf.Box{LLX: 0, LLY: 0, URX: 100, URY: 100},
		Text: []pdf.TextStamp{{Text: "x", Font: pdf.Helvetica, Size: 0.004, X: 0.00001, Y: 0.00002}},
	}}); err != nil {
		t.Fatalf("Write = %v", err)
	}

	if strings.ContainsAny(out.String(), "eE") {
		// "e" appears in words like "endobj", so only the numeric form
		// matters; this checks the specific spelling exponents take.
		if regexp.MustCompile(`\d[eE][-+]?\d`).Match(out.Bytes()) {
			t.Error("a coordinate was written in exponent notation, which no PDF reader accepts")
		}
	}
}

func TestTextWidthGrowsWithTheTextAndTheSize(t *testing.T) {
	t.Parallel()

	narrow := pdf.TextWidth(pdf.Helvetica, "ii", 12)
	wide := pdf.TextWidth(pdf.Helvetica, "WW", 12)

	if narrow >= wide {
		t.Errorf("TextWidth(ii) = %g, TextWidth(WW) = %g: a narrow pair must measure less", narrow, wide)
	}

	if doubled := pdf.TextWidth(pdf.Helvetica, "ii", 24); doubled != narrow*2 {
		t.Errorf("TextWidth at 24pt = %g, want twice %g", doubled, narrow)
	}

	if pdf.TextWidth(pdf.Helvetica, "", 12) != 0 {
		t.Error("empty text must measure zero")
	}
}

// Bold Helvetica is wider than regular for the same string, which is the
// cheapest check that the two tables are not the same table.
func TestTheBoldFontIsNotTheRegularFontsTable(t *testing.T) {
	t.Parallel()

	regular := pdf.TextWidth(pdf.Helvetica, "abcdefgh", 12)
	bold := pdf.TextWidth(pdf.HelveticaBold, "abcdefgh", 12)

	if bold <= regular {
		t.Errorf("bold = %g, regular = %g: bold Helvetica is wider", bold, regular)
	}
}

func TestPageSizesAreTheOnesTheAPIAdvertises(t *testing.T) {
	t.Parallel()

	for _, name := range pdf.PageSizeNames() {
		size, ok := pdf.PageSize(name)
		if !ok {
			t.Errorf("PageSize(%q) is advertised but not known", name)

			continue
		}

		if size.Width() <= 0 || size.Height() <= 0 {
			t.Errorf("PageSize(%q) = %+v, which has no area", name, size)
		}
	}

	if _, ok := pdf.PageSize("tabloid"); ok {
		t.Error("PageSize should not know a size it does not advertise")
	}

	// Case and surrounding space come from a form field, not from code.
	if _, ok := pdf.PageSize(" A4 "); !ok {
		t.Error(`PageSize(" A4 ") should resolve: the value comes from a form`)
	}
}

// writeFixture writes a small PDF to dir and returns its path.
//
// Every engine-backed test needs an input, and building one with Ghostscript
// would mean the fixtures could only be made where the engines are. This
// package can write a PDF, so the fixtures cost nothing and the tests read
// the same on a laptop as in the container.
func writeFixture(t *testing.T, dir, name string, pages int) string {
	t.Helper()

	a4, _ := pdf.PageSize("a4")

	specs := make([]pdf.PageSpec, 0, pages)

	for i := 1; i <= pages; i++ {
		specs = append(specs, pdf.PageSpec{
			Box: a4,
			Text: []pdf.TextStamp{{
				Text: "Fixture page " + strconv.Itoa(i),
				Font: pdf.Helvetica,
				Size: 24,
				X:    72,
				Y:    720,
			}},
		})
	}

	path := filepath.Join(dir, name)

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}

	if err := pdf.Write(file, specs); err != nil {
		_ = file.Close()
		t.Fatalf("write %s: %v", path, err)
	}

	if err := file.Close(); err != nil {
		t.Fatalf("close %s: %v", path, err)
	}

	return path
}
