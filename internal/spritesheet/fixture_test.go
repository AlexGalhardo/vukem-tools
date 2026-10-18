package spritesheet

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// The committed fixture's shape. A margin and a spacing that differ, and a
// column count that differs from the row count, so a transposed or
// off-by-a-margin implementation cannot pass by symmetry.
const (
	fixtureCell    = 8
	fixtureCols    = 4
	fixtureRows    = 2
	fixtureMargin  = 1
	fixtureSpacing = 2

	fixtureWidth  = fixtureMargin*2 + fixtureCols*fixtureCell + (fixtureCols-1)*fixtureSpacing
	fixtureHeight = fixtureMargin*2 + fixtureRows*fixtureCell + (fixtureRows-1)*fixtureSpacing

	// fixtureTrimmedCell is the index of the one cell that is a 2x2 core
	// inside a 3-pixel transparent border, which is what the trim test reads.
	fixtureTrimmedCell   = 7
	fixtureTrimBorder    = 3
	fixtureTrimCoreWidth = fixtureCell - 2*fixtureTrimBorder
)

// asError is errors.As with the target's type inferred, so a test can say
// which error type it expects without naming it twice.
func asError[T error](err error, target *T) bool { return errors.As(err, target) }

// fixturePalette is one colour per cell. The multipliers are odd so no two
// cells share a colour for any count up to 256.
func fixturePalette(n int) []color.NRGBA {
	out := make([]color.NRGBA, n)
	for i := range out {
		out[i] = color.NRGBA{
			R: uint8((i*53 + 17) & 0xff),
			G: uint8((i*97 + 41) & 0xff),
			B: uint8((i*31 + 79) & 0xff),
			A: 0xff,
		}
	}

	return out
}

// fixtureCellImage returns the content of one cell: a flat colour with a
// white diagonal of i+1 pixels, so every cell is distinguishable from every
// other and a frame delivered in the wrong order fails the pixel comparison.
//
// The cell at fixtureTrimmedCell is instead a small opaque core inside a
// transparent border, which is the only cell the trim assertions look at.
func fixtureCellImage(i int) *image.NRGBA {
	cell := image.NewNRGBA(image.Rect(0, 0, fixtureCell, fixtureCell))

	if i == fixtureTrimmedCell {
		core := color.NRGBA{R: 0x20, G: 0xd0, B: 0x60, A: 0xff}

		for y := fixtureTrimBorder; y < fixtureCell-fixtureTrimBorder; y++ {
			for x := fixtureTrimBorder; x < fixtureCell-fixtureTrimBorder; x++ {
				cell.SetNRGBA(x, y, core)
			}
		}

		return cell
	}

	base := fixturePalette(fixtureCols * fixtureRows)[i]

	for y := range fixtureCell {
		for x := range fixtureCell {
			cell.SetNRGBA(x, y, base)
		}
	}

	white := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	for d := 0; d <= i && d < fixtureCell; d++ {
		cell.SetNRGBA(d, d, white)
	}

	return cell
}

// fixtureCellOrigin is where cell i's top-left pixel sits in the sheet.
func fixtureCellOrigin(i int) image.Point {
	col, row := i%fixtureCols, i/fixtureCols

	return image.Pt(
		fixtureMargin+col*(fixtureCell+fixtureSpacing),
		fixtureMargin+row*(fixtureCell+fixtureSpacing),
	)
}

// fixtureSheet builds the sheet from its cells. The gutter — the margin and
// the spacing — is left fully transparent, which is what a real sheet looks
// like and what the auto detector has to see through.
func fixtureSheet() *image.NRGBA {
	sheet := image.NewNRGBA(image.Rect(0, 0, fixtureWidth, fixtureHeight))

	for i := range fixtureCols * fixtureRows {
		at := fixtureCellOrigin(i)
		cell := fixtureCellImage(i)

		for y := range fixtureCell {
			for x := range fixtureCell {
				sheet.SetNRGBA(at.X+x, at.Y+y, cell.NRGBAAt(x, y))
			}
		}
	}

	return sheet
}

// fixturePath returns the path of the committed PNG, writing it on first run
// so the fixture is reproducible from the generator above rather than being an
// opaque blob in the repository.
func fixturePath(t *testing.T) string {
	t.Helper()

	path := filepath.Join("testdata", "sheet4x2.png")

	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("create testdata: %v", err)
		}

		var buf bytes.Buffer
		if err := png.Encode(&buf, fixtureSheet()); err != nil {
			t.Fatalf("encode the fixture: %v", err)
		}

		if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
			t.Fatalf("write the fixture: %v", err)
		}
	}

	return path
}

// TestFixtureMatchesSource decodes the committed PNG and checks it is still
// the sheet every other test assumes.
//
// Without this the goldens could drift from the generator and the suite would
// keep passing while asserting something nobody wrote. internal/pixelsvg has
// the same guard for the same reason.
func TestFixtureMatchesSource(t *testing.T) {
	data, err := os.ReadFile(fixturePath(t))
	if err != nil {
		t.Fatalf("read the fixture: %v", err)
	}

	got, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode the fixture: %v", err)
	}

	want := fixtureSheet()

	if got.Bounds() != want.Bounds() {
		t.Fatalf("the fixture is %v, the generator makes %v", got.Bounds(), want.Bounds())
	}

	for y := range fixtureHeight {
		for x := range fixtureWidth {
			gr, gg, gb, ga := got.At(x, y).RGBA()
			wr, wg, wb, wa := want.At(x, y).RGBA()

			if gr != wr || gg != wg || gb != wb || ga != wa {
				t.Fatalf("pixel (%d,%d) differs: fixture has %v, the generator makes %v",
					x, y, got.At(x, y), want.At(x, y))
			}
		}
	}
}
