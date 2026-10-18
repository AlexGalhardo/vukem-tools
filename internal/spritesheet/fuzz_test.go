package spritesheet

import (
	"bytes"
	"image"
	"image/png"
	"testing"
)

// FuzzSplit feeds arbitrary bytes and arbitrary grid options through the whole
// split path.
//
// The contract is only that it never panics: almost every input is not an
// image, and an error is the right answer for those. What this is hunting is
// the other kind of bug — an index computed from a header, a grid that
// resolves to a cell outside the sheet, a slice built from a count derived
// from attacker-controlled numbers.
func FuzzSplit(f *testing.F) {
	valid := func() []byte {
		var buf bytes.Buffer
		if err := png.Encode(&buf, fixtureSheet()); err != nil {
			f.Fatalf("encode the seed sheet: %v", err)
		}

		return buf.Bytes()
	}()

	// A real sheet with its real grid, so the corpus starts from something
	// that reaches the slicing rather than bouncing off the decoder.
	f.Add(valid, fixtureCell, fixtureCell, fixtureMargin, fixtureSpacing, false)
	f.Add(valid, 1, 1, 0, 0, true)
	f.Add(valid, fixtureCell, fixtureCell, 0, 0, true)
	f.Add(valid, 0, 0, 0, 0, false)
	f.Add(valid, -1, -1, -1, -1, false)
	f.Add(valid, fixtureWidth, fixtureHeight, 0, 0, false)
	f.Add([]byte("not an image at all"), 8, 8, 0, 0, false)
	f.Add([]byte{}, 8, 8, 0, 0, false)
	f.Add(headerOnlyPNG(65535, 65535), 8, 8, 0, 0, false)
	f.Add(headerOnlyPNG(0, 0), 8, 8, 0, 0, false)

	f.Fuzz(func(t *testing.T, data []byte, cellW, cellH, margin, spacing int, trim bool) {
		// Split is expected to fail on almost all of this. A panic, a hang or
		// a write to the output after an error is what would be a finding.
		var out bytes.Buffer

		res, err := Split(t.Context(), &out, bytes.NewReader(data), SliceOptions{
			Mode: ModeGrid,
			Grid: Grid{CellW: cellW, CellH: cellH, Margin: margin, Spacing: spacing},
			Trim: trim,
		})
		if err != nil {
			return
		}

		// On success the invariants have to hold, or a later stage — a
		// metadata writer, an engine importer — would be handed nonsense.
		if res.Frames != len(res.Atlas) {
			t.Fatalf("the result reports %d frames but carries %d in the atlas",
				res.Frames, len(res.Atlas))
		}

		if res.Frames > MaxFrames {
			t.Fatalf("produced %d frames, over the cap of %d", res.Frames, MaxFrames)
		}

		sheet := res.Sheet

		for i, fr := range res.Atlas {
			if fr.Rect.Dx() <= 0 || fr.Rect.Dy() <= 0 {
				t.Fatalf("frame %d is empty: %v", i, fr.Rect)
			}

			if fr.Rect.Min.X < 0 || fr.Rect.Min.Y < 0 ||
				fr.Rect.Max.X > sheet.X || fr.Rect.Max.Y > sheet.Y {
				t.Fatalf("frame %d at %v is outside the %dx%d sheet", i, fr.Rect, sheet.X, sheet.Y)
			}

			if fr.Source.X <= 0 || fr.Source.Y <= 0 {
				t.Fatalf("frame %d has source size %v", i, fr.Source)
			}

			// A trimmed frame's content must fit inside the frame it was
			// trimmed from, or an importer re-inflating it would draw outside
			// its own cell.
			if fr.Offset.X+fr.Rect.Dx() > fr.Source.X || fr.Offset.Y+fr.Rect.Dy() > fr.Source.Y {
				t.Fatalf("frame %d: offset %v plus size %v exceeds the source %v",
					i, fr.Offset, fr.Rect.Size(), fr.Source)
			}
		}
	})
}

// FuzzDetect feeds arbitrary bytes through the automatic path, which is the
// one with the union-find in it: a labelling bug is an out-of-range index, and
// that is exactly what a fuzzer finds and a golden test does not.
func FuzzDetect(f *testing.F) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, irregularSheet()); err != nil {
		f.Fatalf("encode the seed sheet: %v", err)
	}

	f.Add(buf.Bytes(), 0, 0)
	f.Add(buf.Bytes(), 4, 0)
	f.Add(buf.Bytes(), 1<<20, 255)
	f.Add([]byte("not an image"), 0, 0)

	f.Fuzz(func(t *testing.T, data []byte, minFrame, tolerance int) {
		img, err := decodeLimited(bytes.NewReader(data), MaxInputBytes)
		if err != nil {
			return
		}

		res, err := Detect(t.Context(), img, SliceOptions{
			Mode:                ModeAuto,
			MinFrame:            minFrame,
			BackgroundTolerance: tolerance,
		})
		if err != nil {
			return
		}

		b := img.Bounds()

		for i, fr := range res.Frames {
			if !fr.Rect.In(b) {
				t.Fatalf("frame %d at %v is outside the sheet's bounds %v", i, fr.Rect, b)
			}

			if fr.Rect.Dx() <= 0 || fr.Rect.Dy() <= 0 {
				t.Fatalf("frame %d is empty: %v", i, fr.Rect)
			}
		}

		if res.Dropped < 0 {
			t.Fatalf("the dropped count is %d", res.Dropped)
		}
	})
}

// FuzzGridFrames fuzzes the geometry alone, with no decoding in the way, so
// the fuzzer spends its budget on the arithmetic rather than on rediscovering
// that random bytes are not a PNG.
func FuzzGridFrames(f *testing.F) {
	f.Add(40, 20, 8, 8, 1, 2)
	f.Add(1, 1, 1, 1, 0, 0)
	f.Add(0, 0, 0, 0, 0, 0)
	f.Add(4096, 4096, 1, 1, 0, 0)
	f.Add(-1, -1, 8, 8, 0, 0)

	f.Fuzz(func(t *testing.T, w, h, cellW, cellH, margin, spacing int) {
		frames, err := GridFrames(w, h, Grid{
			CellW: cellW, CellH: cellH, Margin: margin, Spacing: spacing,
		})
		if err != nil {
			return
		}

		if len(frames) > MaxFrames {
			t.Fatalf("produced %d frames, over the cap of %d", len(frames), MaxFrames)
		}

		sheet := image.Rect(0, 0, w, h)

		for i, fr := range frames {
			if !fr.Rect.In(sheet) {
				t.Fatalf("frame %d at %v is outside a %dx%d sheet", i, fr.Rect, w, h)
			}
		}

		// Every frame must be the same size, because that is what a grid
		// means; a grid that returned ragged cells would silently corrupt
		// every metadata format.
		for i := 1; i < len(frames); i++ {
			if frames[i].Rect.Size() != frames[0].Rect.Size() {
				t.Fatalf("frame %d is %v but frame 0 is %v: the grid is not uniform",
					i, frames[i].Rect.Size(), frames[0].Rect.Size())
			}
		}
	})
}
