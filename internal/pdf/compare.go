package pdf

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
)

// CompareRequest is one comparison.
type CompareRequest struct {
	Dir string
	// Left and Right are the two documents, "before" and "after".
	Left, Right string
	// LeftName and RightName are what to call them in the report. They are
	// the uploads' own names, so the report says "contract-v1.pdf" rather
	// than "the left document".
	LeftName, RightName string
	// Output is the Markdown report.
	Output string
}

// CompareResult is the summary of a comparison.
type CompareResult struct {
	// Pages is how many pages were compared, which is the longer document's
	// page count.
	Pages int
	// ChangedPages is how many pages differ.
	ChangedPages int
	// Added and Removed are line counts across the whole document.
	Added, Removed int
	// Identical reports whether the two documents' text is the same
	// everywhere.
	Identical bool
	// Truncated reports whether a page was too long to align line by line
	// and was compared as a set of lines instead. The report says so on the
	// page it happened to, because a reader has to know the diff on that
	// page is coarser.
	Truncated bool
}

// maxDiffLines is how many lines a page may have and still be aligned line by
// line.
//
// The alignment is a longest-common-subsequence walk, which costs one cell per
// pair of lines: 2000 by 2000 is four million cells, which is the most this
// service will spend on one page. Past that the page is compared as two sets
// of lines, which still finds every addition and removal and only loses the
// ability to say a line moved.
const maxDiffLines = 2000

// maxComparePages caps a comparison. Two thousand pages of text diff is a
// report nobody reads, and the pair of documents has to be read into memory a
// page at a time either way.
const maxComparePages = 2000

// Compare writes a page-aligned text difference between two documents.
//
// It is a text comparison and the report says so in its first line. The
// honest limit is that two documents whose text is identical and whose layout
// differs — a different font, a different margin, an image replaced — come
// out as identical here, because `pdftotext` reports text. ROADMAP S6.5 took
// this deliberately as the part worth having: a text diff answers "what
// changed in the contract", which is the question people actually ask, and a
// pixel diff is a later addition rather than a prerequisite.
func (e *Engines) Compare(ctx context.Context, req CompareRequest) (CompareResult, error) {
	left, err := e.comparePages(ctx, req.Dir, req.Left, "left")
	if err != nil {
		return CompareResult{}, err
	}

	right, err := e.comparePages(ctx, req.Dir, req.Right, "right")
	if err != nil {
		return CompareResult{}, err
	}

	file, err := os.Create(req.Output)
	if err != nil {
		return CompareResult{}, fmt.Errorf("pdf: create the report: %w", err)
	}

	out := bufio.NewWriter(file)
	result := writeComparison(out, req, left, right)

	flushErr := out.Flush()

	if err := file.Close(); err != nil && flushErr == nil {
		flushErr = err
	}

	if flushErr != nil {
		return CompareResult{}, fmt.Errorf("pdf: write the report: %w", err)
	}

	return result, nil
}

// comparePages extracts one document's text and splits it into pages.
func (e *Engines) comparePages(ctx context.Context, dir, path, side string) ([][]string, error) {
	doc, err := e.Inspect(ctx, dir, path)
	if err != nil {
		return nil, fmt.Errorf("pdf: the %s document: %w", side, err)
	}

	if doc.Count > maxComparePages {
		return nil, &TooManyPagesError{Pages: doc.Count, Limit: maxComparePages}
	}

	text, err := os.CreateTemp(dir, "compare-"+side+"-*.txt")
	if err != nil {
		return nil, fmt.Errorf("pdf: create a text file: %w", err)
	}

	name := text.Name()

	defer func() {
		_ = text.Close()
		_ = os.Remove(name)
	}()

	err = e.ExtractText(ctx, TextRequest{
		Dir:         dir,
		Input:       path,
		Output:      name,
		Layout:      true,
		SourcePages: doc.Count,
	})
	if err != nil {
		return nil, err
	}

	return readPages(name)
}

// readPages reads an extracted text file into one slice of lines per page.
func readPages(path string) ([][]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("pdf: open the extracted text: %w", err)
	}

	defer func() { _ = file.Close() }()

	in := bufio.NewScanner(file)
	in.Buffer(make([]byte, 0, 64<<10), maxTextLine)

	pages := [][]string{{}}

	for in.Scan() {
		line := in.Text()

		for {
			before, after, found := strings.Cut(line, string(pageBreak))

			pages[len(pages)-1] = appendMeaningful(pages[len(pages)-1], before)

			if !found {
				break
			}

			pages = append(pages, []string{})
			line = after
		}
	}

	if err := in.Err(); err != nil {
		return nil, &DamagedError{Detail: "could not read the extracted text: " + err.Error()}
	}

	// pdftotext ends the last page with a form feed, so the split leaves an
	// empty page after it that is not a page of the document.
	if len(pages) > 1 && len(pages[len(pages)-1]) == 0 {
		pages = pages[:len(pages)-1]
	}

	return pages, nil
}

// appendMeaningful adds a line unless it is blank.
//
// Blank lines are dropped because `-layout` emits them to represent vertical
// space, and a comparison that reported "a blank line moved" would bury the
// changes that matter under the ones that do not.
func appendMeaningful(lines []string, raw string) []string {
	if trimmed := strings.TrimSpace(raw); trimmed != "" {
		return append(lines, trimmed)
	}

	return lines
}

// writeComparison writes the Markdown report and returns the summary.
func writeComparison(out *bufio.Writer, req CompareRequest, left, right [][]string) CompareResult {
	result := CompareResult{Pages: max(len(left), len(right))}

	_, _ = fmt.Fprintf(out, "# Text comparison\n\n")
	_, _ = fmt.Fprintf(out, "- Before: `%s` (%d pages)\n", req.LeftName, len(left))
	_, _ = fmt.Fprintf(out, "- After: `%s` (%d pages)\n\n", req.RightName, len(right))
	_, _ = fmt.Fprintf(out, "This compares the **text** of the two documents, page by page. "+
		"A change to a font, a margin or a picture does not change the text, so it does not "+
		"appear here.\n\n")

	var body strings.Builder

	for page := range result.Pages {
		before := pageAt(left, page)
		after := pageAt(right, page)

		diff, truncated := diffLines(before, after)
		if len(diff) == 0 {
			continue
		}

		result.ChangedPages++
		result.Truncated = result.Truncated || truncated

		_, _ = fmt.Fprintf(&body, "## Page %d\n\n", page+1)

		if truncated {
			_, _ = fmt.Fprintf(&body, "This page has more than %d lines, so its lines are "+
				"compared as a set: a line that only moved is reported as removed and added.\n\n",
				maxDiffLines)
		}

		switch {
		case page >= len(left):
			_, _ = fmt.Fprintf(&body, "This page is only in `%s`.\n\n", req.RightName)
		case page >= len(right):
			_, _ = fmt.Fprintf(&body, "This page is only in `%s`.\n\n", req.LeftName)
		}

		body.WriteString("```diff\n")

		for _, line := range diff {
			if line.added {
				result.Added++
				body.WriteString("+ " + line.text + "\n")

				continue
			}

			result.Removed++
			body.WriteString("- " + line.text + "\n")
		}

		body.WriteString("```\n\n")
	}

	result.Identical = result.ChangedPages == 0

	if result.Identical {
		_, _ = fmt.Fprintf(out, "**The text of these two documents is identical.**\n")

		return result
	}

	_, _ = fmt.Fprintf(out, "**%d of %d pages differ**: %d lines removed, %d added.\n\n",
		result.ChangedPages, result.Pages, result.Removed, result.Added)
	_, _ = out.WriteString(body.String())

	return result
}

// pageAt returns a page's lines, or none when the document is shorter.
func pageAt(pages [][]string, index int) []string {
	if index >= len(pages) {
		return nil
	}

	return pages[index]
}

// diffLine is one line of a difference.
type diffLine struct {
	text  string
	added bool
}

// diffLines returns the lines that differ between two pages, and whether the
// comparison had to fall back to a set comparison.
//
// The alignment is a longest common subsequence, which is what makes the
// report readable: without it, inserting one line at the top of a page
// reports every line below it as changed.
func diffLines(before, after []string) ([]diffLine, bool) {
	if len(before) > maxDiffLines || len(after) > maxDiffLines {
		return diffSets(before, after), true
	}

	table := lcsTable(before, after)

	var out []diffLine

	i, j := 0, 0

	for i < len(before) && j < len(after) {
		switch {
		case before[i] == after[j]:
			i++
			j++
		case table[i+1][j] >= table[i][j+1]:
			out = append(out, diffLine{text: before[i]})
			i++
		default:
			out = append(out, diffLine{text: after[j], added: true})
			j++
		}
	}

	for ; i < len(before); i++ {
		out = append(out, diffLine{text: before[i]})
	}

	for ; j < len(after); j++ {
		out = append(out, diffLine{text: after[j], added: true})
	}

	return out, false
}

// lcsTable builds the longest-common-subsequence lengths for two pages.
func lcsTable(before, after []string) [][]int {
	table := make([][]int, len(before)+1)
	for i := range table {
		table[i] = make([]int, len(after)+1)
	}

	// Filled from the end, because each cell is defined in terms of the ones
	// below and to the right of it.
	for i, left := range slices.Backward(before) {
		for j, right := range slices.Backward(after) {
			if left == right {
				table[i][j] = table[i+1][j+1] + 1

				continue
			}

			table[i][j] = max(table[i+1][j], table[i][j+1])
		}
	}

	return table
}

// diffSets compares two pages as multisets of lines, for a page too long to
// align.
//
// It finds every line that was added and every line that was removed; what it
// cannot tell is that a line moved, which it reports as one of each. That is
// the honest degradation, and the report says it happened.
func diffSets(before, after []string) []diffLine {
	counts := make(map[string]int, len(before))
	for _, line := range before {
		counts[line]++
	}

	var out []diffLine

	for _, line := range after {
		if counts[line] > 0 {
			counts[line]--

			continue
		}

		out = append(out, diffLine{text: line, added: true})
	}

	for _, line := range before {
		if counts[line] > 0 {
			counts[line]--

			out = append(out, diffLine{text: line})
		}
	}

	return out
}

// ComparisonSummary is the one-line sentence to show next to the download,
// so a user learns the answer without opening the report.
func (r CompareResult) ComparisonSummary() string {
	if r.Identical {
		return "The text of the two documents is identical."
	}

	return strconv.Itoa(r.ChangedPages) + " of " + strconv.Itoa(r.Pages) +
		" pages differ: " + strconv.Itoa(r.Removed) + " lines removed, " +
		strconv.Itoa(r.Added) + " added."
}
