package spritesheet

import (
	"archive/zip"
	"bytes"
	"image"
	"image/png"
	"io"
	"os"
	"testing"
)

// gridOpts is the fixture's own grid, as options.
func gridOpts() SliceOptions {
	return SliceOptions{
		Mode: ModeGrid,
		Grid: Grid{
			CellW:   fixtureCell,
			CellH:   fixtureCell,
			Margin:  fixtureMargin,
			Spacing: fixtureSpacing,
		},
	}
}

// TestSliceGridPixelsExact is the assert that matters most in S7.1: the eight
// frames are byte-identical to the eight source cells, in row-major order.
// Every off-by-one in the margin, the spacing or the ordering shows up here.
func TestSliceGridPixelsExact(t *testing.T) {
	sheet := fixtureSheet()

	frames, err := Slice(t.Context(), sheet, gridOpts())
	if err != nil {
		t.Fatalf("Slice: %v", err)
	}

	if len(frames) != fixtureCols*fixtureRows {
		t.Fatalf("got %d frames, want %d", len(frames), fixtureCols*fixtureRows)
	}

	for i, f := range frames {
		want := fixtureCellImage(i)

		for y := range fixtureCell {
			for x := range fixtureCell {
				gr, gg, gb, ga := sheet.At(f.Rect.Min.X+x, f.Rect.Min.Y+y).RGBA()
				wr, wg, wb, wa := want.At(x, y).RGBA()

				if gr != wr || gg != wg || gb != wb || ga != wa {
					t.Fatalf("frame %d pixel (%d,%d): sheet has %v, cell %d has %v",
						i, x, y, sheet.At(f.Rect.Min.X+x, f.Rect.Min.Y+y), i, want.At(x, y))
				}
			}
		}
	}
}

// TestSliceGridUntrimmedMetadata checks that without trim every frame reports
// itself untrimmed with a zero offset. An importer reads those fields even
// when nothing was trimmed, so they have to be right in the boring case too.
func TestSliceGridUntrimmedMetadata(t *testing.T) {
	frames, err := Slice(t.Context(), fixtureSheet(), gridOpts())
	if err != nil {
		t.Fatalf("Slice: %v", err)
	}

	for i, f := range frames {
		if f.Trimmed {
			t.Errorf("frame %d reports itself trimmed", i)
		}

		if f.Offset != (image.Point{}) {
			t.Errorf("frame %d has offset %v, want the zero point", i, f.Offset)
		}

		if f.Source != image.Pt(fixtureCell, fixtureCell) {
			t.Errorf("frame %d has source size %v, want %v",
				i, f.Source, image.Pt(fixtureCell, fixtureCell))
		}
	}
}

// TestSliceTrimRecordsOffset is the trim contract. The fixture's last cell is
// a 2x2 core inside a 3-pixel transparent border, so a correct trim reports an
// offset of exactly (3,3) and keeps the untrimmed cell as the source size —
// which is what lets an importer put the frame back where it belongs instead
// of letting the animation jitter.
func TestSliceTrimRecordsOffset(t *testing.T) {
	opts := gridOpts()
	opts.Trim = true

	frames, err := Slice(t.Context(), fixtureSheet(), opts)
	if err != nil {
		t.Fatalf("Slice: %v", err)
	}

	f := frames[fixtureTrimmedCell]

	if !f.Trimmed {
		t.Error("the trimmed cell does not report itself trimmed")
	}

	if want := image.Pt(fixtureTrimBorder, fixtureTrimBorder); f.Offset != want {
		t.Errorf("offset is %v, want %v", f.Offset, want)
	}

	if want := image.Pt(fixtureCell, fixtureCell); f.Source != want {
		t.Errorf("source size is %v, want the untrimmed cell %v", f.Source, want)
	}

	if got := f.Rect.Dx(); got != fixtureTrimCoreWidth {
		t.Errorf("trimmed width is %d, want %d", got, fixtureTrimCoreWidth)
	}

	if got := f.Rect.Dy(); got != fixtureTrimCoreWidth {
		t.Errorf("trimmed height is %d, want %d", got, fixtureTrimCoreWidth)
	}

	// The trimmed rectangle must sit where the core actually is in the sheet.
	at := fixtureCellOrigin(fixtureTrimmedCell)
	if want := at.Add(image.Pt(fixtureTrimBorder, fixtureTrimBorder)); f.Rect.Min != want {
		t.Errorf("trimmed rectangle starts at %v, want %v", f.Rect.Min, want)
	}
}

// TestSliceTrimLeavesOpaqueFramesAlone checks trim is a no-op on a frame that
// fills its cell, rather than shaving a pixel off everything.
func TestSliceTrimLeavesOpaqueFramesAlone(t *testing.T) {
	opts := gridOpts()
	opts.Trim = true

	frames, err := Slice(t.Context(), fixtureSheet(), opts)
	if err != nil {
		t.Fatalf("Slice: %v", err)
	}

	for i, f := range frames {
		if i == fixtureTrimmedCell {
			continue
		}

		if f.Trimmed {
			t.Errorf("frame %d is fully opaque but reports itself trimmed", i)
		}

		if f.Rect.Dx() != fixtureCell || f.Rect.Dy() != fixtureCell {
			t.Errorf("frame %d was trimmed to %v, want the whole %dx%d cell",
				i, f.Rect, fixtureCell, fixtureCell)
		}
	}
}

// TestSplitDeterministic checks two runs over the same sheet produce
// byte-identical archives. The frames go through a ZIP, which records a
// modification time per entry, so this also pins that the time is fixed rather
// than taken from the clock — a conversion that is not reproducible cannot be
// golden-tested and cannot be cached.
func TestSplitDeterministic(t *testing.T) {
	data, err := os.ReadFile(fixturePath(t))
	if err != nil {
		t.Fatalf("read the fixture: %v", err)
	}

	run := func() []byte {
		t.Helper()

		var out bytes.Buffer
		if _, err := Split(t.Context(), &out, bytes.NewReader(data), gridOpts()); err != nil {
			t.Fatalf("Split: %v", err)
		}

		return out.Bytes()
	}

	first, second := run(), run()

	if !bytes.Equal(first, second) {
		t.Fatalf("two runs differ: %d bytes and %d bytes", len(first), len(second))
	}
}

// TestSplitArchiveContents checks the archive holds one PNG per frame, named
// in order, and that each one decodes to the matching source cell. This is the
// end-to-end version of TestSliceGridPixelsExact: it proves the bytes that
// actually reach the user are the right ones, not just the rectangles.
func TestSplitArchiveContents(t *testing.T) {
	data, err := os.ReadFile(fixturePath(t))
	if err != nil {
		t.Fatalf("read the fixture: %v", err)
	}

	var out bytes.Buffer

	res, err := Split(t.Context(), &out, bytes.NewReader(data), gridOpts())
	if err != nil {
		t.Fatalf("Split: %v", err)
	}

	if res.Frames != fixtureCols*fixtureRows {
		t.Errorf("the result reports %d frames, want %d", res.Frames, fixtureCols*fixtureRows)
	}

	zr, err := zip.NewReader(bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("open the archive: %v", err)
	}

	if len(zr.File) != fixtureCols*fixtureRows {
		t.Fatalf("the archive holds %d entries, want %d", len(zr.File), fixtureCols*fixtureRows)
	}

	for i, entry := range zr.File {
		rc, err := entry.Open()
		if err != nil {
			t.Fatalf("open entry %q: %v", entry.Name, err)
		}

		raw, err := io.ReadAll(rc)
		_ = rc.Close()

		if err != nil {
			t.Fatalf("read entry %q: %v", entry.Name, err)
		}

		got, err := png.Decode(bytes.NewReader(raw))
		if err != nil {
			t.Fatalf("decode entry %q: %v", entry.Name, err)
		}

		if got.Bounds() != image.Rect(0, 0, fixtureCell, fixtureCell) {
			t.Fatalf("entry %q is %v, want an %dx%d frame",
				entry.Name, got.Bounds(), fixtureCell, fixtureCell)
		}

		want := fixtureCellImage(i)

		for y := range fixtureCell {
			for x := range fixtureCell {
				gr, gg, gb, ga := got.At(x, y).RGBA()
				wr, wg, wb, wa := want.At(x, y).RGBA()

				if gr != wr || gg != wg || gb != wb || ga != wa {
					t.Fatalf("entry %q pixel (%d,%d) is %v, want %v",
						entry.Name, x, y, got.At(x, y), want.At(x, y))
				}
			}
		}
	}
}
