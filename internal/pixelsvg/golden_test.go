package pixelsvg

import (
	"bytes"
	"flag"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// update rewrites the fixtures and the golden files instead of comparing
// against them: go test ./internal/pixelsvg/... -update.
var update = flag.Bool("update", false, "rewrite testdata fixtures and golden files")

// spriteRows is the hand-made 8x8 sprite every golden test runs on. It has a
// fully transparent background, a half-transparent row, two regions of the
// same colour that are not connected, and a ring with a hole in it, so all
// three modes have something to disagree about.
var spriteRows = [8]string{
	"..KKKK..",
	".KRRRRK.",
	"KRYYYYRK",
	"KRY..YRK",
	"KRY..YRK",
	"KRYYYYRK",
	".KHHHHK.",
	"..KKKK..",
}

// spritePalette maps every character of spriteRows to its colour. H is R at
// half alpha, so the sprite holds two palette entries that differ only in
// alpha and must not be merged.
var spritePalette = map[byte]color.NRGBA{
	'.': {},
	'K': {R: 0x1a, G: 0x1a, B: 0x2e, A: 0xff},
	'R': {R: 0xe9, G: 0x45, B: 0x60, A: 0xff},
	'Y': {R: 0xf5, G: 0xd5, B: 0x47, A: 0xff},
	'H': {R: 0xe9, G: 0x45, B: 0x60, A: 0x80},
}

// spriteImage builds the sprite from spriteRows.
func spriteImage(t *testing.T) *image.NRGBA {
	t.Helper()

	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))

	for y, row := range spriteRows {
		if len(row) != 8 {
			t.Fatalf("sprite row %d is %d characters, want 8", y, len(row))
		}

		for x := range 8 {
			c, ok := spritePalette[row[x]]
			if !ok {
				t.Fatalf("sprite row %d column %d has unknown character %q", y, x, row[x])
			}

			img.SetNRGBA(x, y, c)
		}
	}

	return img
}

// spritePNG returns the path of the committed PNG fixture, writing it first
// when -update was given.
func spritePNG(t *testing.T) string {
	t.Helper()

	path := filepath.Join("testdata", "sprite8x8.png")

	if *update {
		var buf bytes.Buffer
		if err := png.Encode(&buf, spriteImage(t)); err != nil {
			t.Fatalf("encode fixture: %v", err)
		}

		if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
	}

	return path
}

// TestSpriteFixtureMatchesSource decodes the committed PNG and checks it is
// still the sprite the other tests assume. Without this the golden files could
// agree with each other while the fixture had drifted.
func TestSpriteFixtureMatchesSource(t *testing.T) {
	data, err := os.ReadFile(spritePNG(t))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	img, err := decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode fixture: %v", err)
	}

	got := toNRGBA(img)
	want := spriteImage(t)

	if got.Rect != want.Rect {
		t.Fatalf("fixture bounds = %v, want %v", got.Rect, want.Rect)
	}

	for y := range 8 {
		for x := range 8 {
			if g, w := got.NRGBAAt(x, y), want.NRGBAAt(x, y); g != w {
				t.Errorf("fixture pixel (%d,%d) = %v, want %v", x, y, g, w)
			}
		}
	}
}

// goldenModes is every mode with the golden file it is compared against.
var goldenModes = []struct {
	mode Mode
	file string
}{
	{ModeSquare, "sprite8x8.square.svg"},
	{ModeRect, "sprite8x8.rect.svg"},
	{ModeContour, "sprite8x8.contour.svg"},
}

// TestGolden checks all three modes byte for byte against a committed file.
func TestGolden(t *testing.T) {
	src := spriteImage(t)

	for _, tc := range goldenModes {
		t.Run(string(tc.mode), func(t *testing.T) {
			var buf bytes.Buffer

			res, err := ConvertImage(t.Context(), &buf, src, Options{Mode: tc.mode})
			if err != nil {
				t.Fatalf("convert: %v", err)
			}

			if res.Width != 8 || res.Height != 8 {
				t.Errorf("result size = %dx%d, want 8x8", res.Width, res.Height)
			}

			if res.Colors != 4 {
				t.Errorf("result colours = %d, want 4", res.Colors)
			}

			if res.Bytes != int64(buf.Len()) {
				t.Errorf("result bytes = %d, want %d", res.Bytes, buf.Len())
			}

			path := filepath.Join("testdata", tc.file)

			if *update {
				if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
					t.Fatalf("write golden: %v", err)
				}
			}

			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden: %v", err)
			}

			if !bytes.Equal(buf.Bytes(), want) {
				t.Errorf("output does not match %s\n got: %s\nwant: %s", tc.file, buf.Bytes(), want)
			}
		})
	}
}

// TestDeterminism converts the same input twice, and then again with a
// different worker count, and requires byte-identical output every time.
func TestDeterminism(t *testing.T) {
	src := spriteImage(t)

	for _, tc := range goldenModes {
		t.Run(string(tc.mode), func(t *testing.T) {
			var first bytes.Buffer

			if _, err := ConvertImage(t.Context(), &first, src, Options{Mode: tc.mode}); err != nil {
				t.Fatalf("convert: %v", err)
			}

			for _, workers := range []int{0, 1, 2, 3, 8, 64} {
				var again bytes.Buffer

				opts := Options{Mode: tc.mode, Workers: workers}
				if _, err := ConvertImage(t.Context(), &again, src, opts); err != nil {
					t.Fatalf("convert with %d workers: %v", workers, err)
				}

				if !bytes.Equal(first.Bytes(), again.Bytes()) {
					t.Fatalf("output with %d workers differs from the first conversion", workers)
				}
			}
		})
	}
}

// TestDeterminismLargerImage repeats the determinism check on an image tall
// enough to be split across bands however many workers there are, so band
// stitching is actually exercised.
func TestDeterminismLargerImage(t *testing.T) {
	src := generatedImage(301, 257, 37, 5)

	for _, tc := range goldenModes {
		t.Run(string(tc.mode), func(t *testing.T) {
			var first bytes.Buffer

			opts := Options{Mode: tc.mode, Workers: 1}
			if _, err := ConvertImage(t.Context(), &first, src, opts); err != nil {
				t.Fatalf("convert: %v", err)
			}

			for _, workers := range []int{2, 4, 7, 16} {
				var again bytes.Buffer

				opts := Options{Mode: tc.mode, Workers: workers}
				if _, err := ConvertImage(t.Context(), &again, src, opts); err != nil {
					t.Fatalf("convert with %d workers: %v", workers, err)
				}

				if !bytes.Equal(first.Bytes(), again.Bytes()) {
					t.Fatalf("output with %d workers differs from the single-worker conversion", workers)
				}
			}
		})
	}
}

// TestHeaderAndGrouping checks the document shell and the per-colour grouping,
// which the golden files pin but do not explain.
func TestHeaderAndGrouping(t *testing.T) {
	var buf bytes.Buffer

	opts := Options{Mode: ModeRect, Scale: 8}
	if _, err := ConvertImage(t.Context(), &buf, spriteImage(t), opts); err != nil {
		t.Fatalf("convert: %v", err)
	}

	out := buf.String()

	for _, want := range []string{
		`<svg xmlns="http://www.w3.org/2000/svg" width="64" height="64" viewBox="0 0 8 8" shape-rendering="crispEdges">`,
		`<g fill="#e94560" fill-opacity="0.502">`,
		`<g fill="#e94560">`,
		`<g fill="#f5d547">`,
	} {
		if !bytes.Contains(buf.Bytes(), []byte(want)) {
			t.Errorf("output does not contain %q\n%s", want, out)
		}
	}

	for _, unwanted := range []string{"xlink", "<!--", "sodipodi", "inkscape", "<defs>"} {
		if bytes.Contains(buf.Bytes(), []byte(unwanted)) {
			t.Errorf("output contains %q, which no mode should emit in rect mode", unwanted)
		}
	}
}

// TestAlphaDrop checks that dropping partial alpha removes the colour group
// entirely rather than flattening it.
func TestAlphaDrop(t *testing.T) {
	var buf bytes.Buffer

	opts := Options{Mode: ModeRect, Alpha: AlphaDrop}

	res, err := ConvertImage(t.Context(), &buf, spriteImage(t), opts)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}

	if res.Colors != 3 {
		t.Errorf("colours = %d, want 3", res.Colors)
	}

	if bytes.Contains(buf.Bytes(), []byte("fill-opacity")) {
		t.Error("output has a fill-opacity although partial alpha was dropped")
	}
}
