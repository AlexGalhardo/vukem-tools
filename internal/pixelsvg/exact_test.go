package pixelsvg

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

// sourceCoverage returns the colour every drawn pixel of src should end up
// with, under the given alpha mode.
func sourceCoverage(t *testing.T, src *image.NRGBA, alpha AlphaMode) map[point]color.NRGBA {
	t.Helper()

	out := make(map[point]color.NRGBA)
	b := src.Bounds()

	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := src.NRGBAAt(x, y)
			if c.A == 0 || (alpha == AlphaDrop && c.A != 0xff) {
				continue
			}

			out[point{x: x - b.Min.X, y: y - b.Min.Y}] = c
		}
	}

	return out
}

// exactCases are the images the pixel-exactness assertions run on. They cover
// the hand-made sprite, shapes that stress the vertical merge, and a generated
// image big enough to be split over several bands.
func exactCases(t *testing.T) []struct {
	name string
	img  *image.NRGBA
} {
	t.Helper()

	return []struct {
		name string
		img  *image.NRGBA
	}{
		{"sprite", spriteImage(t)},
		{"single-pixel", solidImage(1, 1, color.NRGBA{R: 1, A: 0xff})},
		{"one-row", generatedImage(64, 1, 7, 3)},
		{"one-column", generatedImage(1, 64, 7, 3)},
		{"checkerboard", checkerImage(17, 13)},
		{"banded", generatedImage(131, 97, 11, 4)},
		{"flat", solidImage(40, 40, color.NRGBA{R: 0x20, G: 0x40, B: 0x60, A: 0xff})},
		{"disk", diskImage(41)},
	}
}

// TestPixelExact asserts that square and rect modes emit geometry covering
// exactly the drawn pixels of the source, with no gap and no overlap.
//
// The check runs on the emitted document, parsed back by this package's own
// parser, so it tests what a browser would be handed rather than the internal
// geometry. No rasteriser is involved: every shape in those two modes is an
// axis-aligned rectangle on integer coordinates, so coverage is exact
// arithmetic rather than a pixel comparison with a tolerance.
func TestPixelExact(t *testing.T) {
	for _, tc := range exactCases(t) {
		for _, mode := range []Mode{ModeSquare, ModeRect} {
			for _, alpha := range []AlphaMode{AlphaOpacity, AlphaDrop} {
				name := tc.name + "/" + string(mode) + "/" + string(alpha)

				t.Run(name, func(t *testing.T) {
					var buf bytes.Buffer

					opts := Options{Mode: mode, Alpha: alpha, Workers: 3}
					if _, err := ConvertImage(t.Context(), &buf, tc.img, opts); err != nil {
						t.Fatalf("convert: %v", err)
					}

					doc := parseSVG(t, buf.String())
					got := coverage(t, doc)
					want := sourceCoverage(t, tc.img, alpha)

					b := tc.img.Bounds()
					if doc.viewW != b.Dx() || doc.viewH != b.Dy() {
						t.Errorf("viewBox is %dx%d, want %dx%d", doc.viewW, doc.viewH, b.Dx(), b.Dy())
					}

					comparePixels(t, got, want)
				})
			}
		}
	}
}

// comparePixels reports every pixel the output and the source disagree on.
func comparePixels(t *testing.T, got, want map[point]color.NRGBA) {
	t.Helper()

	if len(got) != len(want) {
		t.Errorf("output covers %d pixels, want %d", len(got), len(want))
	}

	bad := 0

	for p, w := range want {
		g, ok := got[p]

		switch {
		case !ok:
			t.Errorf("pixel (%d,%d) is not covered, want %v", p.x, p.y, w)
		case g != w:
			t.Errorf("pixel (%d,%d) = %v, want %v", p.x, p.y, g, w)
		default:
			continue
		}

		bad++
		if bad > 10 {
			t.Fatal("too many differing pixels")
		}
	}

	for p := range got {
		if _, ok := want[p]; !ok {
			t.Errorf("pixel (%d,%d) is covered but the source does not draw it", p.x, p.y)

			bad++
			if bad > 10 {
				t.Fatal("too many differing pixels")
			}
		}
	}
}

// TestRectAndSquareAgree asserts the two pixel-exact modes cover identically,
// and that rect uses strictly fewer elements on an image that can be merged.
func TestRectAndSquareAgree(t *testing.T) {
	src := generatedImage(96, 64, 9, 4)

	var squares, rects bytes.Buffer

	sq, err := ConvertImage(t.Context(), &squares, src, Options{Mode: ModeSquare})
	if err != nil {
		t.Fatalf("convert square: %v", err)
	}

	rc, err := ConvertImage(t.Context(), &rects, src, Options{Mode: ModeRect})
	if err != nil {
		t.Fatalf("convert rect: %v", err)
	}

	comparePixels(t, coverage(t, parseSVG(t, rects.String())), coverage(t, parseSVG(t, squares.String())))

	if rc.Elements >= sq.Elements {
		t.Errorf("rect mode emitted %d elements, square mode %d; rect should merge", rc.Elements, sq.Elements)
	}

	if rc.Bytes >= sq.Bytes {
		t.Errorf("rect mode wrote %d bytes, square mode %d; rect should be smaller", rc.Bytes, sq.Bytes)
	}
}

// TestContourAreaExact asserts contour mode is pixel-exact by area.
//
// Rasterising the paths and diffing against the source would need a
// rasteriser; the signed area of the traced loops gives the same guarantee
// without one. Every vertex sits on the pixel lattice and every loop is
// closed, so the sum of the signed areas of a colour's loops is exactly the
// number of pixels that colour fills, holes subtracted. Getting that number
// right for every colour of a sprite with a ring, a hole and several
// disconnected regions is only possible if the contours are exact.
func TestContourAreaExact(t *testing.T) {
	for _, tc := range exactCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer

			opts := Options{Mode: ModeContour, Workers: 3}
			if _, err := ConvertImage(t.Context(), &buf, tc.img, opts); err != nil {
				t.Fatalf("convert: %v", err)
			}

			doc := parseSVG(t, buf.String())
			want := make(map[color.NRGBA]int)

			for _, c := range sourceCoverage(t, tc.img, AlphaOpacity) {
				want[c]++
			}

			for _, g := range doc.groups {
				area := 0
				for _, l := range g.loops {
					area += area2(l)
				}

				if area%2 != 0 {
					t.Fatalf("colour %v has an odd doubled area %d", g.fill, area)
				}

				if got := area / 2; got != want[g.fill] {
					t.Errorf("colour %v encloses %d pixels, want %d", g.fill, got, want[g.fill])
				}

				delete(want, g.fill)
			}

			for c, n := range want {
				t.Errorf("colour %v fills %d pixels but has no group", c, n)
			}
		})
	}
}

// TestContourHole checks the sprite's ring: one path, one outer loop wound one
// way and one hole wound the other, so the default non-zero fill rule punches
// the hole out without a fill-rule attribute.
func TestContourHole(t *testing.T) {
	var buf bytes.Buffer

	opts := Options{Mode: ModeContour}
	if _, err := ConvertImage(t.Context(), &buf, spriteImage(t), opts); err != nil {
		t.Fatalf("convert: %v", err)
	}

	if bytes.Contains(buf.Bytes(), []byte("fill-rule")) {
		t.Error("output writes a fill-rule although the winding already handles holes")
	}

	ring := spritePalette['Y']
	doc := parseSVG(t, buf.String())

	for _, g := range doc.groups {
		if g.fill != ring {
			continue
		}

		if len(g.loops) != 2 {
			t.Fatalf("the ring colour has %d loops, want 2", len(g.loops))
		}

		outer, hole := area2(g.loops[0]), area2(g.loops[1])
		if outer <= 0 {
			t.Errorf("the first loop has area %d, want a positive outer boundary", outer)
		}

		if hole >= 0 {
			t.Errorf("the second loop has area %d, want a negative hole", hole)
		}

		if outer/2 != 16 || hole/2 != -4 {
			t.Errorf("loops enclose %d and %d pixels, want 16 and -4", outer/2, hole/2)
		}

		return
	}

	t.Fatalf("no group for the ring colour %v", ring)
}

// TestContourRegionCount pins the number of 4-connected regions of the sprite,
// which is what decides how many paths contour mode writes.
func TestContourRegionCount(t *testing.T) {
	var buf bytes.Buffer

	opts := Options{Mode: ModeContour}

	res, err := ConvertImage(t.Context(), &buf, spriteImage(t), opts)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}

	// 8 for the dark outline, 3 for the mid tone, 1 for the ring, 1 for the
	// half-transparent row.
	if res.Elements != 13 {
		t.Errorf("contour mode wrote %d paths, want 13", res.Elements)
	}
}

// TestContourSimplifyIsLossy records what SimplifyTolerance costs: above zero
// it is allowed to move the outline, and the only guarantee is the documented
// tolerance. Here it must still produce fewer vertices and a valid document.
func TestContourSimplifyIsLossy(t *testing.T) {
	src := diskImage(96)

	count := func(eps float64) int {
		t.Helper()

		var buf bytes.Buffer

		opts := Options{Mode: ModeContour, SimplifyTolerance: eps}
		if _, err := ConvertImage(t.Context(), &buf, src, opts); err != nil {
			t.Fatalf("convert with eps %v: %v", eps, err)
		}

		n := 0
		for _, g := range parseSVG(t, buf.String()).groups {
			for _, l := range g.loops {
				n += len(l)
			}
		}

		return n
	}

	exact, loose := count(0), count(4)
	if loose >= exact {
		t.Errorf("simplifying to 4 pixels kept %d vertices, exact tracing kept %d", loose, exact)
	}
}
