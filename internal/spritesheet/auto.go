package spritesheet

import (
	"context"
	"image"
	"image/color"
	"slices"
)

// DetectResult is what automatic detection found.
type DetectResult struct {
	// Frames are the detected frames, in reading order.
	Frames []Frame
	// Dropped is how many components were discarded as smaller than
	// [SliceOptions.MinFrame]. It is reported rather than swallowed: somebody
	// whose sheet has specks in it needs to know they were ignored.
	Dropped int
}

// Detect finds each frame's bounding box from the sheet's own content.
//
// The background is transparency, or the colour named in opts. A sheet with no
// transparency and no named colour is refused with a [*BackgroundError]: there
// is no signal in it saying what is a frame and what is a gap, and a guess
// would produce frames that are wrong in a way nobody can see.
func Detect(ctx context.Context, img image.Image, opts SliceOptions) (DetectResult, error) {
	if img == nil {
		return DetectResult{}, ErrNilImage
	}

	eff, err := opts.normalize()
	if err != nil {
		return DetectResult{}, err
	}

	b := img.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return DetectResult{}, ErrEmptyImage
	}

	mask, err := backgroundMask(ctx, img, eff)
	if err != nil {
		return DetectResult{}, err
	}

	boxes, dropped := components(ctx, mask, b, eff.MinFrame)
	if err := ctx.Err(); err != nil {
		return DetectResult{}, err
	}

	if len(boxes) > MaxFrames {
		return DetectResult{}, &FrameCountError{Count: len(boxes), Limit: MaxFrames}
	}

	boxes = sortReadingOrder(boxes)

	frames := make([]Frame, 0, len(boxes))
	for i, box := range boxes {
		frames = append(frames, Frame{
			Name:   frameName(eff.Prefix, i),
			Rect:   box,
			Source: image.Pt(box.Dx(), box.Dy()),
		})
	}

	return DetectResult{Frames: frames, Dropped: dropped}, nil
}

// mask is a per-pixel "this is content" bitmap over the sheet's bounds.
type mask struct {
	set  []bool
	w, h int
}

func (m *mask) at(x, y int) bool { return m.set[y*m.w+x] }

// backgroundMask marks every pixel that is not background.
//
// With no colour named the test is alpha, and a sheet in which every pixel is
// opaque is refused: there is nothing to separate frames by. With a colour
// named the test is distance from that colour on each channel, which also
// covers a sheet whose background is a flat magenta or a flat black.
func backgroundMask(ctx context.Context, img image.Image, opts SliceOptions) (*mask, error) {
	b := img.Bounds()
	m := &mask{set: make([]bool, b.Dx()*b.Dy()), w: b.Dx(), h: b.Dy()}
	src := newPixels(img)

	var content int

	for y := range b.Dy() {
		if y%ctxCheckRows == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}

		for x := range b.Dx() {
			c := src.at(b.Min.X+x, b.Min.Y+y)

			if !isBackground(c, opts) {
				m.set[y*m.w+x] = true
				content++
			}
		}
	}

	// Every pixel is content, so there is no background to separate frames by.
	// With a colour named this means the colour is simply not in the sheet,
	// which is the caller's mistake to hear about; without one it means the
	// sheet is opaque, which is the refusal BackgroundError describes.
	if content == b.Dx()*b.Dy() {
		return nil, &BackgroundError{}
	}

	return m, nil
}

// isBackground reports whether c counts as empty.
func isBackground(c color.NRGBA, opts SliceOptions) bool {
	if opts.Background == nil {
		return c.A == 0
	}

	// A transparent pixel is background whatever colour was named: a sheet can
	// have both a transparent border and a flat background inside it.
	if c.A == 0 {
		return true
	}

	bg := *opts.Background
	t := opts.BackgroundTolerance

	return within(c.R, bg.R, t) && within(c.G, bg.G, t) &&
		within(c.B, bg.B, t) && within(c.A, bg.A, t)
}

// within reports whether a and b are no more than t apart.
func within(a, b uint8, t int) bool {
	d := int(a) - int(b)
	if d < 0 {
		d = -d
	}

	return d <= t
}

// ctxCheckRows is how many rows are processed between cancellation checks.
// Checking every row would cost an atomic load per row for no benefit.
const ctxCheckRows = 64

// components returns the bounding box of every 8-connected run of content,
// and how many were dropped for being smaller than minPixels.
//
// The labelling is a two-pass union-find over horizontal runs rather than a
// flood fill per pixel: one pass per row, each run joined to the runs it
// touches in the row above. 8-connected means a run also touches the runs one
// pixel to either side of it in that row, which is what keeps a diagonal
// staircase — the commonest shape in pixel art — as one frame instead of a
// cloud of fragments.
func components(ctx context.Context, m *mask, bounds image.Rectangle, minPixels int) ([]image.Rectangle, int) {
	uf := newUnionFind()

	// prev holds the previous row's runs, so each row is compared with one row
	// rather than the whole image.
	var prev, cur []run

	for y := range m.h {
		if y%ctxCheckRows == 0 {
			if err := ctx.Err(); err != nil {
				return nil, 0
			}
		}

		cur = cur[:0]

		for x := 0; x < m.w; x++ {
			if !m.at(x, y) {
				continue
			}

			start := x
			for x < m.w && m.at(x, y) {
				x++
			}

			r := run{x0: start, x1: x, label: uf.make(start, x, y)}

			// 8-connected: a run in this row joins any run in the previous row
			// whose columns come within one of it.
			for i := range prev {
				if prev[i].x1 >= r.x0 && prev[i].x0 <= r.x1 {
					uf.union(r.label, prev[i].label)
				}
			}

			cur = append(cur, r)
		}

		prev = append(prev[:0], cur...)
	}

	return uf.boxes(bounds, minPixels)
}

// run is one horizontal stretch of content in a row, x0 inclusive and x1
// exclusive.
type run struct {
	x0, x1 int
	label  int
}

// unionFind joins runs into components and accumulates each one's bounding box
// and pixel count as it goes, so no second pass over the image is needed.
type unionFind struct {
	parent []int
	box    []image.Rectangle
	pixels []int
}

func newUnionFind() *unionFind { return &unionFind{} }

// make records a new run as its own component and returns its label.
func (u *unionFind) make(x0, x1, y int) int {
	label := len(u.parent)
	u.parent = append(u.parent, label)
	u.box = append(u.box, image.Rect(x0, y, x1, y+1))
	u.pixels = append(u.pixels, x1-x0)

	return label
}

// find returns a's root, flattening the path as it goes.
func (u *unionFind) find(a int) int {
	for u.parent[a] != a {
		u.parent[a] = u.parent[u.parent[a]]
		a = u.parent[a]
	}

	return a
}

// union joins a and b, merging the smaller root into the larger so the label
// that survives is deterministic — it is always the earlier one, which is what
// makes the component order reproducible.
func (u *unionFind) union(a, b int) {
	ra, rb := u.find(a), u.find(b)
	if ra == rb {
		return
	}

	// The lower label wins, so the surviving label is the one whose first run
	// appeared first in the image.
	if rb < ra {
		ra, rb = rb, ra
	}

	u.parent[rb] = ra
	u.box[ra] = u.box[ra].Union(u.box[rb])
	u.pixels[ra] += u.pixels[rb]
}

// boxes returns one bounding box per component, with the ones under minPixels
// dropped, and how many were dropped.
func (u *unionFind) boxes(bounds image.Rectangle, minPixels int) ([]image.Rectangle, int) {
	var (
		out     []image.Rectangle
		dropped int
	)

	for label := range u.parent {
		if u.find(label) != label {
			continue // not a root, so its box is already merged into one
		}

		if u.pixels[label] < minPixels {
			dropped++

			continue
		}

		// The mask is indexed from zero; the sheet may not be.
		out = append(out, u.box[label].Add(bounds.Min))
	}

	return out, dropped
}

// sortReadingOrder sorts boxes the way a person reads a sheet: into rows, then
// left to right within a row.
//
// Sorting by y and then x would be wrong, and wrong in a way that only shows
// up on real sheets: two frames side by side in the same row rarely start on
// the same scanline, so the taller one would sort before its neighbour's whole
// row. Rows are therefore formed by vertical overlap first, and only then is
// each row sorted by x.
func sortReadingOrder(boxes []image.Rectangle) []image.Rectangle {
	if len(boxes) < 2 {
		return boxes
	}

	// A stable starting order, so the row grouping below is deterministic
	// whatever order the labeller produced.
	slices.SortFunc(boxes, func(a, b image.Rectangle) int {
		if a.Min.Y != b.Min.Y {
			return a.Min.Y - b.Min.Y
		}

		return a.Min.X - b.Min.X
	})

	var (
		rows   [][]image.Rectangle
		cur    []image.Rectangle
		rowEnd int
	)

	for i, box := range boxes {
		// A box starting before the current row's lowest bottom edge overlaps
		// it vertically, so it belongs to the same row.
		if i == 0 || box.Min.Y < rowEnd {
			cur = append(cur, box)
			if i == 0 || box.Max.Y > rowEnd {
				rowEnd = box.Max.Y
			}

			continue
		}

		rows = append(rows, cur)
		cur = []image.Rectangle{box}
		rowEnd = box.Max.Y
	}

	if len(cur) > 0 {
		rows = append(rows, cur)
	}

	out := make([]image.Rectangle, 0, len(boxes))

	for _, row := range rows {
		slices.SortFunc(row, func(a, b image.Rectangle) int { return a.Min.X - b.Min.X })
		out = append(out, row...)
	}

	return out
}
