package pdf

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// textTimeout bounds one text extraction. pdftotext is linear in the
// document's content streams and does not rasterise, so even a large document
// is seconds.
const textTimeout = 10 * time.Minute

// pageBreak is the character pdftotext writes between pages, which is the
// only page boundary its output carries.
const pageBreak = '\f'

// TextRequest is one text extraction.
type TextRequest struct {
	Dir    string
	Input  string
	Output string
	// Layout keeps the page's column layout with spaces, which is what makes
	// a table recognisable and what makes a paragraph hard-wrapped. It is on
	// by default for Markdown, where the wrapping is then undone, and
	// optional for plain text, where some people want it and some do not.
	Layout bool
	// Pages are the 1-based pages to read. Empty reads all.
	Pages []int
	// SourcePages is the input's page count.
	SourcePages int
}

// ExtractText writes a document's text to Output.
//
// pdftotext writes the file itself rather than printing to a pipe, so nothing
// here holds the text: a 2000-page document's text goes from the engine to
// disk without passing through this process.
func (e *Engines) ExtractText(ctx context.Context, req TextRequest) error {
	args := []string{}
	if req.Layout {
		args = append(args, "-layout")
	}

	first, last, err := req.bounds()
	if err != nil {
		return err
	}

	if first > 0 {
		args = append(args, "-f", strconv.Itoa(first), "-l", strconv.Itoa(last))
	}

	// UTF-8 explicitly: pdftotext's default encoding is build-dependent, and
	// a document that came out as Latin-1 on one image and UTF-8 on another
	// would be a difference nobody could explain.
	args = append(args, "-enc", "UTF-8", req.Input, req.Output)

	if _, err := e.run(ctx, command{
		engine:  EnginePDFToText,
		args:    args,
		dir:     req.Dir,
		timeout: textTimeout,
	}); err != nil {
		return textFailure(err)
	}

	return nil
}

// bounds returns the first and last page to read, or 0 for "the whole
// document".
//
// pdftotext takes a contiguous range rather than a list, so a selection that
// is not contiguous is refused instead of being silently widened: returning
// pages 1-10 to somebody who asked for 1,5,9 would be worse than telling them
// the tool reads a range.
func (r TextRequest) bounds() (int, int, error) {
	if len(r.Pages) == 0 {
		return 0, 0, nil
	}

	first, last := r.Pages[0], r.Pages[0]

	for i, page := range r.Pages {
		if page != first+i {
			return 0, 0, &PageRangeError{
				Input:  QPDFRange(r.Pages),
				Reason: "Text extraction reads one continuous range of pages, such as 1-20.",
			}
		}

		last = page
	}

	if last > r.SourcePages {
		return 0, 0, &PageRangeError{
			Input: QPDFRange(r.Pages),
			Reason: "That page range names page " + strconv.Itoa(last) +
				", but this document has " + strconv.Itoa(r.SourcePages) + ".",
		}
	}

	return first, last, nil
}

// textFailure classifies a pdftotext failure. Its usual reason for refusing a
// document is that the document needs a password, which this package checks
// for first — so anything left is a structural problem.
func textFailure(err error) error {
	if engineErr, ok := errors.AsType[*EngineError](err); ok {
		return &DamagedError{Detail: engineErr.Error()}
	}

	return err
}

// MarkdownRequest is one Markdown conversion.
type MarkdownRequest struct {
	Dir   string
	Input string
	// Output is the Markdown file.
	Output string
	// Pages are the 1-based pages to read. Empty reads all.
	Pages []int
	// SourcePages is the input's page count.
	SourcePages int
	// PageRules writes a horizontal rule between pages. A report reads
	// better with them and an article reads better without.
	PageRules bool
}

// MarkdownResult says what the heuristics found, so the tool can tell the
// user what it actually recognised rather than let them assume.
type MarkdownResult struct {
	// Headings is how many headings were recognised, by level: index 0 is
	// level 1.
	Headings [markdownLevels]int
	// ListItems is how many list items were recognised.
	ListItems int
	// Paragraphs is how many paragraphs were written.
	Paragraphs int
}

// markdownLevels is how deep the heading heuristics go. Three levels is what
// a numbered section hierarchy reaches in practice, and a fourth would be
// guessing.
const markdownLevels = 3

// ToMarkdown converts a document to Markdown.
//
// It is explicitly a set of heuristics over extracted text, not a layout
// reconstruction, and the limit is in the nature of the input rather than in
// the effort: `pdftotext` reports characters and their positions, not
// semantics. A PDF does not record that a line is a heading. It records that
// some glyphs are 18 points and some are 11, and even that is not available
// here — so the only signals left are the shape of the text itself.
//
// What is recognised, in the order it is tried:
//
//   - A numbered section line, "3 Title", "3.1 Title", "3.1.2 Title", which
//     becomes a heading of the matching depth. This is the one signal that is
//     nearly always right, because a document that numbers its sections has
//     already told us the hierarchy.
//   - A short line in capitals, which becomes a level-two heading.
//   - A short line with no terminal punctuation between two blank lines,
//     which becomes a level-three heading.
//   - A bullet or a numbered item, which becomes a list item.
//   - Everything else, joined into paragraphs, because `-layout` hard-wraps
//     and a paragraph broken at the page's width is not a paragraph.
//
// What is not recognised, and what the tool page therefore says plainly: a
// document whose headings are marked only by font size comes out as
// paragraphs, and a table comes out as the spaced text `-layout` produced
// rather than as a Markdown table.
func (e *Engines) ToMarkdown(ctx context.Context, req MarkdownRequest) (MarkdownResult, error) {
	raw, err := os.CreateTemp(req.Dir, "text-*.txt")
	if err != nil {
		return MarkdownResult{}, fmt.Errorf("pdf: create the text file: %w", err)
	}

	name := raw.Name()

	defer func() {
		_ = raw.Close()
		_ = os.Remove(name)
	}()

	err = e.ExtractText(ctx, TextRequest{
		Dir:         req.Dir,
		Input:       req.Input,
		Output:      name,
		Layout:      true,
		Pages:       req.Pages,
		SourcePages: req.SourcePages,
	})
	if err != nil {
		return MarkdownResult{}, err
	}

	source, err := os.Open(name)
	if err != nil {
		return MarkdownResult{}, fmt.Errorf("pdf: reopen the text file: %w", err)
	}

	defer func() { _ = source.Close() }()

	out, err := os.Create(req.Output)
	if err != nil {
		return MarkdownResult{}, fmt.Errorf("pdf: create the Markdown file: %w", err)
	}

	result, convErr := renderMarkdown(source, out, req.PageRules)

	if err := out.Close(); err != nil && convErr == nil {
		return MarkdownResult{}, fmt.Errorf("pdf: close the Markdown file: %w", err)
	}

	return result, convErr
}

// maxTextLine caps one line of extracted text.
//
// A document whose text has no line breaks would otherwise make the scanner
// hold the whole document in one token, which is exactly the buffering this
// package is shaped to avoid.
const maxTextLine = 1 << 20

var (
	// numberedHeading matches "3 Title", "3.1 Title" and "3.1.2 Title". The
	// title must start with a letter, so "3.5 million people" — a sentence
	// beginning with a number — is not read as a heading.
	numberedHeading = regexp.MustCompile(`^(\d+(?:\.\d+){0,2})\.?\s+(\p{Lu}.*)$`)
	// bulletItem matches the characters PDF producers use for bullets,
	// including the lowercase o that Word's Courier bullets become.
	bulletItem = regexp.MustCompile(`^\s*([-*\x{2022}\x{2023}\x{25aa}\x{25cf}\x{00b7}]|o)\s+(\S.*)$`)
	// numberedItem matches "1. text" and "1) text".
	numberedItem = regexp.MustCompile(`^\s*(\d{1,3})[.)]\s+(\S.*)$`)
)

// maxHeadingRunes is how long a line may be and still be read as a heading.
// A heading is a label; a line this long is a sentence.
const maxHeadingRunes = 80

// renderMarkdown is the heuristic pass, line by line.
//
// It streams: one line in, one line out, with at most the current paragraph
// held. A 2000-page document costs a paragraph of memory.
func renderMarkdown(r io.Reader, w io.Writer, pageRules bool) (MarkdownResult, error) {
	in := bufio.NewScanner(r)
	in.Buffer(make([]byte, 0, 64<<10), maxTextLine)

	out := bufio.NewWriter(w)
	doc := &markdownDoc{out: out, pageRules: pageRules}

	for in.Scan() {
		doc.line(in.Text())
	}

	if err := in.Err(); err != nil {
		return MarkdownResult{}, &DamagedError{Detail: "could not read the extracted text: " + err.Error()}
	}

	doc.flush()

	if err := out.Flush(); err != nil {
		return MarkdownResult{}, fmt.Errorf("pdf: write the Markdown: %w", err)
	}

	return doc.result, doc.err
}

// markdownDoc holds the state the heuristics need: the paragraph being
// gathered and whether the previous line was blank.
type markdownDoc struct {
	out       *bufio.Writer
	pageRules bool
	paragraph []string
	lastBlank bool
	// inList suppresses the blank line between consecutive list items, which
	// would otherwise make Markdown render each one as its own list.
	inList bool
	result MarkdownResult
	err    error
}

// line handles one line of extracted text.
func (d *markdownDoc) line(raw string) {
	if strings.ContainsRune(raw, pageBreak) {
		d.pageBoundary(raw)

		return
	}

	trimmed := strings.TrimRight(raw, " \t")

	if strings.TrimSpace(trimmed) == "" {
		d.flush()
		d.lastBlank = true

		return
	}

	switch {
	case d.heading(trimmed):
	case d.listItem(trimmed):
	default:
		d.paragraph = append(d.paragraph, strings.TrimSpace(trimmed))
		d.lastBlank = false
	}
}

// pageBoundary ends the page, writing a rule when one was asked for.
//
// The text around the form feed is kept: pdftotext puts the feed on a line of
// its own at a page break, but a document whose last line ends exactly at the
// boundary can have content on the same line.
func (d *markdownDoc) pageBoundary(raw string) {
	before, after, _ := strings.Cut(raw, string(pageBreak))

	if strings.TrimSpace(before) != "" {
		d.line(before)
	}

	d.flush()

	if d.pageRules {
		d.write("---\n\n")
	}

	d.lastBlank = true

	if strings.TrimSpace(after) != "" {
		d.line(after)
	}
}

// heading recognises a heading and writes it, reporting whether it did.
func (d *markdownDoc) heading(line string) bool {
	trimmed := strings.TrimSpace(line)

	if match := numberedHeading.FindStringSubmatch(trimmed); match != nil {
		level := strings.Count(match[1], ".") + 1
		if level <= markdownLevels && len([]rune(trimmed)) <= maxHeadingRunes {
			d.writeHeading(level, match[1]+" "+match[2])

			return true
		}
	}

	// A heading needs a blank line before it, which is what separates a
	// capitalised heading from a capitalised line inside a paragraph.
	if !d.lastBlank || len(d.paragraph) > 0 {
		return false
	}

	if len([]rune(trimmed)) > maxHeadingRunes {
		return false
	}

	if isAllCaps(trimmed) {
		d.writeHeading(2, titleFromCaps(trimmed))

		return true
	}

	if looksLikeLabel(trimmed) {
		d.writeHeading(markdownLevels, trimmed)

		return true
	}

	return false
}

// writeHeading emits a heading and counts it.
func (d *markdownDoc) writeHeading(level int, text string) {
	d.flush()
	d.write(strings.Repeat("#", level) + " " + escapeInline(text) + "\n\n")

	d.result.Headings[level-1]++
	d.lastBlank = true
	d.inList = false
}

// listItem recognises a bullet or a numbered item.
func (d *markdownDoc) listItem(line string) bool {
	if match := bulletItem.FindStringSubmatch(line); match != nil {
		d.writeListItem("- ", match[2])

		return true
	}

	if match := numberedItem.FindStringSubmatch(line); match != nil {
		d.writeListItem(match[1]+". ", match[2])

		return true
	}

	return false
}

// writeListItem emits one item and counts it.
func (d *markdownDoc) writeListItem(marker, text string) {
	d.flush()

	if !d.inList {
		d.inList = true
	}

	d.write(marker + escapeInline(strings.TrimSpace(text)) + "\n")
	d.result.ListItems++
	d.lastBlank = false
}

// flush writes the gathered paragraph, if any.
//
// The lines are joined with spaces rather than kept as they came, because
// `-layout` wraps a paragraph at the page's width: keeping the breaks would
// produce Markdown that re-wraps at the reader's width and reads as poetry.
func (d *markdownDoc) flush() {
	if d.inList {
		d.write("\n")
		d.inList = false
	}

	if len(d.paragraph) == 0 {
		return
	}

	d.write(escapeInline(strings.Join(d.paragraph, " ")) + "\n\n")
	d.result.Paragraphs++
	d.paragraph = d.paragraph[:0]
}

// write appends to the output, remembering the first failure.
func (d *markdownDoc) write(s string) {
	if d.err != nil {
		return
	}

	if _, err := d.out.WriteString(s); err != nil {
		d.err = fmt.Errorf("pdf: write the Markdown: %w", err)
	}
}

// escapeInline escapes the characters that would turn extracted text into
// Markdown markup.
//
// Only the line-leading ones are escaped, and only at the start: escaping
// every asterisk in the body would mangle ordinary prose that happens to use
// them, while a line beginning with "#" or "|" would become a heading or a
// table. The text is a document's content, not markup, so it must not be
// able to become markup by accident.
func escapeInline(text string) string {
	if text == "" {
		return text
	}

	switch text[0] {
	case '#', '>', '|', '`', '-', '+', '=':
		return "\\" + text
	default:
		return text
	}
}

// isAllCaps reports whether a line is capitals and punctuation, with at least
// two letters, and does not end like a sentence.
func isAllCaps(line string) bool {
	letters := 0

	for _, r := range line {
		if unicode.IsLetter(r) {
			if unicode.IsLower(r) {
				return false
			}

			letters++
		}
	}

	return letters >= 2 && !strings.HasSuffix(line, ".")
}

// titleFromCaps leaves a capitalised heading as it is.
//
// Lowercasing it would be a guess about words that are acronyms and words
// that are not, and "Pdf Handling" is worse than "PDF HANDLING".
func titleFromCaps(line string) string { return line }

// looksLikeLabel reports whether a line reads as a title rather than a
// sentence: it starts with a capital, does not end with sentence punctuation,
// and is short.
func looksLikeLabel(line string) bool {
	runes := []rune(line)
	if len(runes) < 2 || len(runes) > maxLabelRunes {
		return false
	}

	if !unicode.IsUpper(runes[0]) {
		return false
	}

	switch runes[len(runes)-1] {
	case '.', ',', ';', ':', '!', '?':
		return false
	}

	// A sentence has verbs and therefore spaces; so does a title. What
	// separates them here is length, and a line with no spaces at all is
	// more likely a stray word than a heading.
	return strings.Contains(line, " ")
}

// maxLabelRunes is how long an unnumbered, unpunctuated line may be and
// still read as a heading rather than as the first line of a paragraph.
const maxLabelRunes = 60
