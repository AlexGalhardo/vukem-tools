package pdf

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
)

// Box is a PDF rectangle in points, lower-left to upper-right.
type Box struct {
	LLX, LLY, URX, URY float64
}

// Width is the box's width in points.
func (b Box) Width() float64 { return b.URX - b.LLX }

// Height is the box's height in points.
func (b Box) Height() float64 { return b.URY - b.LLY }

// Empty reports whether the box has no area, which is how an absent box
// reads: a page with no /CropBox decodes to the zero Box.
func (b Box) Empty() bool { return b.Width() <= 0 || b.Height() <= 0 }

// The page sizes a client may name, in points. A4 is the default because most
// of the world prints on it; Letter is here because a quarter of the world
// does not.
var pageSizes = map[string]Box{
	"a4":     {0, 0, 595.276, 841.89},
	"a3":     {0, 0, 841.89, 1190.551},
	"a5":     {0, 0, 419.528, 595.276},
	"letter": {0, 0, 612, 792},
	"legal":  {0, 0, 612, 1008},
}

// PageSize returns the named page size, and whether it is one this package
// knows. Names are lowercase: "a4", "letter".
func PageSize(name string) (Box, bool) {
	box, ok := pageSizes[strings.ToLower(strings.TrimSpace(name))]
	return box, ok
}

// PageSizeNames returns every page size a client may name, for the API
// documentation and the interface.
func PageSizeNames() []string { return []string{"a4", "a3", "a5", "letter", "legal"} }

// TextStamp is one run of text drawn on a page.
type TextStamp struct {
	Text string
	Font Font
	Size float64
	// X and Y are the text's origin in points from the page's lower-left
	// corner: the left edge of the first glyph and its baseline.
	X, Y float64
	// Gray is the ink, 0 black to 1 white. A watermark is usually light grey
	// rather than transparent, because transparency costs an ExtGState and
	// grey does not.
	Gray float64
	// Opacity is the alpha, 0 invisible to 1 opaque. Below 1 it adds an
	// ExtGState to the page.
	Opacity float64
	// Rotate is the text's rotation in degrees anticlockwise about its
	// origin, for a diagonal watermark.
	Rotate float64
}

// ImageStamp is one JPEG drawn on a page, placed by its box.
//
// The path is a JPEG on local disk and it is copied into the document
// untouched, as a DCTDecode stream: the bytes are the ones the user uploaded,
// re-encoded zero times. That is both faster and better than decoding to
// pixels, and it is what keeps a 50-page image-to-PDF conversion from needing
// 50 pages' worth of pixels in memory.
type ImageStamp struct {
	// JPEGPath is a baseline or progressive JPEG on local disk.
	JPEGPath string
	// PixelWidth and PixelHeight are the image's own dimensions, read from
	// its header by the caller.
	PixelWidth, PixelHeight int
	// Box is where it goes on the page, in points.
	Box Box
	// Gray marks a greyscale JPEG, which needs /DeviceGray rather than
	// /DeviceRGB: naming the wrong colour space produces a picture of noise.
	Gray bool
}

// PageSpec is one page of a document this package writes.
type PageSpec struct {
	// Box is the page's MediaBox.
	Box Box
	// Text and Images are drawn in that order, so text lands on top of a
	// picture rather than under it.
	Text   []TextStamp
	Images []ImageStamp
	// Links are internal link annotations on this page.
	Links []Link
}

// Link is a clickable rectangle that jumps to another page in the same
// document.
//
// Only internal links: an external URL would mean putting a string somebody
// supplied into a document other people open, and this writer exists to make
// stamps and fixtures, neither of which needs one.
type Link struct {
	// Box is the clickable area in points.
	Box Box
	// TargetPage is the 1-based page it jumps to.
	TargetPage int
}

// OutlineEntry is one top-level bookmark.
//
// Flat rather than nested: an outline is here so that a test can prove an
// operation kept one or dropped it, and a tree would add the complexity of
// /Count and /Parent chains without making that proof any stronger.
type OutlineEntry struct {
	Title string
	// TargetPage is the 1-based page it points at.
	TargetPage int
}

// DocumentSpec is a whole document, with the parts a page list cannot carry.
type DocumentSpec struct {
	Pages []PageSpec
	// Outline is the table of contents. Empty writes no /Outlines at all,
	// which is what most documents have.
	Outline []OutlineEntry
}

// maxWriterPages caps a document this package writes.
//
// A stamp document has one page per page of the document being stamped, so
// this is the same bound as MaxPages, kept separate so that raising one does
// not silently raise the other.
const maxWriterPages = 10_000

// Write writes a PDF of pages to w.
//
// It is a single forward pass: each object is written once, its byte offset
// recorded, and the cross-reference table written at the end. Nothing is
// buffered beyond the current content stream, and an image's bytes are copied
// straight from disk — so writing a 200-page document of photographs costs
// one page's text in memory, not 200 photographs.
//
// What it does not do is as deliberate as what it does. There is no embedded
// font, no transparency group, no outline and no form field: this writer
// exists to produce stamps and image pages, and a general-purpose PDF writer
// would be a far larger thing to get right than either of those needs.
func Write(w io.Writer, pages []PageSpec) error {
	return WriteDocument(w, DocumentSpec{Pages: pages})
}

// WriteDocument writes a PDF with an outline as well as pages.
func WriteDocument(w io.Writer, spec DocumentSpec) error {
	switch {
	case len(spec.Pages) == 0:
		return errors.New("pdf: a document needs at least one page")
	case len(spec.Pages) > maxWriterPages:
		return &TooManyPagesError{Pages: len(spec.Pages), Limit: maxWriterPages}
	}

	for _, entry := range spec.Outline {
		if entry.TargetPage < 1 || entry.TargetPage > len(spec.Pages) {
			return &PageRangeError{
				Input:  strconv.Itoa(entry.TargetPage),
				Reason: "A bookmark must point at a page the document has.",
			}
		}
	}

	doc := &writer{out: &offsetWriter{w: bufio.NewWriterSize(w, writeBuffer)}}

	return doc.document(spec)
}

// writeBuffer is the output buffer. A PDF is written as many small pieces —
// a dictionary, a number, a newline — and an unbuffered writer would make a
// syscall of each one.
const writeBuffer = 64 << 10

// writer builds one document.
type writer struct {
	out *offsetWriter
	// offsets[n] is where object n+1 starts. Object numbers are 1-based in a
	// PDF, which is why the slice is offset by one rather than padded.
	offsets []int64
	err     error
}

// object numbers that are fixed because they are written first and referred
// to from every page.
const (
	catalogObj   = 1
	pagesObj     = 2
	fontObj      = 3
	boldFontObj  = 4
	firstFreeObj = 5
)

// document writes the whole file.
func (d *writer) document(spec DocumentSpec) error {
	pages := spec.Pages

	// The header's second line is a comment of high bytes, which is what
	// tells a transfer that treats the file as text that it must not: every
	// PDF has one, and a file without it survives an FTP in ASCII mode as a
	// broken document.
	d.raw("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n")

	// The page objects are numbered before anything is written, because the
	// /Pages dictionary refers to all of them and is written first.
	layout := planObjects(spec)

	d.begin(catalogObj)
	d.raw("<< /Type /Catalog /Pages " + ref(pagesObj))

	if len(spec.Outline) > 0 {
		d.raw(" /Outlines " + ref(layout.outline) + " /PageMode /UseOutlines")
	}

	d.raw(" >>")
	d.end()

	d.begin(pagesObj)
	d.raw("<< /Type /Pages /Count " + strconv.Itoa(len(pages)) + " /Kids [")

	for i := range pages {
		d.raw(" " + ref(layout.page[i]))
	}

	d.raw(" ] >>")
	d.end()

	d.standardFont(fontObj, Helvetica)
	d.standardFont(boldFontObj, HelveticaBold)

	for i := range pages {
		d.pageObjects(pages[i], layout, i)
	}

	d.outline(spec.Outline, layout)

	return d.trailer()
}

// outline writes the /Outlines dictionary and one item per bookmark.
//
// The items form a doubly linked list through /Next and /Prev, which is how
// a PDF outline is shaped: a reader walks /First to /Last. /Count is the
// number of visible descendants, positive because the list is open.
func (d *writer) outline(entries []OutlineEntry, layout objectLayout) {
	if len(entries) == 0 {
		return
	}

	d.begin(layout.outline)
	d.raw("<< /Type /Outlines /Count " + strconv.Itoa(len(entries)) +
		" /First " + ref(layout.outlineItems[0]) +
		" /Last " + ref(layout.outlineItems[len(entries)-1]) + " >>")
	d.end()

	for i, entry := range entries {
		d.begin(layout.outlineItems[i])
		d.raw("<< /Title " + pdfString(entry.Title) +
			" /Parent " + ref(layout.outline) +
			" /Dest [" + ref(layout.page[entry.TargetPage-1]) + " /XYZ null null null]")

		if i > 0 {
			d.raw(" /Prev " + ref(layout.outlineItems[i-1]))
		}

		if i+1 < len(entries) {
			d.raw(" /Next " + ref(layout.outlineItems[i+1]))
		}

		d.raw(" >>")
		d.end()
	}
}

// objectLayout is which object number each piece of each page got.
type objectLayout struct {
	page     []int
	contents []int
	// images[i] holds the XObject number of each image on page i.
	images [][]int
	// alpha[i] holds the ExtGState number for each distinct opacity on page
	// i, in the order the stamps use them.
	alpha []map[int]int
	// outline is the /Outlines dictionary, and outlineItems one object per
	// bookmark. Both are zero when the document has no outline.
	outline      int
	outlineItems []int
	next         int
}

// planObjects assigns an object number to every object the document will
// hold, before any of them is written.
func planObjects(spec DocumentSpec) objectLayout {
	pages := spec.Pages

	layout := objectLayout{
		page:     make([]int, len(pages)),
		contents: make([]int, len(pages)),
		images:   make([][]int, len(pages)),
		alpha:    make([]map[int]int, len(pages)),
		next:     firstFreeObj,
	}

	if len(spec.Outline) > 0 {
		layout.outline = layout.next
		layout.next++

		layout.outlineItems = make([]int, len(spec.Outline))
		for i := range spec.Outline {
			layout.outlineItems[i] = layout.next
			layout.next++
		}
	}

	for i := range pages {
		layout.page[i] = layout.next
		layout.next++
		layout.contents[i] = layout.next
		layout.next++

		layout.images[i] = make([]int, len(pages[i].Images))
		for j := range pages[i].Images {
			layout.images[i][j] = layout.next
			layout.next++
		}

		layout.alpha[i] = make(map[int]int)

		for _, t := range pages[i].Text {
			key := alphaKey(t.Opacity)
			if key == opaqueAlpha {
				continue
			}

			if _, seen := layout.alpha[i][key]; !seen {
				layout.alpha[i][key] = layout.next
				layout.next++
			}
		}
	}

	return layout
}

// alphaKey quantises an opacity to a hundredth, so two stamps asking for the
// same translucency share one graphics state instead of adding an object
// each.
func alphaKey(opacity float64) int {
	if opacity <= 0 || opacity >= 1 {
		return opaqueAlpha
	}

	return int(opacity*100 + 0.5)
}

// opaqueAlpha is the key for "no transparency needed".
const opaqueAlpha = 100

// pageObjects writes one page, its content stream, its images and its
// graphics states.
func (d *writer) pageObjects(page PageSpec, layout objectLayout, i int) {
	content := contentStream(page, layout, i)

	d.begin(layout.page[i])
	d.raw("<< /Type /Page /Parent " + ref(pagesObj) +
		" /MediaBox [" + box(page.Box) + "] /Contents " + ref(layout.contents[i]) +
		" /Resources << /Font << /F1 " + ref(fontObj) + " /F2 " + ref(boldFontObj) + " >>")

	if len(page.Images) > 0 {
		d.raw(" /XObject <<")

		for j := range page.Images {
			d.raw(" /Im" + strconv.Itoa(j) + " " + ref(layout.images[i][j]))
		}

		d.raw(" >>")
	}

	if len(layout.alpha[i]) > 0 {
		d.raw(" /ExtGState <<")

		for key, obj := range layout.alpha[i] {
			d.raw(" /GS" + strconv.Itoa(key) + " " + ref(obj))
		}

		d.raw(" >>")
	}

	d.raw(" >>")

	// The link annotations are written inline rather than as separate
	// objects. A /Annots array of dictionaries is legal, and it keeps the
	// object count down, which matters only because it keeps this writer
	// small.
	if len(page.Links) > 0 {
		d.raw(" /Annots [")

		for _, link := range page.Links {
			if link.TargetPage < 1 || link.TargetPage > len(layout.page) {
				continue
			}

			d.raw(" << /Type /Annot /Subtype /Link /Border [0 0 0] /Rect [" + box(link.Box) +
				"] /Dest [" + ref(layout.page[link.TargetPage-1]) + " /XYZ null null null] >>")
		}

		d.raw(" ]")
	}

	d.raw(" >>")
	d.end()

	d.begin(layout.contents[i])
	d.raw("<< /Length " + strconv.Itoa(len(content)) + " >>\nstream\n")
	d.raw(content)
	d.raw("\nendstream")
	d.end()

	for j := range page.Images {
		d.image(layout.images[i][j], page.Images[j])
	}

	for key, obj := range layout.alpha[i] {
		d.begin(obj)
		d.raw("<< /Type /ExtGState /ca " + num(float64(key)/100) + " /CA " + num(float64(key)/100) + " >>")
		d.end()
	}
}

// standardFont writes one of the base-14 fonts.
//
// WinAnsiEncoding rather than the font's built-in encoding, so that the byte
// values written in a string literal mean what ASCII says they mean. The
// built-in encoding of Helvetica is close enough to ASCII to look right and
// different enough in the punctuation to be wrong.
func (d *writer) standardFont(obj int, f Font) {
	d.begin(obj)
	d.raw("<< /Type /Font /Subtype /Type1 /BaseFont /" + string(f) + " /Encoding /WinAnsiEncoding >>")
	d.end()
}

// image writes one JPEG as a DCTDecode XObject, copying the file's bytes.
func (d *writer) image(obj int, img ImageStamp) {
	if d.err != nil {
		return
	}

	info, err := os.Stat(img.JPEGPath)
	if err != nil {
		d.err = fmt.Errorf("pdf: measure %s: %w", img.JPEGPath, err)

		return
	}

	space := "/DeviceRGB"
	if img.Gray {
		space = "/DeviceGray"
	}

	d.begin(obj)
	d.raw("<< /Type /XObject /Subtype /Image" +
		" /Width " + strconv.Itoa(img.PixelWidth) +
		" /Height " + strconv.Itoa(img.PixelHeight) +
		" /ColorSpace " + space +
		" /BitsPerComponent 8 /Filter /DCTDecode" +
		" /Length " + strconv.FormatInt(info.Size(), 10) + " >>\nstream\n")

	src, err := os.Open(img.JPEGPath)
	if err != nil {
		d.err = fmt.Errorf("pdf: open %s: %w", img.JPEGPath, err)

		return
	}

	defer func() { _ = src.Close() }()

	// Copied rather than read: a 20-megapixel JPEG is tens of megabytes, and
	// holding one per page is exactly the thing this writer is shaped to
	// avoid.
	if _, err := io.Copy(d.out, src); err != nil {
		d.err = fmt.Errorf("pdf: copy %s: %w", img.JPEGPath, err)

		return
	}

	d.raw("\nendstream")
	d.end()
}

// contentStream builds one page's drawing operators.
//
// It is built in memory because a stream needs its /Length before its bytes,
// and a page of stamps is a few hundred bytes. An image contributes six
// numbers and a name to it, not its pixels, so the size stays bounded by the
// number of stamps rather than by what they draw.
func contentStream(page PageSpec, layout objectLayout, i int) string {
	var b strings.Builder

	for j := range page.Images {
		img := page.Images[j]
		if img.Box.Empty() {
			continue
		}

		// q/Q around each one, so a transform cannot leak into the next
		// drawing operation.
		b.WriteString("q\n" + num(img.Box.Width()) + " 0 0 " + num(img.Box.Height()) + " " +
			num(img.Box.LLX) + " " + num(img.Box.LLY) + " cm\n/Im" + strconv.Itoa(j) + " Do\nQ\n")
	}

	for _, t := range page.Text {
		if t.Text == "" || t.Size <= 0 {
			continue
		}

		b.WriteString("q\n")

		if key := alphaKey(t.Opacity); key != opaqueAlpha {
			if _, ok := layout.alpha[i][key]; ok {
				b.WriteString("/GS" + strconv.Itoa(key) + " gs\n")
			}
		}

		b.WriteString(num(clampGray(t.Gray)) + " g\nBT\n/" + fontResource(t.Font) + " " + num(t.Size) + " Tf\n")
		b.WriteString(textMatrix(t))
		b.WriteString(pdfString(t.Text) + " Tj\nET\nQ\n")
	}

	return b.String()
}

// textMatrix places and rotates one text run.
//
// Tm rather than Td even when there is no rotation, because one operator that
// always says the same thing is easier to be sure of than two that differ by
// a case.
func textMatrix(t TextStamp) string {
	if t.Rotate == 0 {
		return "1 0 0 1 " + num(t.X) + " " + num(t.Y) + " Tm\n"
	}

	cos, sin := cosDeg(t.Rotate), sinDeg(t.Rotate)

	return num(cos) + " " + num(sin) + " " + num(-sin) + " " + num(cos) + " " +
		num(t.X) + " " + num(t.Y) + " Tm\n"
}

// cosDeg and sinDeg are the rotation matrix's two entries, in degrees
// because that is the unit the API takes: nobody asks for a watermark at 0.7
// radians.
func cosDeg(deg float64) float64 { return math.Cos(deg * math.Pi / 180) }

func sinDeg(deg float64) float64 { return math.Sin(deg * math.Pi / 180) }

// fontResource is the resource name a font is reached by on the page.
func fontResource(f Font) string {
	if f == HelveticaBold {
		return "F2"
	}

	return "F1"
}

// clampGray keeps the ink inside the 0 to 1 range a PDF operator accepts.
func clampGray(g float64) float64 { return min(max(g, 0), 1) }

// pdfString renders text as a PDF literal string.
//
// The three characters that end or nest a literal string are escaped. This is
// the one place in the package where user text becomes part of a file's
// syntax, so it is the one place an escape being wrong would let a watermark
// rewrite the document around it — which is why the text is also restricted
// to printable ASCII before it gets here.
func pdfString(text string) string {
	var b strings.Builder

	b.Grow(len(text) + 2)
	b.WriteByte('(')

	for i := range len(text) {
		switch c := text[i]; c {
		case '(', ')', '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}

	b.WriteByte(')')

	return b.String()
}

// begin records where an object starts and writes its header.
func (d *writer) begin(obj int) {
	for len(d.offsets) < obj {
		d.offsets = append(d.offsets, 0)
	}

	d.offsets[obj-1] = d.out.n
	d.raw(strconv.Itoa(obj) + " 0 obj\n")
}

// end closes an object.
func (d *writer) end() { d.raw("\nendobj\n") }

// raw writes a piece of the file, remembering the first failure.
//
// Errors are collected rather than returned at each call because writing a
// PDF is forty small writes in a row, and threading an error through each one
// would bury the structure of the document in error handling. The first
// failure stops every later write and is returned by trailer.
func (d *writer) raw(s string) {
	if d.err != nil {
		return
	}

	if _, err := io.WriteString(d.out, s); err != nil {
		d.err = fmt.Errorf("pdf: write: %w", err)
	}
}

// trailer writes the cross-reference table and the trailer, and returns the
// first error anything hit.
func (d *writer) trailer() error {
	if d.err != nil {
		return d.err
	}

	start := d.out.n
	count := len(d.offsets) + 1

	d.raw("xref\n0 " + strconv.Itoa(count) + "\n")
	// Object 0 is always the head of the free list, in the exact spelling the
	// specification gives: twenty bytes, including the trailing space.
	d.raw("0000000000 65535 f \n")

	for _, offset := range d.offsets {
		d.raw(fmt.Sprintf("%010d 00000 n \n", offset))
	}

	d.raw("trailer\n<< /Size " + strconv.Itoa(count) + " /Root " + ref(catalogObj) + " >>\n")
	d.raw("startxref\n" + strconv.FormatInt(start, 10) + "\n%%EOF\n")

	if d.err != nil {
		return d.err
	}

	return d.out.flush()
}

// ref renders an indirect reference.
func ref(obj int) string { return strconv.Itoa(obj) + " 0 R" }

// box renders a rectangle's four numbers.
func box(b Box) string {
	return num(b.LLX) + " " + num(b.LLY) + " " + num(b.URX) + " " + num(b.URY)
}

// num renders a coordinate.
//
// Fixed to three decimals and then trimmed, rather than %g: %g switches to
// exponent notation for small numbers, and a PDF number may not have an
// exponent — a page whose offset came out as "1e-05" is a page no reader
// renders.
func num(f float64) string {
	s := strconv.FormatFloat(f, 'f', 3, 64)
	s = strings.TrimRight(s, "0")
	s = strings.TrimSuffix(s, ".")

	if s == "" || s == "-" {
		return "0"
	}

	return s
}

// offsetWriter counts the bytes written, which is what the cross-reference
// table is made of.
type offsetWriter struct {
	w *bufio.Writer
	n int64
}

func (o *offsetWriter) Write(p []byte) (int, error) {
	n, err := o.w.Write(p)
	o.n += int64(n)

	return n, err
}

func (o *offsetWriter) flush() error {
	if err := o.w.Flush(); err != nil {
		return fmt.Errorf("pdf: flush: %w", err)
	}

	return nil
}
