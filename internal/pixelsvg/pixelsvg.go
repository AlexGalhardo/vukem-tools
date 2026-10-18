// Package pixelsvg converts raster pixel art into SVG without moving a pixel edge.
//
// A conversion runs as five stages: decode into a non-premultiplied RGBA
// buffer, extract the palette and reduce the image to one palette index per
// pixel, optionally quantise and tolerance-merge the palette, extract geometry
// in one of three modes, and stream the SVG out through a [bufio.Writer].
//
// # Modes
//
// [ModeSquare] emits one unit shape per drawn pixel. Faithful and large; it is
// what the prior art pixel2svg produces.
//
// [ModeRect] is the default. Pixels are first merged into maximal horizontal
// runs, then runs with identical extent in consecutive rows are merged
// vertically into maximal rectangles. The result covers exactly the same
// pixels as [ModeSquare] with far fewer elements.
//
// [ModeContour] labels 4-connected regions of one colour and traces the
// boundary of each as a closed polygon on the pixel lattice, with holes as
// counter-wound subpaths of the same path. With [Options.SimplifyTolerance]
// left at zero the polygons are pixel-exact; above zero Douglas-Peucker cuts
// corners and the output is lossy by at most that many pixels.
//
// # Determinism
//
// The same input and the same Options always produce byte-identical output,
// whatever the worker count. Every ordering in the pipeline is total: the
// palette is sorted by R, G, B then A; geometry is emitted in palette order
// and within a colour in row-major order; regions are numbered by the
// first-appearance order of their topmost-leftmost run.
//
// # Limits
//
// An image wider or taller than [MaxDimension] is refused with a
// [*DimensionError]. Encoded input longer than [MaxInputBytes] is refused with
// an [*InputSizeError]. An image with more distinct colours than
// [Options.MaxColors] is refused with a [*ColorCountError] unless a quantiser
// is selected.
//
// # Licensing
//
// This package is an independent implementation written from the algorithm
// description in docs/web-research.md § 7. No code was taken, translated or
// paraphrased from pixels2svg (Python, GPL-3.0) or pixel2svg-rs (Rust,
// GPL-3.0); both are credited as prior art.
package pixelsvg

import (
	"bufio"
	"context"
	"fmt"
	"image"
	"io"
	"strconv"
)

// Mode selects how pixels are turned into SVG shapes.
type Mode string

// The supported output modes.
const (
	// ModeSquare emits one unit shape per drawn pixel.
	ModeSquare Mode = "square"
	// ModeRect merges pixels into maximal rectangles. It is the default.
	ModeRect Mode = "rect"
	// ModeContour traces one path around each 4-connected region.
	ModeContour Mode = "contour"
)

// AlphaMode selects what happens to pixels that are not fully opaque.
type AlphaMode string

// The supported alpha handling modes.
const (
	// AlphaOpacity keeps partial alpha: a fully transparent pixel is dropped
	// and any other pixel keeps its alpha, written once per colour group as
	// fill-opacity. It is the default.
	AlphaOpacity AlphaMode = "opacity"
	// AlphaDrop drops every pixel that is not fully opaque, so no
	// fill-opacity is ever written.
	AlphaDrop AlphaMode = "drop"
)

// Quantizer selects the colour reduction applied when an image holds more
// distinct colours than [Options.MaxColors] allows.
type Quantizer string

// The supported quantisers.
const (
	// QuantizerMedianCut reduces the palette with median cut. It is the default.
	QuantizerMedianCut Quantizer = "median-cut"
	// QuantizerNone does not reduce colours, so an image over the cap is refused.
	QuantizerNone Quantizer = "none"
)

// The hard limits of a conversion.
const (
	// MaxDimension is the largest accepted width and height, in pixels. The
	// cap is deliberately below the 4K suggested by the original brief: at
	// 2048 the RGBA buffer is 16 MiB and the index grid 16 MiB, which fits a
	// small container, and the SVG a 4K input would produce is already beyond
	// what a browser renders comfortably.
	MaxDimension = 2048
	// ColorCap is the largest accepted value for [Options.MaxColors].
	ColorCap = 256
	// MaxInputBytes bounds how much encoded image data is read, so a
	// malformed or hostile file cannot be streamed indefinitely.
	MaxInputBytes int64 = 64 << 20
)

// writeBufferSize is the size of the bufio.Writer the SVG is streamed into.
const writeBufferSize = 64 << 10

// Options configures a conversion. The zero value is valid and selects
// [ModeRect], [AlphaOpacity], [QuantizerMedianCut], a palette capped at
// [ColorCap], no tolerance merging, no simplification, scale 1 and the worker
// count reported by [Workers].
type Options struct {
	// Mode selects the geometry mode. Empty means [ModeRect].
	Mode Mode
	// Alpha selects the alpha handling. Empty means [AlphaOpacity].
	Alpha AlphaMode
	// Quantizer selects the colour reduction used when the image holds more
	// than MaxColors distinct colours. Empty means [QuantizerMedianCut].
	Quantizer Quantizer
	// MaxColors caps the palette. Zero means [ColorCap]; -1 means no cap and
	// no quantisation. Any other value above [ColorCap] is refused.
	MaxColors int
	// Tolerance merges colours closer together than this distance, measured
	// by [ColorDistance] on the same 0 to 255 scale. Zero disables the pass.
	Tolerance int
	// Scale multiplies the width and height attributes of the root element
	// only. The viewBox and every coordinate stay in pixel units, so the
	// geometry is identical at any scale. Zero means 1.
	Scale int
	// SimplifyTolerance is the Douglas-Peucker epsilon, in pixels, applied to
	// contours in [ModeContour]. Zero keeps the contours pixel-exact and only
	// collapses collinear vertices. It is ignored in the other modes.
	SimplifyTolerance float64
	// Workers bounds how many goroutines process row bands and regions. Zero
	// means the value reported by [Workers].
	Workers int
}

// Result reports what a conversion produced.
type Result struct {
	// Width and Height are the pixel dimensions of the source image.
	Width, Height int
	// Colors is the number of colour groups written, after quantisation and
	// tolerance merging.
	Colors int
	// Elements is the number of shape elements written.
	Elements int
	// Bytes is the length of the SVG written to the destination.
	Bytes int64
}

// normalize returns opts with every zero field replaced by its default, and an
// error for any value the conversion cannot accept.
func (o Options) normalize() (Options, error) {
	n := o

	if n.Mode == "" {
		n.Mode = ModeRect
	}

	switch n.Mode {
	case ModeSquare, ModeRect, ModeContour:
	default:
		return Options{}, &OptionError{
			Field: "Mode",
			Value: strconv.Quote(string(n.Mode)),
			Want:  `one of "square", "rect" or "contour"`,
		}
	}

	if n.Alpha == "" {
		n.Alpha = AlphaOpacity
	}

	switch n.Alpha {
	case AlphaOpacity, AlphaDrop:
	default:
		return Options{}, &OptionError{
			Field: "Alpha",
			Value: strconv.Quote(string(n.Alpha)),
			Want:  `one of "opacity" or "drop"`,
		}
	}

	if n.Quantizer == "" {
		n.Quantizer = QuantizerMedianCut
	}

	switch n.Quantizer {
	case QuantizerMedianCut, QuantizerNone:
	default:
		return Options{}, &OptionError{
			Field: "Quantizer",
			Value: strconv.Quote(string(n.Quantizer)),
			Want:  `one of "median-cut" or "none"`,
		}
	}

	switch {
	case n.MaxColors == 0:
		n.MaxColors = ColorCap
	case n.MaxColors < -1, n.MaxColors > ColorCap:
		return Options{}, &OptionError{
			Field: "MaxColors",
			Value: strconv.Itoa(n.MaxColors),
			Want:  "-1 for no cap, or 1 to " + strconv.Itoa(ColorCap),
		}
	}

	if n.Tolerance < 0 || n.Tolerance > 255 {
		return Options{}, &OptionError{
			Field: "Tolerance",
			Value: strconv.Itoa(n.Tolerance),
			Want:  "0 to 255",
		}
	}

	switch {
	case n.Scale == 0:
		n.Scale = 1
	case n.Scale < 0:
		return Options{}, &OptionError{Field: "Scale", Value: strconv.Itoa(n.Scale), Want: "1 or more"}
	}

	if n.SimplifyTolerance < 0 {
		return Options{}, &OptionError{
			Field: "SimplifyTolerance",
			Value: strconv.FormatFloat(n.SimplifyTolerance, 'g', -1, 64),
			Want:  "0 or more",
		}
	}

	switch {
	case n.Workers == 0:
		n.Workers = Workers()
	case n.Workers < 0:
		return Options{}, &OptionError{Field: "Workers", Value: strconv.Itoa(n.Workers), Want: "1 or more"}
	}

	return n, nil
}

// Convert decodes the image in r and streams its SVG to w.
//
// The dimensions are checked from the image header before any pixel data is
// decoded, so an oversized input is refused without allocating for it.
func Convert(ctx context.Context, w io.Writer, r io.Reader, opts Options) (Result, error) {
	if _, err := opts.normalize(); err != nil {
		return Result{}, err
	}

	img, err := decode(r)
	if err != nil {
		return Result{}, err
	}

	return ConvertImage(ctx, w, img, opts)
}

// ConvertImage streams the SVG for an already decoded image to w.
func ConvertImage(ctx context.Context, w io.Writer, src image.Image, opts Options) (Result, error) {
	if src == nil {
		return Result{}, ErrNilImage
	}

	eff, err := opts.normalize()
	if err != nil {
		return Result{}, err
	}

	b := src.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return Result{}, ErrEmptyImage
	}

	if b.Dx() > MaxDimension || b.Dy() > MaxDimension {
		return Result{}, &DimensionError{Width: b.Dx(), Height: b.Dy(), Limit: MaxDimension}
	}

	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	g, err := newGrid(ctx, toNRGBA(src), eff)
	if err != nil {
		return Result{}, err
	}

	cw := &countingWriter{w: w}
	bw := bufio.NewWriterSize(cw, writeBufferSize)
	enc := &encoder{w: &svgWriter{bw: bw}, g: g, opts: eff}

	if err := enc.encode(ctx); err != nil {
		return Result{}, err
	}

	if err := bw.Flush(); err != nil {
		return Result{}, fmt.Errorf("pixelsvg: write svg: %w", err)
	}

	return Result{
		Width:    g.w,
		Height:   g.h,
		Colors:   len(g.pal),
		Elements: enc.elements,
		Bytes:    cw.n,
	}, nil
}

// countingWriter counts the bytes written through it.
type countingWriter struct {
	w io.Writer
	n int64
}

// Write implements [io.Writer].
func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)

	return n, err
}
