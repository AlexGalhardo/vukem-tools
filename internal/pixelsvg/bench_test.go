package pixelsvg

import (
	"image"
	"image/color"
	"io"
	"testing"
)

// testPalette returns n distinct colours, every fifth of them half
// transparent. The multipliers are odd, so the channel values do not repeat
// for any n up to 256.
func testPalette(n int) []color.NRGBA {
	out := make([]color.NRGBA, n)

	for i := range out {
		out[i] = color.NRGBA{
			R: uint8((i*37 + 11) & 0xff),
			G: uint8((i*91 + 29) & 0xff),
			B: uint8((i*53 + 7) & 0xff),
			A: 0xff,
		}

		if i%5 == 4 {
			out[i].A = 0x80
		}
	}

	return out
}

// generatedImage builds a deterministic pixel-art-like image: flat blocks of
// one colour, with one block value in every colours+1 left fully transparent.
// Real pixel art is blocky, so this is a fairer benchmark input than noise,
// which would turn every pixel into its own run.
func generatedImage(w, h, colors, block int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	pal := testPalette(colors)

	for y := range h {
		for x := range w {
			k := ((x/block)*73856093 ^ (y/block)*19349663) % (colors + 1)
			if k < 0 {
				k += colors + 1
			}

			if k == colors {
				continue
			}

			img.SetNRGBA(x, y, pal[k])
		}
	}

	return img
}

// solidImage returns an image of one colour.
func solidImage(w, h int, c color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))

	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, c)
		}
	}

	return img
}

// diskImage returns a filled disk, whose contour is a long staircase of
// one-pixel steps. It is the shape that tells an exact trace apart from a
// simplified one: a rectangle has nothing left to simplify.
func diskImage(size int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	c := color.NRGBA{R: 0x30, G: 0xc0, B: 0x90, A: 0xff}
	r := size / 2

	for y := range size {
		for x := range size {
			dx, dy := x-r, y-r
			if dx*dx+dy*dy <= r*r {
				img.SetNRGBA(x, y, c)
			}
		}
	}

	return img
}

// checkerImage returns a one-pixel checkerboard, the worst case for both the
// rectangle merge and the contour trace.
func checkerImage(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	a := color.NRGBA{R: 0xff, A: 0xff}
	b := color.NRGBA{B: 0xff, A: 0xff}

	for y := range h {
		for x := range w {
			if (x+y)%2 == 0 {
				img.SetNRGBA(x, y, a)
			} else {
				img.SetNRGBA(x, y, b)
			}
		}
	}

	return img
}

// benchModes is every mode, benchmarked at the 2048x2048 cap.
var benchModes = []Mode{ModeSquare, ModeRect, ModeContour}

// BenchmarkConvert2048 measures each mode at the dimension cap and reports the
// output size and element count alongside the timing, because the cost of a
// mode is as much what it hands the browser as how long it took.
func BenchmarkConvert2048(b *testing.B) {
	src := generatedImage(MaxDimension, MaxDimension, 64, 8)

	for _, mode := range benchModes {
		b.Run(string(mode), func(b *testing.B) {
			var res Result

			b.ReportAllocs()
			b.ResetTimer()

			for b.Loop() {
				var err error

				res, err = ConvertImage(b.Context(), io.Discard, src, Options{Mode: mode})
				if err != nil {
					b.Fatalf("convert: %v", err)
				}
			}

			b.StopTimer()
			b.ReportMetric(float64(res.Bytes), "svg_bytes")
			b.ReportMetric(float64(res.Elements), "elements")
			b.ReportMetric(float64(res.Colors), "colors")
		})
	}
}

// BenchmarkConvert2048Quantize measures the cost of the colour reduction
// passes on an input that actually needs them.
func BenchmarkConvert2048Quantize(b *testing.B) {
	src := generatedImage(MaxDimension, MaxDimension, 256, 4)

	for _, tc := range []struct {
		name string
		opts Options
	}{
		{"none", Options{Mode: ModeRect, MaxColors: -1}},
		{"cap64", Options{Mode: ModeRect, MaxColors: 64}},
		{"tolerance32", Options{Mode: ModeRect, Tolerance: 32}},
	} {
		b.Run(tc.name, func(b *testing.B) {
			var res Result

			b.ReportAllocs()
			b.ResetTimer()

			for b.Loop() {
				var err error

				res, err = ConvertImage(b.Context(), io.Discard, src, tc.opts)
				if err != nil {
					b.Fatalf("convert: %v", err)
				}
			}

			b.StopTimer()
			b.ReportMetric(float64(res.Bytes), "svg_bytes")
			b.ReportMetric(float64(res.Elements), "elements")
			b.ReportMetric(float64(res.Colors), "colors")
		})
	}
}
