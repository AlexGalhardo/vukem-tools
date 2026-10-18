package pixelsvg

import (
	"cmp"
	"context"
	"image"
	"image/color"
	"slices"
)

// transparentIndex marks a pixel that is not drawn at all.
const transparentIndex int32 = -1

// grid is an image reduced to one palette index per pixel.
type grid struct {
	w, h int
	// idx holds w*h palette indices in row-major order, or transparentIndex
	// for a pixel that is not drawn.
	idx []int32
	// pal is the final palette, sorted by R, G, B then A. It never holds a
	// fully transparent colour.
	pal []color.NRGBA
	// counts is the number of pixels per palette entry.
	counts []int
}

// row returns the palette indices of row y.
func (g *grid) row(y int) []int32 {
	return g.idx[y*g.w : (y+1)*g.w]
}

// newGrid reduces img to a palette and an index grid, applying the colour cap,
// the quantiser and the tolerance pass from opts.
func newGrid(ctx context.Context, img *image.NRGBA, opts Options) (*grid, error) {
	w, h := img.Rect.Dx(), img.Rect.Dy()

	colors, counts, err := collectPalette(ctx, img, opts)
	if err != nil {
		return nil, err
	}

	pal, palCounts, lookup, err := reduce(colors, counts, opts)
	if err != nil {
		return nil, err
	}

	g := &grid{w: w, h: h, idx: make([]int32, w*h), pal: pal, counts: palCounts}
	if len(pal) == 0 {
		for i := range g.idx {
			g.idx[i] = transparentIndex
		}

		return g, nil
	}

	bands := bandsFor(h, opts.Workers)
	errs := make([]error, len(bands))
	dropPartial := opts.Alpha == AlphaDrop

	runBands(bands, func(bi int, b band) {
		var (
			last color.NRGBA
			li   int32
			have bool
		)

		for y := b.y0; y < b.y1; y++ {
			if (y-b.y0)%ctxCheckRows == 0 {
				if err := ctx.Err(); err != nil {
					errs[bi] = err

					return
				}
			}

			src := img.Pix[y*img.Stride : y*img.Stride+w*4]
			dst := g.row(y)

			for x := range w {
				o := x * 4
				c := color.NRGBA{R: src[o], G: src[o+1], B: src[o+2], A: src[o+3]}

				switch {
				case c.A == 0 || (dropPartial && c.A != 0xff):
					dst[x] = transparentIndex
					have = false
				case have && c == last:
					dst[x] = li
				default:
					li = lookup[c]
					last, have = c, true
					dst[x] = li
				}
			}
		}
	})

	if err := firstErr(errs); err != nil {
		return nil, err
	}

	return g, nil
}

// collectPalette returns every distinct drawn colour of img, sorted by R, G, B
// then A, with the pixel count of each.
//
// Rows are shared out over bands and counted into a per-band map, so no two
// workers touch the same map; the maps are merged afterwards. Within a band a
// run of one colour is counted in bulk, which turns most of the pass into a
// four-byte comparison instead of a map write.
func collectPalette(ctx context.Context, img *image.NRGBA, opts Options) ([]color.NRGBA, []int, error) {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	bands := bandsFor(h, opts.Workers)
	locals := make([]map[color.NRGBA]int, len(bands))
	errs := make([]error, len(bands))
	dropPartial := opts.Alpha == AlphaDrop

	runBands(bands, func(bi int, b band) {
		m := make(map[color.NRGBA]int)

		var (
			last color.NRGBA
			n    int
		)

		flush := func() {
			if n > 0 {
				m[last] += n
				n = 0
			}
		}

		for y := b.y0; y < b.y1; y++ {
			if (y-b.y0)%ctxCheckRows == 0 {
				if err := ctx.Err(); err != nil {
					errs[bi] = err

					return
				}
			}

			src := img.Pix[y*img.Stride : y*img.Stride+w*4]

			for x := range w {
				o := x * 4
				c := color.NRGBA{R: src[o], G: src[o+1], B: src[o+2], A: src[o+3]}

				switch {
				case c.A == 0 || (dropPartial && c.A != 0xff):
					flush()
				case n > 0 && c == last:
					n++
				default:
					flush()
					last, n = c, 1
				}
			}
		}

		flush()

		locals[bi] = m
	})

	if err := firstErr(errs); err != nil {
		return nil, nil, err
	}

	total := locals[0]
	for _, m := range locals[1:] {
		for c, n := range m {
			total[c] += n
		}
	}

	colors := make([]color.NRGBA, 0, len(total))
	for c := range total {
		colors = append(colors, c)
	}

	slices.SortFunc(colors, compareNRGBA)

	counts := make([]int, len(colors))
	for i, c := range colors {
		counts[i] = total[c]
	}

	return colors, counts, nil
}

// compareNRGBA orders colours by R, then G, then B, then A. It is the total
// order the palette, and with it the whole output, is sorted by.
func compareNRGBA(a, b color.NRGBA) int {
	if c := cmp.Compare(a.R, b.R); c != 0 {
		return c
	}

	if c := cmp.Compare(a.G, b.G); c != 0 {
		return c
	}

	if c := cmp.Compare(a.B, b.B); c != 0 {
		return c
	}

	return cmp.Compare(a.A, b.A)
}

// subset is a set of palette entries with their pixel counts, kept in
// ascending palette-index order.
type subset struct {
	// ids are indices into the original palette.
	ids    []int32
	colors []color.NRGBA
	counts []int
}

// fullSubset returns the subset holding every entry of the original palette.
func fullSubset(colors []color.NRGBA, counts []int) subset {
	s := subset{ids: make([]int32, len(colors)), colors: colors, counts: counts}
	for i := range s.ids {
		s.ids[i] = int32(i)
	}

	return s
}

// restrict returns the subset of the representatives named by rep, with the
// counts of the entries they represent summed into them. colors is the
// original palette, which rep indexes into.
func (s subset) restrict(rep []int32, colors []color.NRGBA) subset {
	ids := slices.Clone(rep)
	slices.Sort(ids)
	ids = slices.Compact(ids)

	out := subset{ids: ids, colors: make([]color.NRGBA, len(ids)), counts: make([]int, len(ids))}
	at := make(map[int32]int, len(ids))

	for i, id := range ids {
		out.colors[i] = colors[id]
		at[id] = i
	}

	for i, id := range rep {
		out.counts[at[id]] += s.counts[i]
	}

	return out
}

// reduce builds the final palette and a lookup from every original colour to
// its index in that palette, applying the colour cap, the quantiser and the
// tolerance pass in that order.
func reduce(
	colors []color.NRGBA,
	counts []int,
	opts Options,
) (pal []color.NRGBA, palCounts []int, lookup map[color.NRGBA]int32, err error) {
	rep := make([]int32, len(colors))
	for i := range rep {
		rep[i] = int32(i)
	}

	s := fullSubset(colors, counts)

	if opts.MaxColors >= 0 && len(s.ids) > opts.MaxColors {
		if opts.Quantizer == QuantizerNone {
			return nil, nil, nil, &ColorCountError{Count: len(s.ids), Limit: opts.MaxColors}
		}

		r := medianCut(s, opts.MaxColors)
		compose(rep, s, r)
		s = s.restrict(r, colors)
	}

	if opts.Tolerance > 0 && len(s.ids) > 1 {
		r := mergeTolerance(s, opts.Tolerance)
		compose(rep, s, r)
		s = s.restrict(r, colors)
	}

	pal = make([]color.NRGBA, len(s.ids))
	palCounts = make([]int, len(s.ids))
	at := make(map[int32]int32, len(s.ids))

	for i, id := range s.ids {
		pal[i] = colors[id]
		at[id] = int32(i)
	}

	lookup = make(map[color.NRGBA]int32, len(colors))

	for i, c := range colors {
		p := at[rep[i]]
		lookup[c] = p
		palCounts[p] += counts[i]
	}

	return pal, palCounts, lookup, nil
}

// compose folds a reduction of s into rep, which maps every original palette
// index to the original index of its current representative.
func compose(rep []int32, s subset, r []int32) {
	m := make(map[int32]int32, len(s.ids))
	for i, id := range s.ids {
		m[id] = r[i]
	}

	for i, cur := range rep {
		if next, ok := m[cur]; ok {
			rep[i] = next
		}
	}
}
