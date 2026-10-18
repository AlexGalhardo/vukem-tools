package spritesheet

import (
	"context"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io"
	"math"
	"slices"
	"strconv"
)

// Layout selects how frames are arranged on the sheet.
type Layout string

// The supported layouts.
const (
	// LayoutGrid gives every frame an identical cell, sized to the largest
	// frame, in a near-square arrangement. It is the default, and it is the
	// layout Godot 4 and Unity import with no metadata file at all — which is
	// why it is the default rather than the tighter one.
	LayoutGrid Layout = "grid"
	// LayoutCompact packs frames of differing sizes with a skyline
	// bottom-left heuristic, for an atlas whose frames are not uniform.
	LayoutCompact Layout = "compact"
)

// Sheet-building limits.
const (
	// DefaultMaxSheetSize is the largest sheet produced unless the caller
	// asks for more. 4096 is what every GPU in use handles as a single
	// texture.
	DefaultMaxSheetSize = 4096
	// MaxExtrude bounds the edge repeat. More than a few pixels is not
	// bleed prevention, it is a mistake.
	MaxExtrude = 8
)

// BuildOptions configures building a sheet. The zero value is valid and means
// a grid layout, no gutters and a 4096 pixel cap.
type BuildOptions struct {
	// Layout selects the arrangement. Empty means [LayoutGrid].
	Layout Layout
	// Margin is the transparent gutter at the sheet's edges.
	Margin int
	// Padding is the transparent gutter between two frames. It stops a
	// neighbour's colour bleeding in when the GPU samples half a texel
	// outside the quad.
	Padding int
	// Extrude repeats each frame's own edge pixels outward into the padding.
	// Padding alone leaves transparent black, which bleeds as a dark halo on
	// a tileset at non-integer zoom; this is the fix for that.
	Extrude int
	// MaxSize caps the sheet's width and height. Zero means
	// [DefaultMaxSheetSize].
	MaxSize int
	// POT rounds the sheet up to a power of two on both axes.
	POT bool
}

// BuildResult reports what a build produced.
type BuildResult struct {
	// Sheet is the finished sheet's size.
	Sheet image.Point
	// Frames is how many frames were packed.
	Frames int
	// Atlas describes where each frame landed, for a metadata writer.
	Atlas []Frame
	// FrameArea is the sum of the frames' own areas, in pixels.
	FrameArea int64
	// Efficiency is FrameArea as a fraction of the sheet's area, from 0 to 1.
	// It is reported so the packing can be measured rather than trusted.
	Efficiency float64
}

// normalize fills in the defaults and refuses a value that cannot be used.
func (o BuildOptions) normalize() (BuildOptions, error) {
	n := o

	if n.Layout == "" {
		n.Layout = LayoutGrid
	}

	switch n.Layout {
	case LayoutGrid, LayoutCompact:
	default:
		return BuildOptions{}, &OptionError{
			Field: "layout",
			Value: strconv.Quote(string(n.Layout)),
			Want:  `either "grid" or "compact"`,
		}
	}

	if n.MaxSize == 0 {
		n.MaxSize = DefaultMaxSheetSize
	}

	switch {
	case n.MaxSize < 1 || n.MaxSize > MaxDimension:
		return BuildOptions{}, &OptionError{
			Field: "max_size",
			Value: strconv.Itoa(n.MaxSize),
			Want:  "1 to " + strconv.Itoa(MaxDimension),
		}
	case n.Margin < 0 || n.Margin > MaxGutter:
		return BuildOptions{}, &OptionError{
			Field: "margin",
			Value: strconv.Itoa(n.Margin),
			Want:  "0 to " + strconv.Itoa(MaxGutter),
		}
	case n.Padding < 0 || n.Padding > MaxGutter:
		return BuildOptions{}, &OptionError{
			Field: "padding",
			Value: strconv.Itoa(n.Padding),
			Want:  "0 to " + strconv.Itoa(MaxGutter),
		}
	case n.Extrude < 0 || n.Extrude > MaxExtrude:
		return BuildOptions{}, &OptionError{
			Field: "extrude",
			Value: strconv.Itoa(n.Extrude),
			Want:  "0 to " + strconv.Itoa(MaxExtrude),
		}
	case n.Extrude > n.Padding+n.Margin:
		// Extruding further than the gutter would write a frame's edge
		// pixels over its neighbour.
		return BuildOptions{}, &OptionError{
			Field: "extrude",
			Value: strconv.Itoa(n.Extrude),
			Want: "no more than the padding plus the margin (" +
				strconv.Itoa(n.Padding+n.Margin) + "), or the repeat would overwrite a neighbour",
		}
	}

	return n, nil
}

// Build packs the frames in a ZIP archive into one sheet and writes it to dst
// as a PNG.
//
// It takes an [io.ReaderAt] and an [io.Writer] and no path of any kind. That
// is deliberate and load-bearing: an archive's entry names are
// attacker-controlled strings, and the defence against a name like
// "../../etc/passwd" is that there is no filesystem operation for it to reach.
// Only each entry's basename, with its extension dropped, becomes a frame
// name. Adding a path or directory parameter here would undo that.
//
// The work is two passes over the same archive, which is why the input is
// random-access rather than a stream. Pass one reads only each frame's image
// header, so the layout — and every refusal — is decided for the cost of
// headers rather than of pixels. Pass two allocates the canvas once and
// decodes one frame at a time into it. The result is a peak memory of one
// canvas plus one frame, whatever the frame count.
func Build(
	ctx context.Context,
	dst io.Writer,
	src io.ReaderAt,
	size int64,
	opts BuildOptions,
) (BuildResult, error) {
	eff, err := opts.normalize()
	if err != nil {
		return BuildResult{}, err
	}

	members, err := readMembers(ctx, src, size, defaultPrefix)
	if err != nil {
		return BuildResult{}, err
	}

	plan, err := planSheet(members, eff)
	if err != nil {
		return BuildResult{}, err
	}

	if err := ctx.Err(); err != nil {
		return BuildResult{}, err
	}

	// Only now is anything large allocated, and planSheet has already proved
	// it is within MaxPixels.
	canvas := image.NewNRGBA(image.Rect(0, 0, plan.sheet.X, plan.sheet.Y))

	if err := drawFrames(ctx, canvas, members, plan, eff); err != nil {
		return BuildResult{}, err
	}

	enc := &png.Encoder{CompressionLevel: png.DefaultCompression, BufferPool: encoderPool{}}
	if err := enc.Encode(dst, canvas); err != nil {
		return BuildResult{}, fmt.Errorf("spritesheet: encode the sheet: %w", err)
	}

	effi := 0.0
	if area := int64(plan.sheet.X) * int64(plan.sheet.Y); area > 0 {
		effi = float64(plan.frameArea) / float64(area)
	}

	return BuildResult{
		Sheet:      plan.sheet,
		Frames:     len(plan.frames),
		Atlas:      plan.frames,
		FrameArea:  plan.frameArea,
		Efficiency: effi,
	}, nil
}

// sheetPlan is the layout decision: where every frame goes and how big the
// sheet has to be. It is produced from headers alone.
type sheetPlan struct {
	sheet     image.Point
	frames    []Frame
	frameArea int64
}

// planSheet decides the layout and refuses a sheet that must not be
// allocated.
//
// The refusals happen here, before the canvas exists, because on this tool the
// output size is attacker-influenced too: an archive of 2048 frames of
// 4096x4096 is a request for an allocation that has to be declined rather
// than discovered while attempting it.
func planSheet(members []member, opts BuildOptions) (sheetPlan, error) {
	if len(members) == 0 {
		return sheetPlan{}, &ArchiveError{
			Reason: "it holds no frames",
			Public: "That archive holds no frames.",
		}
	}

	// The cheapest refusal of all: the frames cannot fit any permitted sheet
	// whatever the packing, so there is no point packing them.
	var frameArea int64
	for i := range members {
		frameArea += int64(members[i].size.X) * int64(members[i].size.Y)
	}

	if frameArea > int64(MaxPixels) {
		return sheetPlan{}, &SheetSizeError{
			FrameArea: frameArea,
			Limit:     MaxPixels,
			Frames:    len(members),
		}
	}

	var plan sheetPlan

	var err error

	switch opts.Layout {
	case LayoutCompact:
		// Only the compact packer can fail to place a frame: a grid always
		// has room for one more cell, and whether the resulting sheet is
		// within the caps is checked below for both.
		plan, err = packCompact(members, opts)
		if err != nil {
			return sheetPlan{}, err
		}
	case LayoutGrid:
		plan = packGrid(members, opts)
	}

	if opts.POT {
		plan.sheet = image.Pt(nextPOT(plan.sheet.X), nextPOT(plan.sheet.Y))
	}

	plan.frameArea = frameArea

	// Checked after the layout, because the layout is what decides the size,
	// and checked before the allocation, which is the whole point.
	if plan.sheet.X > opts.MaxSize || plan.sheet.Y > opts.MaxSize {
		return sheetPlan{}, &SheetSizeError{
			Width:     plan.sheet.X,
			Height:    plan.sheet.Y,
			MaxSize:   opts.MaxSize,
			FrameArea: frameArea,
			Frames:    len(members),
		}
	}

	if int64(plan.sheet.X)*int64(plan.sheet.Y) > int64(MaxPixels) {
		return sheetPlan{}, &SheetSizeError{
			Width:     plan.sheet.X,
			Height:    plan.sheet.Y,
			Limit:     MaxPixels,
			FrameArea: frameArea,
			Frames:    len(members),
		}
	}

	return plan, nil
}

// SheetSizeError reports a sheet that would be too large to build.
//
// It carries the frame count and the total frame area as well as the size,
// because the useful message is not "too big" but "this many frames need this
// much room and the limit is that".
type SheetSizeError struct {
	// Width and Height are the sheet the layout asked for, zero when the
	// frames were refused before any layout was attempted.
	Width, Height int
	// MaxSize is the per-side cap that was exceeded, zero when the area cap
	// was the one exceeded.
	MaxSize int
	// Limit is the area cap that was exceeded, zero when the per-side cap was
	// the one exceeded.
	Limit int
	// FrameArea is the total area of the frames, in pixels.
	FrameArea int64
	// Frames is how many frames were given.
	Frames int
}

// Error implements error.
func (e *SheetSizeError) Error() string {
	if e.MaxSize > 0 {
		return fmt.Sprintf("spritesheet: %d frames need a %dx%d sheet, over the %d pixel limit per side",
			e.Frames, e.Width, e.Height, e.MaxSize)
	}

	if e.Width > 0 {
		return fmt.Sprintf("spritesheet: %d frames need a %dx%d sheet, which is %d pixels, over the limit of %d",
			e.Frames, e.Width, e.Height, int64(e.Width)*int64(e.Height), e.Limit)
	}

	return fmt.Sprintf("spritesheet: %d frames total %d pixels, over the limit of %d",
		e.Frames, e.FrameArea, e.Limit)
}

// Message is the sentence to show the person. It states the limit as a number
// and says what would be needed, because a person who hits a cap needs both.
func (e *SheetSizeError) Message() string {
	if e.MaxSize > 0 {
		return fmt.Sprintf("Those %d frames need a %d×%d sheet, and the largest allowed is "+
			"%d×%d. Use fewer frames, make them smaller, or raise the maximum size.",
			e.Frames, e.Width, e.Height, e.MaxSize, e.MaxSize)
	}

	side := squareSideOf(e.Limit)

	if e.Width > 0 {
		return fmt.Sprintf("Those %d frames need a %d×%d sheet, which is more than the "+
			"%d pixel limit — about %d×%d. Use fewer frames or make them smaller.",
			e.Frames, e.Width, e.Height, e.Limit, side, side)
	}

	return fmt.Sprintf("Those %d frames come to %d pixels in total, and the limit for one sheet "+
		"is %d — about %d×%d. Use fewer frames or make them smaller.",
		e.Frames, e.FrameArea, e.Limit, side, side)
}

// packGrid gives every frame an identical cell, sized to the largest frame.
//
// Uniform cells waste space when the frames differ, and that is accepted on
// purpose: a uniform grid is the one layout every engine imports with no
// metadata file at all, which is worth more to somebody targeting Godot or
// Unity than a few per cent of texture area.
func packGrid(members []member, opts BuildOptions) sheetPlan {
	cellW, cellH := 0, 0
	for i := range members {
		cellW = max(cellW, members[i].size.X)
		cellH = max(cellH, members[i].size.Y)
	}

	n := len(members)

	// Near-square, because a long thin sheet wastes a texture unit and reads
	// badly when a person opens it.
	cols := int(math.Ceil(math.Sqrt(float64(n))))
	rows := (n + cols - 1) / cols

	stepX, stepY := cellW+opts.Padding, cellH+opts.Padding

	sheet := image.Pt(
		2*opts.Margin+cols*cellW+(cols-1)*opts.Padding,
		2*opts.Margin+rows*cellH+(rows-1)*opts.Padding,
	)

	frames := make([]Frame, 0, n)

	for i := range members {
		m := members[i]
		col, row := i%cols, i/cols

		at := image.Pt(opts.Margin+col*stepX, opts.Margin+row*stepY)

		frames = append(frames, Frame{
			Name:   m.name,
			Rect:   image.Rect(at.X, at.Y, at.X+m.size.X, at.Y+m.size.Y),
			Source: m.size,
		})
	}

	return sheetPlan{sheet: sheet, frames: frames}
}

// packCompact places frames with a skyline bottom-left heuristic.
//
// Frames are sorted by descending height, then by descending width, then by
// name: a total order, so the output is identical on every run whatever order
// the archive listed them in. MaxRects would pack a little tighter; whether
// that is worth a second algorithm is a question for the efficiency figure in
// docs/benchmarks.md rather than for taste.
func packCompact(members []member, opts BuildOptions) (sheetPlan, error) {
	order := make([]int, len(members))
	for i := range order {
		order[i] = i
	}

	slices.SortFunc(order, func(a, b int) int {
		ma, mb := members[a], members[b]
		if ma.size.Y != mb.size.Y {
			return mb.size.Y - ma.size.Y
		}

		if ma.size.X != mb.size.X {
			return mb.size.X - ma.size.X
		}

		return cmpString(ma.name, mb.name)
	})

	// The width to pack into: the square whose area is the frames' total,
	// widened to fit the widest frame, and never over the cap. Starting from
	// the ideal square is what keeps the sheet near-square.
	var area int64
	widest := 0

	for i := range members {
		area += int64(members[i].size.X+opts.Padding) * int64(members[i].size.Y+opts.Padding)
		widest = max(widest, members[i].size.X)
	}

	width := max(squareSideOf(int(min(area, int64(MaxPixels)))), widest+2*opts.Margin)
	width = min(width, opts.MaxSize)

	placed := make([]Frame, len(members))

	// skyline holds, per column, the first free y. One int per pixel column
	// is at most MaxDimension ints, which is nothing beside the canvas.
	skyline := make([]int, width)
	for i := range skyline {
		skyline[i] = opts.Margin
	}

	sheetH := opts.Margin

	for _, idx := range order {
		m := members[idx]

		w := m.size.X + opts.Padding
		h := m.size.Y + opts.Padding

		x, y, ok := lowestFit(skyline, w, opts.Margin, width)
		if !ok {
			return sheetPlan{}, &SheetSizeError{
				Width:   width,
				Height:  sheetH,
				MaxSize: opts.MaxSize,
				Frames:  len(members),
			}
		}

		for i := x; i < x+w && i < width; i++ {
			skyline[i] = y + h
		}

		placed[idx] = Frame{
			Name:   m.name,
			Rect:   image.Rect(x, y, x+m.size.X, y+m.size.Y),
			Source: m.size,
		}

		sheetH = max(sheetH, y+h)
	}

	// The padding after the last row and column is not needed, but the margin
	// is.
	return sheetPlan{
		sheet:  image.Pt(width, sheetH-opts.Padding+opts.Margin),
		frames: placed,
	}, nil
}

// lowestFit returns the leftmost position at the lowest height where a run of
// w columns fits under the skyline.
func lowestFit(skyline []int, w, margin, width int) (int, int, bool) {
	if w > width-margin {
		return 0, 0, false
	}

	bestX, bestY := -1, 0

	for x := margin; x+w <= width; x++ {
		y := 0
		for i := x; i < x+w; i++ {
			y = max(y, skyline[i])
		}

		if bestX < 0 || y < bestY {
			bestX, bestY = x, y
		}
	}

	if bestX < 0 {
		return 0, 0, false
	}

	return bestX, bestY, true
}

// cmpString compares two strings, for a total sort order.
func cmpString(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// nextPOT rounds n up to a power of two.
func nextPOT(n int) int {
	if n < 1 {
		return 1
	}

	p := 1
	for p < n {
		p <<= 1
	}

	return p
}

// drawFrames is pass two: decode one frame at a time into the canvas.
//
// One frame is live at a time and nothing accumulates, which is what makes the
// peak memory one canvas plus one frame rather than one canvas plus every
// frame.
func drawFrames(
	ctx context.Context,
	canvas *image.NRGBA,
	members []member,
	plan sheetPlan,
	opts BuildOptions,
) error {
	byName := make(map[string]int, len(plan.frames))
	for i := range plan.frames {
		byName[plan.frames[i].Name] = i
	}

	for i := range members {
		if i%ctxCheckFrames == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}

		m := members[i]

		at, ok := byName[m.name]
		if !ok {
			return fmt.Errorf("spritesheet: frame %q was planned but not placed", m.name)
		}

		dst := plan.frames[at].Rect

		img, err := decodeMember(m)
		if err != nil {
			return err
		}

		draw.Draw(canvas, dst, img, img.Bounds().Min, draw.Src)

		if opts.Extrude > 0 {
			extrude(canvas, dst, opts.Extrude)
		}
	}

	return nil
}

// extrude repeats a frame's own edge pixels outward by n pixels.
//
// Transparent padding stops a neighbour bleeding in, but it bleeds itself: at
// a non-integer zoom the filter mixes the frame's edge with transparent black
// and the frame gets a dark halo. Repeating the edge instead means whatever
// the filter samples outside the frame is the frame's own colour.
func extrude(canvas *image.NRGBA, r image.Rectangle, n int) {
	bounds := canvas.Bounds()

	// The four sides.
	for d := 1; d <= n; d++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			copyPixel(canvas, bounds, x, r.Min.Y-d, x, r.Min.Y)
			copyPixel(canvas, bounds, x, r.Max.Y-1+d, x, r.Max.Y-1)
		}

		for y := r.Min.Y; y < r.Max.Y; y++ {
			copyPixel(canvas, bounds, r.Min.X-d, y, r.Min.X, y)
			copyPixel(canvas, bounds, r.Max.X-1+d, y, r.Max.X-1, y)
		}
	}

	// The four corners, so a diagonal sample outside a corner is also the
	// frame's own colour rather than a gap between two extruded edges.
	for dy := 1; dy <= n; dy++ {
		for dx := 1; dx <= n; dx++ {
			copyPixel(canvas, bounds, r.Min.X-dx, r.Min.Y-dy, r.Min.X, r.Min.Y)
			copyPixel(canvas, bounds, r.Max.X-1+dx, r.Min.Y-dy, r.Max.X-1, r.Min.Y)
			copyPixel(canvas, bounds, r.Min.X-dx, r.Max.Y-1+dy, r.Min.X, r.Max.Y-1)
			copyPixel(canvas, bounds, r.Max.X-1+dx, r.Max.Y-1+dy, r.Max.X-1, r.Max.Y-1)
		}
	}
}

// copyPixel copies one pixel inside the canvas, ignoring a destination that
// falls outside it.
func copyPixel(canvas *image.NRGBA, bounds image.Rectangle, dx, dy, sx, sy int) {
	if !image.Pt(dx, dy).In(bounds) || !image.Pt(sx, sy).In(bounds) {
		return
	}

	di := canvas.PixOffset(dx, dy)
	si := canvas.PixOffset(sx, sy)

	copy(canvas.Pix[di:di+4], canvas.Pix[si:si+4])
}
