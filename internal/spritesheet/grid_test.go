package spritesheet

import (
	"image"
	"strings"
	"testing"
)

// TestGridFramesRowMajor pins the geometry of a grid with both a margin and a
// spacing, which is the case every off-by-one lives in: the margin is outside
// the first cell, the spacing only between cells, and neither is counted after
// the last one.
func TestGridFramesRowMajor(t *testing.T) {
	frames, err := GridFrames(fixtureWidth, fixtureHeight, Grid{
		CellW:   fixtureCell,
		CellH:   fixtureCell,
		Margin:  fixtureMargin,
		Spacing: fixtureSpacing,
	})
	if err != nil {
		t.Fatalf("GridFrames: %v", err)
	}

	if len(frames) != fixtureCols*fixtureRows {
		t.Fatalf("got %d frames, want %d", len(frames), fixtureCols*fixtureRows)
	}

	for row := range fixtureRows {
		for col := range fixtureCols {
			i := row*fixtureCols + col
			want := image.Rect(
				fixtureMargin+col*(fixtureCell+fixtureSpacing),
				fixtureMargin+row*(fixtureCell+fixtureSpacing),
				fixtureMargin+col*(fixtureCell+fixtureSpacing)+fixtureCell,
				fixtureMargin+row*(fixtureCell+fixtureSpacing)+fixtureCell,
			)

			if frames[i].Rect != want {
				t.Errorf("frame %d (row %d, col %d) is %v, want %v", i, row, col, frames[i].Rect, want)
			}
		}
	}
}

// TestGridFramesByCounts checks that naming the column and row counts produces
// exactly the same frames as naming the cell size. They are two ways of saying
// one thing, so a difference between them is a bug a person would never be able
// to explain.
func TestGridFramesByCounts(t *testing.T) {
	bySize, err := GridFrames(fixtureWidth, fixtureHeight, Grid{
		CellW:   fixtureCell,
		CellH:   fixtureCell,
		Margin:  fixtureMargin,
		Spacing: fixtureSpacing,
	})
	if err != nil {
		t.Fatalf("by size: %v", err)
	}

	byCount, err := GridFrames(fixtureWidth, fixtureHeight, Grid{
		Cols:    fixtureCols,
		Rows:    fixtureRows,
		Margin:  fixtureMargin,
		Spacing: fixtureSpacing,
	})
	if err != nil {
		t.Fatalf("by count: %v", err)
	}

	if len(bySize) != len(byCount) {
		t.Fatalf("by size gave %d frames, by count gave %d", len(bySize), len(byCount))
	}

	for i := range bySize {
		if bySize[i].Rect != byCount[i].Rect {
			t.Errorf("frame %d: by size %v, by count %v", i, bySize[i].Rect, byCount[i].Rect)
		}
	}
}

// TestGridDoesNotTile checks the refusal, and checks the message rather than
// only that the error is non-nil: the whole value of this refusal is that it
// tells the person what did not divide.
func TestGridDoesNotTile(t *testing.T) {
	_, err := GridFrames(fixtureWidth, fixtureHeight, Grid{
		CellW:   7, // 40 is not 1 + n*7 + (n-1)*2 + 1 for any n
		CellH:   fixtureCell,
		Margin:  fixtureMargin,
		Spacing: fixtureSpacing,
	})
	if err == nil {
		t.Fatal("a grid that does not tile the sheet was accepted")
	}

	var ge *GridError
	if !asError(err, &ge) {
		t.Fatalf("error is %T, want *GridError", err)
	}

	msg := err.Error()
	for _, want := range []string{"40", "7", "remainder"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not name %q", msg, want)
		}
	}
}

// TestGridRefusesTooManyFrames checks the frame cap is enforced from the grid
// alone, before a single frame is cut.
func TestGridRefusesTooManyFrames(t *testing.T) {
	// A 1-pixel cell over a large sheet asks for far more frames than the cap.
	side := 4096

	_, err := GridFrames(side, side, Grid{CellW: 1, CellH: 1})
	if err == nil {
		t.Fatalf("a grid of %d frames was accepted, the cap is %d", side*side, MaxFrames)
	}

	var fe *FrameCountError
	if !asError(err, &fe) {
		t.Fatalf("error is %T, want *FrameCountError", err)
	}

	if fe.Limit != MaxFrames {
		t.Errorf("the error reports a limit of %d, want %d", fe.Limit, MaxFrames)
	}
}

// TestGridRejectsBadOptions checks every way of describing a grid wrongly is
// refused rather than silently normalised into something that happens to work.
func TestGridRejectsBadOptions(t *testing.T) {
	cases := map[string]Grid{
		"nothing stated":         {},
		"negative cell":          {CellW: -1, CellH: 8},
		"negative margin":        {CellW: 8, CellH: 8, Margin: -1},
		"negative spacing":       {CellW: 8, CellH: 8, Spacing: -1},
		"cell bigger than sheet": {CellW: fixtureWidth + 1, CellH: 8},
		"counts and size both":   {CellW: 8, CellH: 8, Cols: 2, Rows: 2},
	}

	for name, g := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := GridFrames(fixtureWidth, fixtureHeight, g); err == nil {
				t.Fatalf("grid %+v was accepted", g)
			}
		})
	}
}
