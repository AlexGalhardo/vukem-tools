package pixelsvg

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"testing"
)

// fuzzModes is indexed by the first seed byte, so the fuzzer can reach every
// mode and both alpha handlings without a second input.
var fuzzModes = []Options{
	{Mode: ModeRect},
	{Mode: ModeSquare},
	{Mode: ModeContour},
	{Mode: ModeContour, SimplifyTolerance: 2.5},
	{Mode: ModeRect, Alpha: AlphaDrop, Tolerance: 48},
	{Mode: ModeRect, MaxColors: 4},
	{Mode: ModeRect, MaxColors: -1, Workers: 3},
	{Mode: ModeSquare, Quantizer: QuantizerNone, MaxColors: 2},
}

// FuzzConvert feeds arbitrary bytes through the whole pipeline.
//
// Any outcome is acceptable except a panic: a malformed or hostile file has to
// come back as an error. The first byte of the input picks the options, so one
// corpus entry exercises every mode rather than only the default one.
func FuzzConvert(f *testing.F) {
	sprite := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for y := range 8 {
		for x := range 8 {
			sprite.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 32 & 0xff), G: uint8(y * 32 & 0xff), A: 0xff})
		}
	}

	encoders := []func(io.Writer, image.Image) error{
		png.Encode,
		func(w io.Writer, m image.Image) error { return gif.Encode(w, m, nil) },
		func(w io.Writer, m image.Image) error { return jpeg.Encode(w, m, nil) },
	}

	for i, encode := range encoders {
		var buf bytes.Buffer
		if err := encode(&buf, sprite); err != nil {
			f.Fatalf("encode seed %d: %v", i, err)
		}

		for m := range fuzzModes {
			f.Add(append([]byte{byte(m & 0xff)}, buf.Bytes()...))
		}
	}

	f.Add([]byte{})
	f.Add([]byte{0})
	f.Add([]byte("\x89PNG\r\n\x1a\n"))
	f.Add([]byte("\x00\x89PNG\r\n\x1a\nIHDR"))
	f.Add([]byte("\x02GIF89a\x01\x00\x01\x00\x80\x00\x00"))
	f.Add([]byte("\x01\xff\xd8\xff\xe0\x00\x10JFIF"))
	f.Add([]byte("\x03RIFF\x24\x00\x00\x00WEBPVP8L"))

	f.Fuzz(func(t *testing.T, data []byte) {
		opts := fuzzModes[0]
		if len(data) > 0 {
			opts = fuzzModes[int(data[0])%len(fuzzModes)]
			data = data[1:]
		}

		res, err := Convert(t.Context(), io.Discard, bytes.NewReader(data), opts)
		if err != nil {
			return
		}

		if res.Width <= 0 || res.Height <= 0 {
			t.Fatalf("a successful conversion reported size %dx%d", res.Width, res.Height)
		}

		if res.Width > MaxDimension || res.Height > MaxDimension {
			t.Fatalf("a successful conversion reported size %dx%d, over the cap", res.Width, res.Height)
		}

		if res.Bytes <= 0 {
			t.Fatalf("a successful conversion reported %d bytes", res.Bytes)
		}
	})
}

// FuzzSVGIsParseable converts arbitrary small images and requires the result
// to parse back and, in the pixel-exact modes, to cover the source exactly.
// It reaches the geometry code with shapes no hand-written fixture would.
func FuzzSVGIsParseable(f *testing.F) {
	f.Add([]byte{3, 3, 0, 1, 2, 3, 4, 5, 6, 7, 8})
	f.Add([]byte{1, 8, 1, 0, 1, 0, 1, 0, 1, 0})
	f.Add([]byte{5, 5, 0, 0, 0, 1, 1, 1, 0, 0, 0, 2, 2, 2, 3, 3, 3, 0, 1, 2, 3, 4, 5, 6, 7})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 3 {
			return
		}

		w, h := int(data[0])%24+1, int(data[1])%24+1
		cells := data[2:]

		if len(cells) < w*h {
			return
		}

		pal := testPalette(5)
		img := image.NewNRGBA(image.Rect(0, 0, w, h))

		for y := range h {
			for x := range w {
				k := int(cells[y*w+x]) % (len(pal) + 1)
				if k < len(pal) {
					img.SetNRGBA(x, y, pal[k])
				}
			}
		}

		for _, mode := range []Mode{ModeSquare, ModeRect} {
			var buf bytes.Buffer

			if _, err := ConvertImage(t.Context(), &buf, img, Options{Mode: mode}); err != nil {
				t.Fatalf("convert %s: %v", mode, err)
			}

			comparePixels(t, coverage(t, parseSVG(t, buf.String())), sourceCoverage(t, img, AlphaOpacity))
		}

		var buf bytes.Buffer

		if _, err := ConvertImage(t.Context(), &buf, img, Options{Mode: ModeContour}); err != nil {
			t.Fatalf("convert contour: %v", err)
		}

		want := make(map[color.NRGBA]int)
		for _, c := range sourceCoverage(t, img, AlphaOpacity) {
			want[c]++
		}

		for _, g := range parseSVG(t, buf.String()).groups {
			area := 0
			for _, l := range g.loops {
				area += area2(l)
			}

			if area != 2*want[g.fill] {
				t.Fatalf("colour %v encloses %d pixels, want %d", g.fill, area/2, want[g.fill])
			}
		}
	})
}
