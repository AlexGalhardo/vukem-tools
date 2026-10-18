package pixelsvg

import (
	"bufio"
	"context"
	"fmt"
	"image/color"
	"strconv"
)

// hexDigits and decDigits are indexed directly by a byte value, which writes a
// digit without converting an integer to a byte.
const (
	hexDigits = "0123456789abcdef"
	decDigits = "0123456789"
)

// unitRectID is the id of the one-pixel rectangle that [ModeSquare] reuses.
const unitRectID = "p"

// contourBatch is how many region paths are built before they are written out.
// Tracing is parallel but the output order is fixed, so the paths of a batch
// have to be held; batching bounds that to a few hundred kilobytes instead of
// a whole colour's worth of geometry.
const contourBatch = 256

// svgWriter writes SVG fragments into a [bufio.Writer], keeping the first
// error it hits so the calling code reads as straight-line writes and the
// error is checked once at the end.
type svgWriter struct {
	bw      *bufio.Writer
	scratch []byte
	err     error
}

// str writes a literal.
func (s *svgWriter) str(v string) {
	if s.err == nil {
		_, s.err = s.bw.WriteString(v)
	}
}

// ch writes a single byte.
func (s *svgWriter) ch(c byte) {
	if s.err == nil {
		s.err = s.bw.WriteByte(c)
	}
}

// num writes a decimal integer.
func (s *svgWriter) num(v int) {
	if s.err == nil {
		s.scratch = strconv.AppendInt(s.scratch[:0], int64(v), 10)
		_, s.err = s.bw.Write(s.scratch)
	}
}

// hex writes a byte as two lowercase hex digits.
func (s *svgWriter) hex(v uint8) {
	s.ch(hexDigits[v>>4])
	s.ch(hexDigits[v&0x0f])
}

// opacity writes an alpha value as a fixed three-decimal fraction, which keeps
// the output byte-identical across platforms without going through a float.
// It is only called for an alpha below 255, so the result is always below 1.
func (s *svgWriter) opacity(a uint8) {
	m := (int(a)*1000 + 127) / 255

	s.str("0.")
	s.ch(decDigits[m/100])
	s.ch(decDigits[(m/10)%10])
	s.ch(decDigits[m%10])
}

// encoder streams one image out as SVG.
type encoder struct {
	w        *svgWriter
	g        *grid
	opts     Options
	elements int
}

// encode writes the whole document.
func (e *encoder) encode(ctx context.Context) error {
	e.header()

	runs, err := extractRuns(ctx, e.g, e.opts)
	if err != nil {
		return err
	}

	switch e.opts.Mode {
	case ModeSquare:
		err = e.squares(ctx, runs)
	case ModeRect:
		err = e.rects(ctx, runs)
	case ModeContour:
		err = e.contours(ctx, runs)
	default:
		err = &OptionError{
			Field: "Mode",
			Value: strconv.Quote(string(e.opts.Mode)),
			Want:  `one of "square", "rect" or "contour"`,
		}
	}

	if err != nil {
		return err
	}

	e.str("</svg>\n")

	if e.w.err != nil {
		return fmt.Errorf("pixelsvg: write svg: %w", e.w.err)
	}

	return nil
}

// str forwards to the writer, so the encoder body stays readable.
func (e *encoder) str(v string) { e.w.str(v) }

// header writes the root element and, in [ModeSquare], the unit rectangle that
// every pixel references.
func (e *encoder) header() {
	e.str(`<svg xmlns="http://www.w3.org/2000/svg" width="`)
	e.w.num(e.g.w * e.opts.Scale)
	e.str(`" height="`)
	e.w.num(e.g.h * e.opts.Scale)
	e.str(`" viewBox="0 0 `)
	e.w.num(e.g.w)
	e.w.ch(' ')
	e.w.num(e.g.h)
	e.str(`" shape-rendering="crispEdges">` + "\n")

	if e.opts.Mode == ModeSquare && len(e.g.pal) > 0 {
		e.str(`<defs><rect id="` + unitRectID + `" width="1" height="1"/></defs>` + "\n")
	}
}

// openGroup starts a colour group. The fill is written once per colour rather
// than once per shape, which is most of the size difference between this
// output and one shape with its own fill attribute.
func (e *encoder) openGroup(c color.NRGBA) {
	e.str(`<g fill="#`)
	e.w.hex(c.R)
	e.w.hex(c.G)
	e.w.hex(c.B)

	if c.A != 0xff {
		e.str(`" fill-opacity="`)
		e.w.opacity(c.A)
	}

	e.str(`">`)
}

// squares writes one reference to the unit rectangle per drawn pixel.
//
// A <use> is shorter than a <rect> with its own width and height, which is
// where the only real repetition in this mode is, so this is the one place the
// element is worth the indirection. In the other modes the shapes differ in
// size and a <use> with a transform would be longer than the shape it replaces.
func (e *encoder) squares(ctx context.Context, runs [][]run) error {
	for ci, c := range e.g.pal {
		if err := ctx.Err(); err != nil {
			return err
		}

		if len(runs[ci]) == 0 {
			continue
		}

		e.openGroup(c)

		for _, r := range runs[ci] {
			for x := r.x0; x < r.x1; x++ {
				e.str(`<use href="#` + unitRectID + `" x="`)
				e.w.num(x)
				e.str(`" y="`)
				e.w.num(r.y)
				e.str(`"/>`)
				e.elements++
			}
		}

		e.str("</g>\n")
	}

	return nil
}

// rects writes the maximal rectangles of every colour.
func (e *encoder) rects(ctx context.Context, runs [][]run) error {
	for ci, c := range e.g.pal {
		if err := ctx.Err(); err != nil {
			return err
		}

		if len(runs[ci]) == 0 {
			continue
		}

		e.openGroup(c)

		for _, r := range mergeRuns(runs[ci]) {
			e.str(`<rect x="`)
			e.w.num(r.x)
			e.str(`" y="`)
			e.w.num(r.y)
			e.str(`" width="`)
			e.w.num(r.w)
			e.str(`" height="`)
			e.w.num(r.h)
			e.str(`"/>`)
			e.elements++
		}

		e.str("</g>\n")
	}

	return nil
}

// contours writes one path per 4-connected region.
//
// Regions are independent of each other, so they are traced by a bounded pool
// in batches. Each worker only fills the slot of the region it was handed and
// the batch is written in region order, so the output does not depend on which
// worker finished first.
func (e *encoder) contours(ctx context.Context, runs [][]run) error {
	buf := make([]string, contourBatch)

	for ci, c := range e.g.pal {
		if err := ctx.Err(); err != nil {
			return err
		}

		if len(runs[ci]) == 0 {
			continue
		}

		regionOf, count := labelRuns(runs[ci])
		regions := groupByRegion(runs[ci], regionOf, count)

		e.openGroup(c)

		for start := 0; start < count; start += contourBatch {
			if err := ctx.Err(); err != nil {
				return err
			}

			end := min(start+contourBatch, count)

			forEachIndex(end-start, e.opts.Workers, func(i int) {
				buf[i] = regionPath(regions[start+i], e.opts.SimplifyTolerance)
			})

			for _, d := range buf[:end-start] {
				if d == "" {
					continue
				}

				e.str(`<path d="`)
				e.str(d)
				e.str(`"/>`)
				e.elements++
			}
		}

		e.str("</g>\n")
	}

	return nil
}
