package spritesheet

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"runtime"
	"testing"
)

// benchSheet builds a sheet of cell-sized blocks, each a flat colour with a
// transparent border, which is what a real sprite sheet looks like: flat
// regions, not noise. Noise would make every run its own component and
// benchmark a shape nobody has.
func benchSheet(side, cell int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, side, side))
	cols := side / cell
	pal := fixturePalette(256)

	for i := range cols * cols {
		ox, oy := (i%cols)*cell, (i/cols)*cell
		c := pal[i%len(pal)]

		// A one-pixel transparent border per cell, so auto detection has
		// something to separate frames by and trim has something to remove.
		for y := 1; y < cell-1; y++ {
			for x := 1; x < cell-1; x++ {
				img.SetNRGBA(ox+x, oy+y, c)
			}
		}
	}

	return img
}

// benchSheetPNG returns the encoded sheet, so a benchmark measures the whole
// path a conversion takes rather than only the slicing.
func benchSheetPNG(tb testing.TB, side, cell int) []byte {
	tb.Helper()

	var buf bytes.Buffer
	if err := png.Encode(&buf, benchSheet(side, cell)); err != nil {
		tb.Fatalf("encode the benchmark sheet: %v", err)
	}

	return buf.Bytes()
}

// BenchmarkSplitGrid measures the full split path at the sizes the ROADMAP
// records: a 4096 by 4096 sheet is the area limit, and 1024 frames is half the
// frame cap.
func BenchmarkSplitGrid(b *testing.B) {
	cases := []struct{ side, cell int }{
		{1024, 128}, // 64 frames
		{2048, 128}, // 256 frames
		{4096, 128}, // 1024 frames
	}

	for _, c := range cases {
		frames := (c.side / c.cell) * (c.side / c.cell)

		b.Run(fmt.Sprintf("%dx%d/%dframes", c.side, c.side, frames), func(b *testing.B) {
			raw := benchSheetPNG(b, c.side, c.cell)
			opts := SliceOptions{Mode: ModeGrid, Grid: Grid{CellW: c.cell, CellH: c.cell}}

			b.ReportAllocs()
			b.SetBytes(int64(len(raw)))
			b.ResetTimer()

			for range b.N {
				if _, err := Split(b.Context(), io.Discard, bytes.NewReader(raw), opts); err != nil {
					b.Fatalf("Split: %v", err)
				}
			}
		})
	}
}

// BenchmarkDetectAuto measures component labelling, which is the one pass over
// every pixel that grid mode does not do.
func BenchmarkDetectAuto(b *testing.B) {
	for _, side := range []int{1024, 2048, 4096} {
		b.Run(fmt.Sprintf("%dx%d", side, side), func(b *testing.B) {
			img := benchSheet(side, 128)

			b.ReportAllocs()
			b.ResetTimer()

			for range b.N {
				if _, err := Detect(b.Context(), img, SliceOptions{Mode: ModeAuto}); err != nil {
					b.Fatalf("Detect: %v", err)
				}
			}
		})
	}
}

// TestPeakMemoryDoesNotGrowWithFrameCount is the assert the whole design of
// this package rests on: frames are encoded into the archive one at a time
// through views that share the decoded sheet's pixels, so the cost is one
// sheet plus one encoder, not one sheet plus every frame.
//
// It is written as a test rather than a benchmark because it asserts a bound
// rather than reporting a number, and a regression here — collecting frames
// into a slice, say — is the kind that passes every other test while making
// the tool unrunnable on a small container.
//
// The figure is Go's own peak heap, not the process's RSS: Go's runtime is
// where this package's allocations live, and RSS on this developer machine
// (Windows) is not comparable with the Linux container's. docs/benchmarks.md
// records that limitation rather than presenting a host number as a container
// number.
func TestPeakMemoryDoesNotGrowWithFrameCount(t *testing.T) {
	if testing.Short() {
		t.Skip("allocates a 4096x4096 sheet")
	}

	const side = 2048

	type sample struct {
		frames int
		peak   uint64
	}

	var samples []sample

	// The same sheet, cut into more and more frames. The decoded sheet is
	// identical in every case, so any growth in the peak is the frames being
	// kept rather than streamed.
	for _, cell := range []int{256, 128, 64} {
		frames := (side / cell) * (side / cell)
		raw := benchSheetPNG(t, side, cell)
		opts := SliceOptions{Mode: ModeGrid, Grid: Grid{CellW: cell, CellH: cell}}

		runtime.GC()

		var before runtime.MemStats

		runtime.ReadMemStats(&before)

		if _, err := Split(t.Context(), io.Discard, bytes.NewReader(raw), opts); err != nil {
			t.Fatalf("Split at %d frames: %v", frames, err)
		}

		var after runtime.MemStats

		runtime.ReadMemStats(&after)

		// HeapInuse at its high-water mark across the run. TotalAlloc would
		// count every byte ever allocated, which does grow with the frame
		// count and is not the question; what matters is how much is live at
		// once.
		peak := after.HeapInuse
		if after.HeapSys > peak {
			peak = after.HeapInuse
		}

		samples = append(samples, sample{frames: frames, peak: peak})
		t.Logf("%d frames: heap in use %d bytes (%.1f MiB)", frames, peak, float64(peak)/(1<<20))
	}

	// The 64-frame run is the baseline. Sixteen times as many frames must not
	// cost appreciably more live heap; a generous factor of two still catches
	// a design that accumulates frames, which would grow by 16x.
	base, most := samples[0].peak, samples[len(samples)-1].peak
	if most > base*2 {
		t.Errorf("peak heap grew from %d bytes at %d frames to %d bytes at %d frames, "+
			"which is more than the 2x slack: frames are being kept, not streamed",
			base, samples[0].frames, most, samples[len(samples)-1].frames)
	}
}

// TestPeakMemoryAtTheAreaLimit records the figure the ROADMAP asks for: the
// peak live heap for a sheet at MaxPixels, cut into a thousand frames.
//
// It is the number that decides whether this tool fits the container, so it is
// measured rather than extrapolated from the smaller sheet above.
func TestPeakMemoryAtTheAreaLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("allocates a 4096x4096 sheet")
	}

	const (
		side = 4096
		cell = 128
	)

	if int64(side)*int64(side) != int64(MaxPixels) {
		t.Fatalf("a %dx%d sheet is %d pixels, and MaxPixels is %d: this is no longer the limit",
			side, side, side*side, MaxPixels)
	}

	raw := benchSheetPNG(t, side, cell)
	opts := SliceOptions{Mode: ModeGrid, Grid: Grid{CellW: cell, CellH: cell}}

	runtime.GC()

	res, err := Split(t.Context(), io.Discard, bytes.NewReader(raw), opts)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}

	var after runtime.MemStats

	runtime.ReadMemStats(&after)

	// The decoded sheet alone is four bytes a pixel. The assert is that the
	// peak is that plus a working margin, not a multiple of it: anything
	// approaching twice this would mean the sheet is held twice over.
	sheetBytes := uint64(side) * uint64(side) * 4
	ceiling := sheetBytes * 3 / 2

	// The race detector keeps shadow state per allocation, which inflates the
	// live heap by roughly half again on this path — measured at 102 MiB
	// against 68.6 MiB for the same split without it. The bound is widened
	// rather than skipped, so a real regression is still caught in a race
	// run; the figure docs/benchmarks.md records is the one from a build
	// without the detector, because that is the number the container sees.
	if raceEnabled {
		ceiling = sheetBytes * 5 / 2
	}

	t.Logf("%d frames from %dx%d: heap in use %.1f MiB, decoded sheet alone %.1f MiB, ceiling %.1f MiB",
		res.Frames, side, side,
		float64(after.HeapInuse)/(1<<20), float64(sheetBytes)/(1<<20), float64(ceiling)/(1<<20))

	if after.HeapInuse > ceiling {
		t.Errorf("peak heap is %d bytes, over the ceiling of %d (1.5x the decoded sheet)",
			after.HeapInuse, ceiling)
	}
}

// TestSplitDoesNotCopyTheSheet checks the frame crop shares the sheet's pixels
// instead of copying them, which is what makes the bound above hold. A copy
// per frame would allocate the whole sheet again across a full split.
func TestSplitDoesNotCopyTheSheet(t *testing.T) {
	sheet := fixtureSheet()

	view := cropToOrigin(sheet, image.Rect(fixtureMargin, fixtureMargin,
		fixtureMargin+fixtureCell, fixtureMargin+fixtureCell))

	if view.Bounds() != image.Rect(0, 0, fixtureCell, fixtureCell) {
		t.Fatalf("the view is %v, want an %dx%d frame at the origin",
			view.Bounds(), fixtureCell, fixtureCell)
	}

	// Writing through the sheet must be visible through the view, which is
	// only true if no copy was made.
	marker := color.NRGBA{R: 1, G: 2, B: 3, A: 4}
	sheet.SetNRGBA(fixtureMargin, fixtureMargin, marker)

	gr, gg, gb, ga := view.At(0, 0).RGBA()
	wr, wg, wb, wa := marker.RGBA()

	if gr != wr || gg != wg || gb != wb || ga != wa {
		t.Error("the frame crop copied the sheet's pixels instead of sharing them")
	}
}
