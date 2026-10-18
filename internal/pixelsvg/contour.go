package pixelsvg

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
)

// The four directions a boundary edge can run in, on the lattice of pixel
// corners rather than pixel centres.
const (
	dirRight uint8 = iota
	dirDown
	dirLeft
	dirUp
)

// The step each direction takes, indexed by direction. y grows downwards.
var (
	dirDX = [4]int{1, 0, -1, 0}
	dirDY = [4]int{0, 1, 0, -1}
)

// point is a corner of the pixel lattice. A w by h image has (w+1) by (h+1) of
// them, and every contour vertex is one, so a contour bounds its pixels
// exactly instead of running through their centres.
type point struct {
	x, y int
}

// edge is one unit of a region boundary, as the corner it starts at and the
// direction it runs in.
type edge struct {
	x, y int
	dir  uint8
}

// cross returns the z component of the cross product of two directions. It is
// +1 for one turn, -1 for the other and 0 for carrying straight on.
func cross(a, b uint8) int {
	return dirDX[a]*dirDY[b] - dirDY[a]*dirDX[b]
}

// regionEdges returns every boundary edge of one region, oriented so that the
// region is always on the right-hand side of the direction of travel.
//
// With that orientation an outer boundary comes out with a positive signed
// area and a hole with a negative one, which is exactly what the default
// non-zero fill rule needs to punch the hole out: no fill-rule attribute has
// to be written, and no second classification pass over the pixels is needed.
//
// The runs of a region are maximal per colour, so the pixel left of a run's
// start and the pixel right of its end are never part of the region: the left
// and right edges of every run are boundary edges unconditionally. Only the
// top and bottom have to be tested, against the region's runs in the row above
// and below, which a single forward pointer per row does in linear time.
func regionEdges(runs []run) []edge {
	edges := make([]edge, 0, 4*len(runs))

	prevLo, prevHi := 0, 0

	for i := 0; i < len(runs); {
		y := runs[i].y

		j := i
		for j < len(runs) && runs[j].y == y {
			j++
		}

		k := j
		for k < len(runs) && runs[k].y == y+1 {
			k++
		}

		var prev, next []run

		if prevHi > prevLo && runs[prevLo].y == y-1 {
			prev = runs[prevLo:prevHi]
		}

		if k > j && runs[j].y == y+1 {
			next = runs[j:k]
		}

		pi, ni := 0, 0

		for _, r := range runs[i:j] {
			edges = append(edges,
				edge{x: r.x0, y: y + 1, dir: dirUp},
				edge{x: r.x1, y: y, dir: dirDown},
			)

			for x := r.x0; x < r.x1; x++ {
				for pi < len(prev) && prev[pi].x1 <= x {
					pi++
				}

				if pi >= len(prev) || prev[pi].x0 > x {
					edges = append(edges, edge{x: x, y: y, dir: dirRight})
				}
			}

			for x := r.x0; x < r.x1; x++ {
				for ni < len(next) && next[ni].x1 <= x {
					ni++
				}

				if ni >= len(next) || next[ni].x0 > x {
					edges = append(edges, edge{x: x + 1, y: y + 1, dir: dirLeft})
				}
			}
		}

		prevLo, prevHi = i, j
		i = j
	}

	return edges
}

// compareEdge orders edges by the corner they start at, then by direction, so
// every edge leaving one corner sits next to its siblings.
func compareEdge(a, b edge) int {
	if c := cmp.Compare(a.y, b.y); c != 0 {
		return c
	}

	if c := cmp.Compare(a.x, b.x); c != 0 {
		return c
	}

	return cmp.Compare(a.dir, b.dir)
}

// traceLoops walks the boundary edges of one region into closed loops.
//
// This is Moore-neighbour tracing moved onto the lattice of pixel corners. The
// textbook version walks pixel centres, which for pixel art loses half a pixel
// on every edge and cannot represent a one-pixel-wide limb at all; following
// the cracks between pixels instead keeps the outline exactly on the pixel
// boundary. The walk is the same idea: arrive at a corner, look at the
// boundary edges leaving it and take the one that turns the same way every
// time.
//
// Turning consistently is not cosmetic. At a corner where two diagonally
// opposite pixels of the region meet, four boundary edges share the corner and
// two loops pass through it; always taking the same turn is what keeps the two
// apart instead of splicing them into a figure of eight.
func traceLoops(edges []edge) [][]point {
	slices.SortFunc(edges, compareEdge)

	used := make([]bool, len(edges))

	var loops [][]point

	for i := range edges {
		if used[i] {
			continue
		}

		start := point{x: edges[i].x, y: edges[i].y}
		pts := make([]point, 0, 8)
		closed := false

		for k := i; k >= 0; {
			e := edges[k]
			used[k] = true
			pts = append(pts, point{x: e.x, y: e.y})

			at := point{x: e.x + dirDX[e.dir], y: e.y + dirDY[e.dir]}
			if at == start {
				closed = true

				break
			}

			k = pickNext(edges, used, at, e.dir)
		}

		if closed && len(pts) >= 4 {
			loops = append(loops, pts)
		}
	}

	return loops
}

// pickNext returns the unused boundary edge leaving at that turns the most in
// the chosen direction, or -1 when there is none.
func pickNext(edges []edge, used []bool, at point, from uint8) int {
	lo, _ := slices.BinarySearchFunc(edges, edge{x: at.x, y: at.y}, func(a, b edge) int {
		if c := cmp.Compare(a.y, b.y); c != 0 {
			return c
		}

		return cmp.Compare(a.x, b.x)
	})

	best, bestTurn := -1, -2

	for i := lo; i < len(edges) && edges[i].y == at.y && edges[i].x == at.x; i++ {
		if used[i] {
			continue
		}

		if turn := cross(from, edges[i].dir); turn > bestTurn {
			best, bestTurn = i, turn
		}
	}

	return best
}

// area2 returns twice the signed area of a closed loop. It is positive for an
// outer boundary and negative for a hole, and its magnitude on an unsimplified
// contour is exactly twice the number of pixels the loop encloses.
func area2(pts []point) int {
	s := 0

	for i, p := range pts {
		q := pts[(i+1)%len(pts)]
		s += p.x*q.y - q.x*p.y
	}

	return s
}

// collapseCollinear drops every vertex that sits on the straight line between
// its neighbours, including across the point where the loop closes. It loses
// nothing: the loop still passes through exactly the same corners.
func collapseCollinear(pts []point) []point {
	out := make([]point, 0, len(pts))

	for _, p := range pts {
		for len(out) >= 2 && turnOf(out[len(out)-2], out[len(out)-1], p) == 0 {
			out = out[:len(out)-1]
		}

		out = append(out, p)
	}

	for len(out) >= 4 && turnOf(out[len(out)-2], out[len(out)-1], out[0]) == 0 {
		out = out[:len(out)-1]
	}

	for len(out) >= 4 && turnOf(out[len(out)-1], out[0], out[1]) == 0 {
		out = out[1:]
	}

	return out
}

// turnOf returns the cross product of b-a and c-b, which is zero exactly when
// the three points are collinear.
func turnOf(a, b, c point) int {
	return (b.x-a.x)*(c.y-b.y) - (b.y-a.y)*(c.x-b.x)
}

// simplifyRing runs Douglas-Peucker over a closed loop with the given epsilon
// in pixels. An epsilon of zero returns the loop untouched, which is what
// keeps [ModeContour] pixel-exact by default.
//
// A closed ring has no natural endpoints, so it is cut at its first vertex and
// at the vertex farthest from it, and the two open polylines are simplified
// separately. Cutting at the farthest vertex keeps the two halves of similar
// length, which matters because Douglas-Peucker never removes an endpoint: a
// badly chosen cut pins a vertex that should have gone.
func simplifyRing(pts []point, eps float64) []point {
	if eps <= 0 || len(pts) < 5 {
		return pts
	}

	far, best := 1, -1

	for i, p := range pts[1:] {
		dx, dy := p.x-pts[0].x, p.y-pts[0].y
		if d := dx*dx + dy*dy; d > best {
			far, best = i+1, d
		}
	}

	second := make([]point, 0, len(pts)-far+1)
	second = append(second, pts[far:]...)
	second = append(second, pts[0])

	head := douglasPeucker(pts[:far+1], eps)
	tail := douglasPeucker(second, eps)

	out := make([]point, 0, len(head)+len(tail))
	out = append(out, head...)
	out = append(out, tail[1:len(tail)-1]...)

	if len(out) < 3 {
		return pts
	}

	return out
}

// douglasPeucker keeps the vertices of an open polyline that are farther than
// eps from the chord they would otherwise be replaced by.
func douglasPeucker(pts []point, eps float64) []point {
	if len(pts) < 3 {
		return slices.Clone(pts)
	}

	keep := make([]bool, len(pts))
	keep[0] = true
	keep[len(pts)-1] = true
	dpSplit(pts, 0, len(pts)-1, eps*eps, keep)

	out := make([]point, 0, len(pts))

	for i, p := range pts {
		if keep[i] {
			out = append(out, p)
		}
	}

	return out
}

// dpSplit marks the vertex of pts[i:j] farthest from the chord i to j when it
// is farther than the squared tolerance, then recurses into both halves.
func dpSplit(pts []point, i, j int, epsSquared float64, keep []bool) {
	if j <= i+1 {
		return
	}

	a, b := pts[i], pts[j]
	dx, dy := b.x-a.x, b.y-a.y
	span := dx*dx + dy*dy

	at, best := -1, 0.0

	for k := i + 1; k < j; k++ {
		var d float64

		if span == 0 {
			ex, ey := pts[k].x-a.x, pts[k].y-a.y
			d = float64(ex*ex + ey*ey)
		} else {
			c := dx*(pts[k].y-a.y) - dy*(pts[k].x-a.x)
			d = float64(c*c) / float64(span)
		}

		if d > best {
			at, best = k, d
		}
	}

	if at < 0 || best <= epsSquared {
		return
	}

	keep[at] = true
	dpSplit(pts, i, at, epsSquared, keep)
	dpSplit(pts, at, j, epsSquared, keep)
}

// regionPath returns the SVG path data for one region: its outer boundary
// followed by its holes, as subpaths of a single path.
func regionPath(runs []run, eps float64) string {
	loops := traceLoops(regionEdges(runs))
	if len(loops) == 0 {
		return ""
	}

	holes := make([]bool, len(loops))
	for i, l := range loops {
		holes[i] = area2(l) < 0
	}

	var b strings.Builder

	for _, hole := range []bool{false, true} {
		for i, l := range loops {
			if holes[i] != hole {
				continue
			}

			writeLoop(&b, simplifyRing(collapseCollinear(l), eps))
		}
	}

	return b.String()
}

// writeLoop appends one closed subpath, using H and V for the axis-aligned
// segments that make up almost all of a pixel-art contour.
func writeLoop(b *strings.Builder, pts []point) {
	if len(pts) < 3 {
		return
	}

	b.WriteByte('M')
	b.WriteString(strconv.Itoa(pts[0].x))
	b.WriteByte(' ')
	b.WriteString(strconv.Itoa(pts[0].y))

	prev := pts[0]

	for _, p := range pts[1:] {
		switch {
		case p.y == prev.y:
			b.WriteByte('H')
			b.WriteString(strconv.Itoa(p.x))
		case p.x == prev.x:
			b.WriteByte('V')
			b.WriteString(strconv.Itoa(p.y))
		default:
			b.WriteByte('L')
			b.WriteString(strconv.Itoa(p.x))
			b.WriteByte(' ')
			b.WriteString(strconv.Itoa(p.y))
		}

		prev = p
	}

	b.WriteByte('Z')
}
