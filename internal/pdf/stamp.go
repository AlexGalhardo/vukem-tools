package pdf

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Position is where on the page a stamp goes.
type Position string

// The nine positions, named as a person describes them.
const (
	TopLeft      Position = "top-left"
	TopCenter    Position = "top-center"
	TopRight     Position = "top-right"
	MiddleLeft   Position = "middle-left"
	Center       Position = "center"
	MiddleRight  Position = "middle-right"
	BottomLeft   Position = "bottom-left"
	BottomCenter Position = "bottom-center"
	BottomRight  Position = "bottom-right"
)

// positions is the closed set, reading order.
var positions = []Position{
	TopLeft, TopCenter, TopRight,
	MiddleLeft, Center, MiddleRight,
	BottomLeft, BottomCenter, BottomRight,
}

// Valid reports whether p is a position this package knows.
func (p Position) Valid() bool { return slices.Contains(positions, p) }

// Positions returns every position a client may name.
func Positions() []string {
	out := make([]string, 0, len(positions))
	for _, p := range positions {
		out = append(out, string(p))
	}

	return out
}

// stampMargin is how far a stamp sits from the page's edge, in points. Half
// an inch, which is inside every printer's printable area.
const stampMargin = 36

// place returns the origin of a text run of this width and size at p.
//
// The vertical placement uses the font size as a stand-in for the ascender,
// which is close enough for a stamp and avoids embedding a second metric
// table for a number nobody can see being slightly off.
func (p Position) place(page Box, width, size float64) (float64, float64) {
	x := page.LLX + stampMargin

	switch p {
	case TopCenter, Center, BottomCenter:
		x = page.LLX + (page.Width()-width)/2
	case TopRight, MiddleRight, BottomRight:
		x = page.URX - stampMargin - width
	case TopLeft, MiddleLeft, BottomLeft:
	}

	y := page.LLY + stampMargin

	switch p {
	case TopLeft, TopCenter, TopRight:
		y = page.URY - stampMargin - size
	case MiddleLeft, Center, MiddleRight:
		y = page.LLY + (page.Height()-size)/2
	case BottomLeft, BottomCenter, BottomRight:
	}

	return x, y
}

// MaxStampTextBytes caps stamped text. A watermark is a word or a sentence;
// anything longer would not fit on the page it is stamped onto.
const MaxStampTextBytes = 200

// The font-size bounds a stamp may use.
const (
	MinStampSize     = 4
	MaxStampSize     = 144
	DefaultStampSize = 10
)

// WatermarkRequest is one watermark.
type WatermarkRequest struct {
	Dir    string
	Input  string
	Output string
	// Text is the watermark. It is limited to unaccented Latin characters,
	// for the reason metrics.go gives.
	Text string
	// Pages are the 1-based pages to stamp. Empty stamps every page.
	Pages []int
	// Position defaults to the centre of the page, which is what a
	// watermark usually is.
	Position Position
	// Size is the font size in points. Zero picks a size from the page's
	// width, so a watermark on A4 and one on A3 look the same.
	Size float64
	// Rotate is the text's angle in degrees anticlockwise. 45 is the
	// diagonal a watermark is normally set at.
	Rotate float64
	// Opacity is 0 to 1. The default is light enough to read the document
	// through and dark enough to see.
	Opacity float64
	// Gray is the ink, 0 black to 1 white.
	Gray float64
	// Bold uses Helvetica-Bold.
	Bold bool
}

// PageNumberRequest is one run of page numbers.
type PageNumberRequest struct {
	Dir    string
	Input  string
	Output string
	// Format is the label, with "{n}" for the page number and "{total}" for
	// the page count: "Page {n} of {total}". A format with neither is a
	// footer rather than a page number, which is allowed.
	Format string
	// Pages are the 1-based pages to number. Empty numbers every page.
	Pages []int
	// StartAt is the number printed on the first numbered page, so a
	// document with a cover can start at 1 on page 2.
	StartAt int
	// Position defaults to the bottom centre.
	Position Position
	// Size is the font size in points.
	Size float64
}

// defaultWatermarkOpacity is light enough to read the page through.
const defaultWatermarkOpacity = 0.18

// Watermark stamps text across pages.
//
// The stamp is built as a PDF of its own — one page per page of the document,
// with that page's own size — and then composited by qpdf's --overlay. That
// is the decision worth explaining, because the obvious alternative is
// Ghostscript, and Ghostscript would re-render the entire document to draw
// one line of text on it: every image re-encoded, every font re-subsetted,
// the outline rebuilt. An overlay changes each page's content stream and
// nothing else.
//
// It also makes the memory cost independent of the page count, which ROADMAP
// S6.6 asks for and benchmarks. The stamp document is written streaming, one
// page at a time, and qpdf composites page by page — so a 500-page watermark
// costs what a 5-page watermark costs plus the file on disk.
func (e *Engines) Watermark(ctx context.Context, req WatermarkRequest) (int, error) {
	if err := validateStampText("text", req.Text, req.Size); err != nil {
		return 0, err
	}

	if req.Position == "" {
		req.Position = Center
	}

	if !req.Position.Valid() {
		return 0, &OptionError{
			Field:  "position",
			Reason: "The position is one of: " + strings.Join(Positions(), ", ") + ".",
		}
	}

	if req.Opacity == 0 {
		req.Opacity = defaultWatermarkOpacity
	}

	if req.Opacity < 0 || req.Opacity > 1 {
		return 0, &OptionError{Field: "opacity", Reason: "The opacity must be between 0 and 1."}
	}

	boxes, pages, err := e.stampTargets(ctx, req.Dir, req.Input, req.Pages)
	if err != nil {
		return 0, err
	}

	font := Helvetica
	if req.Bold {
		font = HelveticaBold
	}

	specs := make([]PageSpec, 0, len(boxes))

	for i, page := range boxes {
		if !slices.Contains(pages, i+1) {
			// An empty page of the right size, so the overlay stays aligned
			// page for page: qpdf matches the overlay's page 1 to the
			// document's page 1, so a skipped page needs a blank, not a
			// missing page.
			specs = append(specs, PageSpec{Box: page})

			continue
		}

		size := req.Size
		if size == 0 {
			size = autoWatermarkSize(page, req.Text)
		}

		width := TextWidth(font, req.Text, size)
		x, y := req.Position.place(page, width, size)

		if req.Rotate != 0 {
			// A rotated run grows past the width its glyphs occupy, so it is
			// placed from the page's centre rather than from an edge: a
			// diagonal watermark positioned by its unrotated width would
			// hang off the page.
			x, y = rotatedOrigin(page, width, size, req.Rotate)
		}

		specs = append(specs, PageSpec{Box: page, Text: []TextStamp{{
			Text:    req.Text,
			Font:    font,
			Size:    size,
			X:       x,
			Y:       y,
			Gray:    req.Gray,
			Opacity: req.Opacity,
			Rotate:  req.Rotate,
		}}})
	}

	return len(pages), e.overlay(ctx, req.Dir, req.Input, req.Output, specs)
}

// autoWatermarkSize picks a size that spans about three quarters of the
// page's width, which is what a watermark looks like when nobody chose a
// size.
func autoWatermarkSize(page Box, text string) float64 {
	measured := TextWidth(Helvetica, text, 100)
	if measured <= 0 {
		return MaxStampSize / 2
	}

	size := page.Width() * 0.75 / measured * 100

	return min(max(size, MinStampSize), MaxStampSize)
}

// rotatedOrigin centres a rotated run of text on the page.
func rotatedOrigin(page Box, width, size, degrees float64) (float64, float64) {
	cx, cy := page.LLX+page.Width()/2, page.LLY+page.Height()/2
	cos, sin := cosDeg(degrees), sinDeg(degrees)

	// Step back along the baseline from the centre by half the run, then down
	// by half the font size, both in the rotated frame.
	return cx - (width/2)*cos + (size/2)*sin, cy - (width/2)*sin - (size/2)*cos
}

// NumberPages stamps page numbers.
//
// The numbers go on through the same overlay as the watermark, for the same
// reason, and the format is a template rather than a choice from a list
// because "Page 3 of 12", "- 3 -" and "3" are all things people want and none
// of them is worth an option of its own.
func (e *Engines) NumberPages(ctx context.Context, req PageNumberRequest) (int, error) {
	if req.Format == "" {
		req.Format = "{n}"
	}

	if req.Position == "" {
		req.Position = BottomCenter
	}

	if !req.Position.Valid() {
		return 0, &OptionError{
			Field:  "position",
			Reason: "The position is one of: " + strings.Join(Positions(), ", ") + ".",
		}
	}

	if req.Size == 0 {
		req.Size = DefaultStampSize
	}

	if req.StartAt == 0 {
		req.StartAt = 1
	}

	if req.StartAt < 0 {
		return 0, &OptionError{Field: "start_at", Reason: "The first number must be 1 or more."}
	}

	// The template is checked rather than the rendered label, so a format
	// this package cannot draw is refused before any page is numbered.
	if err := validateStampText("format", req.Format, req.Size); err != nil {
		return 0, err
	}

	boxes, pages, err := e.stampTargets(ctx, req.Dir, req.Input, req.Pages)
	if err != nil {
		return 0, err
	}

	specs := make([]PageSpec, 0, len(boxes))
	number := req.StartAt

	for i, page := range boxes {
		if !slices.Contains(pages, i+1) {
			specs = append(specs, PageSpec{Box: page})

			continue
		}

		label := renderPageLabel(req.Format, number, len(pages))
		number++

		width := TextWidth(Helvetica, label, req.Size)
		x, y := req.Position.place(page, width, req.Size)

		specs = append(specs, PageSpec{Box: page, Text: []TextStamp{{
			Text: label,
			Font: Helvetica,
			Size: req.Size,
			X:    x,
			Y:    y,
		}}})
	}

	return len(pages), e.overlay(ctx, req.Dir, req.Input, req.Output, specs)
}

// renderPageLabel fills the template in.
func renderPageLabel(format string, number, total int) string {
	label := strings.ReplaceAll(format, "{n}", strconv.Itoa(number))

	return strings.ReplaceAll(label, "{total}", strconv.Itoa(total))
}

// validateStampText refuses text or a size this package cannot draw.
func validateStampText(field, text string, size float64) error {
	if strings.TrimSpace(text) == "" {
		return &OptionError{Field: field, Reason: "Enter the text to stamp."}
	}

	if len(text) > MaxStampTextBytes {
		return &OptionError{
			Field:  field,
			Reason: "The text may be at most " + strconv.Itoa(MaxStampTextBytes) + " characters.",
		}
	}

	if size != 0 && (size < MinStampSize || size > MaxStampSize) {
		return &OptionError{
			Field: field,
			Reason: "The size must be between " + strconv.Itoa(MinStampSize) + " and " +
				strconv.Itoa(MaxStampSize) + " points.",
		}
	}

	return checkWinAnsi(field, text)
}

// stampTargets returns every page's box and the pages to stamp.
func (e *Engines) stampTargets(ctx context.Context, dir, input string, want []int) ([]Box, []int, error) {
	doc, err := e.Inspect(ctx, dir, input)
	if err != nil {
		return nil, nil, err
	}

	pages := want
	if len(pages) == 0 {
		pages = sequence(1, doc.Count)
	}

	if highest(pages) > doc.Count {
		return nil, nil, &PageRangeError{
			Input: QPDFRange(pages),
			Reason: "That page range names page " + strconv.Itoa(highest(pages)) +
				", but this document has " + strconv.Itoa(doc.Count) + ".",
		}
	}

	graph, err := e.readGraph(ctx, dir, input)
	if err != nil {
		return nil, nil, err
	}

	boxes := make([]Box, 0, doc.Count)

	for page := 1; page <= doc.Count; page++ {
		id, _ := doc.Object(page)

		node, ok := graph.nodes[id]
		if !ok {
			return nil, nil, &DamagedError{Detail: "page " + strconv.Itoa(page) + " is not in the page tree"}
		}

		box, err := graph.boxOf(id, node)
		if err != nil {
			return nil, nil, err
		}

		boxes = append(boxes, box)
	}

	return boxes, pages, nil
}

// overlay writes the stamp document and composites it onto the input.
//
// --overlay rather than --underlay: a watermark that a page's own white
// background hides is a watermark nobody sees, and most PDF producers do fill
// the page. The cost is that the stamp sits over the text rather than under
// it, which is what the opacity is for.
func (e *Engines) overlay(ctx context.Context, dir, input, output string, specs []PageSpec) error {
	stamp := filepath.Join(dir, "stamp.pdf")

	file, err := os.Create(stamp)
	if err != nil {
		return fmt.Errorf("pdf: create the stamp: %w", err)
	}

	writeErr := Write(file, specs)

	if err := file.Close(); err != nil && writeErr == nil {
		writeErr = fmt.Errorf("pdf: close the stamp: %w", err)
	}

	if writeErr != nil {
		removeAll([]string{stamp})

		return writeErr
	}

	defer func() { _ = os.Remove(stamp) }()

	// No --from and no --repeat: qpdf's default maps the overlay's pages to
	// the destination's in order, and the stamp has exactly one page per
	// page of the document, so the mapping is one to one. --repeat would
	// only matter for a stamp shorter than the document, which this one
	// never is.
	return e.qpdf(ctx, dir, []string{input, "--overlay", stamp, "--", output})
}
