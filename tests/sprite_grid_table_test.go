package tests

import (
	"testing"

	"github.com/AlexGalhardo/vukem-tools/internal/spritesheet"
)

// The shared table.
//
// Every row is run against the Go implementation here and against the
// TypeScript one in resources/js/lib/spriteGrid.test.ts. The two are therefore
// pinned to one written-down set of answers rather than to each other, so a
// change to either that is not matched by the other fails here or there.
//
// It lives in ./tests/ rather than beside internal/spritesheet because it is
// a cross-cutting gate: it is about two implementations agreeing, which is
// exactly the kind of thing a domain author cannot see from inside their own
// package.
//
// Keep the two tables identical. The TypeScript one carries the same comment.
func TestTheGridTableHoldsInGo(t *testing.T) {
	t.Parallel()

	for _, row := range []struct {
		name   string
		w, h   int
		grid   spritesheet.Grid
		frames int // -1 means the grid must be refused
	}{
		{"cell size tiles exactly", 128, 64, spritesheet.Grid{CellW: 32, CellH: 32}, 8},
		{"counts tile exactly", 128, 64, spritesheet.Grid{Cols: 4, Rows: 2}, 8},
		{"a margin on every edge", 132, 68, spritesheet.Grid{CellW: 32, CellH: 32, Margin: 2}, 8},
		{"spacing between cells only", 134, 66, spritesheet.Grid{CellW: 32, CellH: 32, Spacing: 2}, 8},
		{
			"a margin and a spacing", 138, 70,
			spritesheet.Grid{CellW: 32, CellH: 32, Margin: 2, Spacing: 2},
			8,
		},
		{"a cell that leaves a remainder", 100, 100, spritesheet.Grid{CellW: 32, CellH: 32}, -1},
		{"both a size and a count", 128, 64, spritesheet.Grid{CellW: 32, CellH: 32, Cols: 4}, -1},
		{"neither a size nor a count", 128, 64, spritesheet.Grid{}, -1},
		{"a cell wider than the sheet", 128, 64, spritesheet.Grid{CellW: 200, CellH: 32}, -1},
		{"a margin that eats the sheet", 128, 64, spritesheet.Grid{CellW: 32, CellH: 32, Margin: 70}, -1},
		{"a thousand frames", 4096, 4096, spritesheet.Grid{CellW: 128, CellH: 128}, 1024},
		{"past the frame cap", 4096, 4096, spritesheet.Grid{CellW: 64, CellH: 64}, -1},
		{"a single row", 96, 32, spritesheet.Grid{Cols: 3, Rows: 1}, 3},
		{
			"counts whose spacing leaves a remainder", 128, 64,
			spritesheet.Grid{Cols: 4, Rows: 2, Spacing: 2},
			-1,
		},
		{
			"counts with a margin and a spacing", 140, 72,
			spritesheet.Grid{Cols: 4, Rows: 2, Margin: 2, Spacing: 4},
			8,
		},
	} {
		frames, err := spritesheet.GridFrames(row.w, row.h, row.grid)

		if row.frames < 0 {
			if err == nil {
				t.Errorf("%s: %dx%d with %+v produced %d frames, want a refusal",
					row.name, row.w, row.h, row.grid, len(frames))
			}

			continue
		}

		if err != nil {
			t.Errorf("%s: %dx%d with %+v was refused: %v", row.name, row.w, row.h, row.grid, err)

			continue
		}

		if len(frames) != row.frames {
			t.Errorf("%s: %dx%d with %+v produced %d frames, want %d",
				row.name, row.w, row.h, row.grid, len(frames), row.frames)
		}
	}
}

// The counts case resolves to the cell size the TypeScript mirror computes,
// which is where the two could most easily drift: Go divides integers and
// JavaScript does not, so the mirror has to floor explicitly.
func TestTheGridResolvesCountsWithIntegerDivision(t *testing.T) {
	t.Parallel()

	frames, err := spritesheet.GridFrames(140, 72, spritesheet.Grid{Cols: 4, Rows: 2, Margin: 2, Spacing: 4})
	if err != nil {
		t.Fatal(err)
	}

	if len(frames) == 0 {
		t.Fatal("no frames")
	}

	if got := frames[0].Rect; got.Dx() != 31 || got.Dy() != 32 {
		t.Errorf("the first cell is %dx%d, want 31x32: 124/4 is 31, not 31.0",
			got.Dx(), got.Dy())
	}
}
