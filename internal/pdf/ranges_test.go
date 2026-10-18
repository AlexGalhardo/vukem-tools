package pdf_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/AlexGalhardo/vukem-tools/internal/pdf"
)

func TestAPageRangeExpandsInTheOrderItWasWritten(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input string
		pages int
		want  []int
	}{
		{"one page", "3", 10, []int{3}},
		{"an ascending span", "2-4", 10, []int{2, 3, 4}},
		{"a descending span reverses", "4-2", 10, []int{4, 3, 2}},
		{"open on the right", "8-", 10, []int{8, 9, 10}},
		{"open on the left", "-3", 10, []int{1, 2, 3}},
		{"the last page", "z", 10, []int{10}},
		{"a span ending at the last page", "9-z", 10, []int{9, 10}},
		{"several parts keep their order", "5,1-2,z", 10, []int{5, 1, 2, 10}},
		{"a repeated page is kept", "1,1,2", 10, []int{1, 1, 2}},
		{"spaces are ignored", " 1 - 2 , 4 ", 10, []int{1, 2, 4}},
		{"the whole document", "1-z", 3, []int{1, 2, 3}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			got, err := pdf.ParsePageRange(c.input, c.pages)
			if err != nil {
				t.Fatalf("ParsePageRange(%q, %d) = %v", c.input, c.pages, err)
			}

			if !slices.Equal(got, c.want) {
				t.Errorf("ParsePageRange(%q, %d) = %v, want %v", c.input, c.pages, got, c.want)
			}
		})
	}
}

// A page the document does not have is a validation error rather than a page
// quietly dropped: ROADMAP S6.6 asks for exactly that, and the reason is that
// somebody asking for pages 1-20 of a 10-page document has made a mistake a
// silent answer would hide.
func TestAPageOutsideTheDocumentIsRefused(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input string
		pages int
	}{
		{"past the end", "11", 10},
		{"a span past the end", "8-12", 10},
		{"page zero", "0", 10},
		{"a negative page", "-0", 10},
		{"empty", "", 10},
		{"only separators", ",,", 10},
		{"not a number", "two", 10},
		{"a double dash", "1--2", 10},
		{"a trailing comma is still empty part", "1,", 10},
		{"an unbounded range with no document", "1-", 0},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			got, err := pdf.ParsePageRange(c.input, c.pages)
			if err == nil {
				t.Fatalf("ParsePageRange(%q, %d) = %v, want an error", c.input, c.pages, got)
			}

			rangeErr, ok := errors.AsType[*pdf.PageRangeError](err)
			if !ok {
				t.Fatalf("ParsePageRange(%q, %d) error is %T, want *pdf.PageRangeError", c.input, c.pages, err)
			}

			if rangeErr.Message() == "" {
				t.Error("a page range error must carry a sentence to show the user")
			}
		})
	}
}

// The message names the document's own page count, because "page 11 does not
// exist" is actionable and "invalid page range" is not.
func TestThePageRangeMessageNamesTheDocumentsLength(t *testing.T) {
	t.Parallel()

	_, err := pdf.ParsePageRange("11", 10)
	if err == nil {
		t.Fatal("want an error")
	}

	rangeErr, ok := errors.AsType[*pdf.PageRangeError](err)
	if !ok {
		t.Fatalf("error is %T, want *pdf.PageRangeError", err)
	}

	if !strings.Contains(rangeErr.Message(), "10") {
		t.Errorf("message %q should name the page count", rangeErr.Message())
	}
}

func TestComplementLeavesTheDocumentsOtherPagesInOrder(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		removed []int
		pages   int
		want    []int
	}{
		{"one page out of the middle", []int{3}, 5, []int{1, 2, 4, 5}},
		{"the first page", []int{1}, 3, []int{2, 3}},
		{"out of order, deduplicated", []int{4, 2, 2}, 5, []int{1, 3, 5}},
		{"nothing removed", nil, 3, []int{1, 2, 3}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			got := pdf.Complement(c.removed, c.pages)
			if !slices.Equal(got, c.want) {
				t.Errorf("Complement(%v, %d) = %v, want %v", c.removed, c.pages, got, c.want)
			}
		})
	}
}

// Removing every page would produce a document with no pages, which no reader
// opens. The refusal belongs to the tool, so Complement returning empty is the
// signal it checks.
func TestComplementOfEveryPageIsEmpty(t *testing.T) {
	t.Parallel()

	if got := pdf.Complement([]int{1, 2, 3}, 3); len(got) != 0 {
		t.Errorf("Complement = %v, want empty", got)
	}
}

func TestAPageRangeIsCappedSoOneRangeCannotExplode(t *testing.T) {
	t.Parallel()

	// A range may repeat a page, so its length is not bounded by the page
	// count: "1-z,1-z,1-z..." would be. The cap is what keeps the expansion
	// bounded by the option rather than by the document.
	huge := strings.Repeat("1-z,", 2000) + "1"

	_, err := pdf.ParsePageRange(huge, 1000)
	if err == nil {
		t.Fatal("want an error for a range that expands past the cap")
	}

	if _, ok := errors.AsType[*pdf.PageRangeError](err); !ok {
		t.Fatalf("error is %T, want *pdf.PageRangeError", err)
	}
}

func TestEveryNPagesSplitsIntoContiguousGroups(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		pages int
		every int
		want  [][]int
	}{
		{"single pages", 3, 1, [][]int{{1}, {2}, {3}}},
		{"pairs", 5, 2, [][]int{{1, 2}, {3, 4}, {5}}},
		{"one group", 3, 10, [][]int{{1, 2, 3}}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			got := pdf.GroupEvery(c.pages, c.every)
			if len(got) != len(c.want) {
				t.Fatalf("GroupEvery(%d, %d) = %v, want %v", c.pages, c.every, got, c.want)
			}

			for i := range got {
				if !slices.Equal(got[i], c.want[i]) {
					t.Errorf("group %d = %v, want %v", i, got[i], c.want[i])
				}
			}
		})
	}
}

func TestQPDFRangeIsTheCompactFormTheEngineReads(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		pages []int
		want  string
	}{
		{"a run collapses", []int{1, 2, 3}, "1-3"},
		{"singles stay", []int{1, 3, 5}, "1,3,5"},
		{"runs and singles", []int{1, 2, 3, 7, 9, 10}, "1-3,7,9-10"},
		{"a pair is not a run", []int{4, 5}, "4-5"},
		{"order is kept, not sorted", []int{3, 1}, "3,1"},
		{"a descending run collapses", []int{5, 4, 3}, "5-3"},
		{"a repeat is kept", []int{1, 1}, "1,1"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			if got := pdf.QPDFRange(c.pages); got != c.want {
				t.Errorf("QPDFRange(%v) = %q, want %q", c.pages, got, c.want)
			}
		})
	}
}
