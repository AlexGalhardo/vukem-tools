package pixelsvg

import (
	"cmp"
	"context"
	"slices"
)

// run is a maximal horizontal stretch of one palette index in one row. x1 is
// exclusive.
type run struct {
	y, x0, x1 int
}

// rect is an axis-aligned rectangle of one colour, in pixel units.
type rect struct {
	x, y, w, h int
}

// extractRuns returns, for every palette index, that colour's runs in
// row-major order.
//
// Rows are shared out over bands and each band collects its own fragments, so
// no two workers append to the same slice; the fragments are then stitched in
// band order, which is why the result does not depend on how the bands were
// scheduled. This is the per-pixel stage of the geometry pass, and the only
// part of it worth parallelising: everything downstream works on runs, of
// which pixel art has orders of magnitude fewer.
func extractRuns(ctx context.Context, g *grid, opts Options) ([][]run, error) {
	if len(g.pal) == 0 {
		return nil, nil
	}

	bands := bandsFor(g.h, opts.Workers)
	parts := make([][][]run, len(bands))
	errs := make([]error, len(bands))

	runBands(bands, func(bi int, b band) {
		local := make([][]run, len(g.pal))

		for y := b.y0; y < b.y1; y++ {
			if (y-b.y0)%ctxCheckRows == 0 {
				if err := ctx.Err(); err != nil {
					errs[bi] = err

					return
				}
			}

			row := g.row(y)

			for x := 0; x < g.w; {
				c := row[x]
				if c == transparentIndex {
					x++

					continue
				}

				x0 := x
				for x++; x < g.w && row[x] == c; x++ {
				}

				local[c] = append(local[c], run{y: y, x0: x0, x1: x})
			}
		}

		parts[bi] = local
	})

	if err := firstErr(errs); err != nil {
		return nil, err
	}

	if len(parts) == 1 {
		return parts[0], nil
	}

	out := make([][]run, len(g.pal))

	for ci := range out {
		total := 0
		for _, p := range parts {
			total += len(p[ci])
		}

		out[ci] = make([]run, 0, total)
		for _, p := range parts {
			out[ci] = append(out[ci], p[ci]...)
		}
	}

	return out, nil
}

// mergeRuns merges the runs of one colour into maximal rectangles.
//
// The horizontal merge already happened: a run is as wide as it can be. This
// is the vertical half, a single sweep that extends an open rectangle down
// whenever the row below holds a run with exactly the same start and width,
// and closes it otherwise. Two rectangles never overlap and together they
// cover exactly the runs they came from, so the output is pixel-exact.
//
// The result is sorted by row then column, which is the order it is written
// in, so it does not depend on the order rectangles happened to close in.
func mergeRuns(runs []run) []rect {
	out := make([]rect, 0, len(runs))

	var open, next []rect

	for i := 0; i < len(runs); {
		y := runs[i].y

		j := i
		for j < len(runs) && runs[j].y == y {
			j++
		}

		if len(open) > 0 && open[0].y+open[0].h != y {
			out = append(out, open...)
			open = open[:0]
		}

		next = next[:0]
		oi := 0

		for _, r := range runs[i:j] {
			for oi < len(open) && open[oi].x < r.x0 {
				out = append(out, open[oi])
				oi++
			}

			if oi < len(open) && open[oi].x == r.x0 && open[oi].w == r.x1-r.x0 {
				e := open[oi]
				e.h++
				next = append(next, e)
				oi++

				continue
			}

			next = append(next, rect{x: r.x0, y: y, w: r.x1 - r.x0, h: 1})
		}

		out = append(out, open[oi:]...)
		open = append(open[:0], next...)
		i = j
	}

	out = append(out, open...)

	slices.SortFunc(out, func(a, b rect) int {
		if c := cmp.Compare(a.y, b.y); c != 0 {
			return c
		}

		return cmp.Compare(a.x, b.x)
	})

	return out
}

// labelRuns groups the runs of one colour into 4-connected regions with
// union-find over the runs themselves.
//
// One pass per row: every run of the current row is unioned with the runs of
// the previous row whose horizontal extent it overlaps. Working on runs rather
// than pixels keeps the pass linear in the number of runs, and two-pointering
// the two rows keeps the overlap test linear too.
//
// It returns the region id of every run and the number of regions. Ids are
// handed out in the order the runs appear, so the topmost-leftmost run of a
// region decides its id and the numbering is reproducible.
func labelRuns(runs []run) ([]int, int) {
	parent := make([]int, len(runs))
	for i := range parent {
		parent[i] = i
	}

	union := func(a, b int) {
		ra, rb := findRoot(parent, a), findRoot(parent, b)
		if ra == rb {
			return
		}

		if ra < rb {
			parent[rb] = ra
		} else {
			parent[ra] = rb
		}
	}

	prevLo, prevHi := 0, 0

	for i := 0; i < len(runs); {
		y := runs[i].y

		j := i
		for j < len(runs) && runs[j].y == y {
			j++
		}

		if prevHi > prevLo && runs[prevLo].y == y-1 {
			p := prevLo

			for k := i; k < j; k++ {
				for p < prevHi && runs[p].x1 <= runs[k].x0 {
					p++
				}

				for q := p; q < prevHi && runs[q].x0 < runs[k].x1; q++ {
					union(k, q)
				}
			}
		}

		prevLo, prevHi = i, j
		i = j
	}

	regionOf := make([]int, len(runs))
	ids := make(map[int]int)
	count := 0

	for i := range runs {
		root := findRoot(parent, i)

		id, ok := ids[root]
		if !ok {
			id = count
			ids[root] = id
			count++
		}

		regionOf[i] = id
	}

	return regionOf, count
}

// groupByRegion returns the runs of each region, keeping row-major order
// inside a region.
func groupByRegion(runs []run, regionOf []int, count int) [][]run {
	sizes := make([]int, count)
	for _, id := range regionOf {
		sizes[id]++
	}

	out := make([][]run, count)
	for i := range out {
		out[i] = make([]run, 0, sizes[i])
	}

	for i, r := range runs {
		id := regionOf[i]
		out[id] = append(out[id], r)
	}

	return out
}

// findRoot returns the representative of i in the union-find forest, halving
// the path on the way up.
func findRoot(parent []int, i int) int {
	for parent[i] != i {
		parent[i] = parent[parent[i]]
		i = parent[i]
	}

	return i
}
