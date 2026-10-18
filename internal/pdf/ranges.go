package pdf

import (
	"slices"
	"strconv"
	"strings"
)

// MaxRangePages caps how many pages one range may expand to.
//
// A range may name a page twice, so its length is not bounded by the
// document: "1-z,1-z,1-z,..." expands to whatever the caller wrote. The cap is
// what keeps a structural operation bounded by its options rather than by a
// string somebody sent.
const MaxRangePages = 20_000

// lastPageToken is how a range names the document's last page without
// knowing how long it is, which is the one thing a client cannot look up
// before submitting.
const lastPageToken = "z"

// ParsePageRange expands a page range against a document of pages pages.
//
// The syntax is deliberately the small, readable subset of qpdf's: a page
// number, a span `a-b`, an open span `a-` or `-b`, and `z` for the last page,
// joined by commas. What it does not have is as important as what it does —
// no "odd", no "even", no step — because every one of those is a thing a user
// would have to read documentation to predict.
//
// The order written is the order returned, and a repeated page is kept. Both
// matter: "Organise PDF" is exactly a reordering, and duplicating a page is a
// legitimate thing to ask of it.
func ParsePageRange(input string, pages int) ([]int, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return nil, &PageRangeError{Input: input, Reason: "Name at least one page, for example 1-3,5."}
	}

	var out []int

	for part := range strings.SplitSeq(trimmed, ",") {
		span, err := parseSpan(strings.TrimSpace(part), input, pages)
		if err != nil {
			return nil, err
		}

		if len(out)+len(span) > MaxRangePages {
			return nil, &PageRangeError{
				Input:  input,
				Reason: "That page range covers more than " + strconv.Itoa(MaxRangePages) + " pages.",
			}
		}

		out = append(out, span...)
	}

	return out, nil
}

// parseSpan expands one comma-separated part.
func parseSpan(part, input string, pages int) ([]int, error) {
	if part == "" {
		return nil, &PageRangeError{
			Input:  input,
			Reason: "That page range has an empty part. Write it as 1-3,5 with no stray commas.",
		}
	}

	from, to, isSpan := strings.Cut(part, "-")
	if !isSpan {
		page, err := parsePage(part, input, pages)
		if err != nil {
			return nil, err
		}

		return []int{page}, nil
	}

	start, end, err := spanBounds(strings.TrimSpace(from), strings.TrimSpace(to), input, pages)
	if err != nil {
		return nil, err
	}

	return sequence(start, end), nil
}

// spanBounds resolves the two ends of a span, filling in an open end.
func spanBounds(from, to, input string, pages int) (int, int, error) {
	start, end := 1, pages

	if from != "" {
		page, err := parsePage(from, input, pages)
		if err != nil {
			return 0, 0, err
		}

		start = page
	}

	if to != "" {
		page, err := parsePage(to, input, pages)
		if err != nil {
			return 0, 0, err
		}

		end = page
	}

	// An open span against a document with no pages has no honest answer, and
	// a document with no pages is not a document this service accepts.
	if start < 1 || end < 1 {
		return 0, 0, &PageRangeError{
			Input: input,
			Reason: "That page range names a page this document does not have: it has " +
				strconv.Itoa(pages) + ".",
		}
	}

	return start, end, nil
}

// sequence returns the pages from start to end inclusive, ascending or
// descending. A descending span reverses the pages, which is what somebody
// writing "10-1" asked for.
func sequence(start, end int) []int {
	if start <= end {
		out := make([]int, 0, end-start+1)
		for page := start; page <= end; page++ {
			out = append(out, page)
		}

		return out
	}

	out := make([]int, 0, start-end+1)
	for page := start; page >= end; page-- {
		out = append(out, page)
	}

	return out
}

// parsePage reads one page number, resolving the last-page token, and refuses
// anything outside the document.
func parsePage(token, input string, pages int) (int, error) {
	if token == lastPageToken {
		if pages < 1 {
			return 0, &PageRangeError{Input: input, Reason: "This document has no pages."}
		}

		return pages, nil
	}

	page, err := strconv.Atoi(token)
	if err != nil {
		return 0, &PageRangeError{
			Input:  input,
			Reason: "\"" + token + "\" is not a page number. Use numbers, spans like 2-5, or z for the last page.",
		}
	}

	if page < 1 || page > pages {
		return 0, &PageRangeError{
			Input: input,
			Reason: "That page range names page " + strconv.Itoa(page) +
				", but this document has " + strconv.Itoa(pages) + ".",
		}
	}

	return page, nil
}

// Complement returns the pages of a document that removed does not name, in
// ascending order.
//
// It is how "remove pages" is expressed: the engine can only be told which
// pages to keep, so removing is selecting the others. Duplicates and order in
// removed are irrelevant, which is why it deduplicates rather than refusing.
func Complement(removed []int, pages int) []int {
	drop := make(map[int]struct{}, len(removed))
	for _, page := range removed {
		drop[page] = struct{}{}
	}

	out := make([]int, 0, max(pages-len(drop), 0))

	for page := 1; page <= pages; page++ {
		if _, gone := drop[page]; !gone {
			out = append(out, page)
		}
	}

	return out
}

// GroupEvery splits a document of pages pages into contiguous groups of at
// most every pages. The last group is short when the count does not divide.
func GroupEvery(pages, every int) [][]int {
	if pages < 1 || every < 1 {
		return nil
	}

	out := make([][]int, 0, (pages+every-1)/every)

	for start := 1; start <= pages; start += every {
		out = append(out, sequence(start, min(start+every-1, pages)))
	}

	return out
}

// QPDFRange renders pages as the compact range qpdf's --pages reads.
//
// Runs are collapsed because the argument goes on a command line: a 5000-page
// document selected one page at a time would otherwise be a 25-kilobyte
// argument, and several kernels cap one argument well below that. The order
// is kept exactly, never sorted, because the order is the instruction.
func QPDFRange(pages []int) string {
	if len(pages) == 0 {
		return ""
	}

	var out strings.Builder

	for i := 0; i < len(pages); {
		end := runEnd(pages, i)

		if i > 0 {
			out.WriteByte(',')
		}

		out.WriteString(strconv.Itoa(pages[i]))

		if end > i {
			out.WriteByte('-')
			out.WriteString(strconv.Itoa(pages[end]))
		}

		i = end + 1
	}

	return out.String()
}

// runEnd returns the index of the last page in the consecutive run starting
// at i. A run of two is still a run: "4-5" is shorter than "4,5".
func runEnd(pages []int, i int) int {
	if i+1 >= len(pages) {
		return i
	}

	step := pages[i+1] - pages[i]
	if step != 1 && step != -1 {
		return i
	}

	end := i + 1
	for end+1 < len(pages) && pages[end+1]-pages[end] == step {
		end++
	}

	return end
}

// highest returns the largest page in pages, or zero when there are none. It
// is what checks a range against a document before an engine sees it.
func highest(pages []int) int {
	if len(pages) == 0 {
		return 0
	}

	return slices.Max(pages)
}
