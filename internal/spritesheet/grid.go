package spritesheet

import (
	"fmt"
	"image"
	"strconv"
)

// GridFrames returns the frames of a w by h sheet under g, in row-major order.
//
// It takes the dimensions rather than an image on purpose: the whole point of
// a grid is that it is decided from the sheet's size, so the frame count and
// the refusal are both available before a pixel has been decoded. That is what
// lets the frame cap be enforced without allocating anything.
func GridFrames(w, h int, g Grid) ([]Frame, error) {
	return gridFrames(w, h, g, defaultPrefix)
}

// gridFrames is GridFrames with the frame-name prefix as a parameter.
func gridFrames(w, h int, g Grid, prefix string) ([]Frame, error) {
	if w <= 0 || h <= 0 {
		return nil, ErrEmptyImage
	}

	if err := g.validate(); err != nil {
		return nil, err
	}

	eff, err := g.resolve(w, h)
	if err != nil {
		return nil, err
	}

	cols, rows := eff.Cols, eff.Rows

	// Checked before anything is built: the cap exists to stop a request, not
	// to trim a result that has already been produced.
	if n := cols * rows; n > MaxFrames {
		return nil, &FrameCountError{Count: n, Limit: MaxFrames}
	}

	out := make([]Frame, 0, cols*rows)

	for row := range rows {
		for col := range cols {
			x := eff.Margin + col*(eff.CellW+eff.Spacing)
			y := eff.Margin + row*(eff.CellH+eff.Spacing)

			out = append(out, Frame{
				Name:   frameName(prefix, len(out)),
				Rect:   image.Rect(x, y, x+eff.CellW, y+eff.CellH),
				Source: image.Pt(eff.CellW, eff.CellH),
			})
		}
	}

	return out, nil
}

// validate refuses a grid that is malformed in itself, before it is compared
// with any sheet.
func (g Grid) validate() error {
	bySize := g.CellW > 0 || g.CellH > 0
	byCount := g.Cols > 0 || g.Rows > 0

	switch {
	case !bySize && !byCount:
		return &GridError{Reason: "a grid needs either a cell size or a column and row count"}
	case bySize && byCount:
		return &GridError{
			Reason: "a grid takes either a cell size or a column and row count, not both: " +
				"they are two ways of saying one thing and these two disagree",
		}
	case bySize && (g.CellW <= 0 || g.CellH <= 0):
		return &GridError{
			Reason: "a cell size needs both a width and a height, each above zero",
		}
	case byCount && (g.Cols <= 0 || g.Rows <= 0):
		return &GridError{
			Reason: "a column and row count needs both, each above zero",
		}
	case g.Margin < 0 || g.Margin > MaxGutter:
		return &GridError{
			Reason: "the margin is " + strconv.Itoa(g.Margin) +
				", and must be between 0 and " + strconv.Itoa(MaxGutter),
		}
	case g.Spacing < 0 || g.Spacing > MaxGutter:
		return &GridError{
			Reason: "the spacing is " + strconv.Itoa(g.Spacing) +
				", and must be between 0 and " + strconv.Itoa(MaxGutter),
		}
	}

	return nil
}

// resolve fills in whichever of the cell size and the counts was not given,
// and checks the result tiles the sheet exactly.
//
// The usable area is the sheet minus the margin on both sides. Within it, n
// cells of size c separated by s take n*c + (n-1)*s — the spacing appears
// between cells only, never after the last one, which is the off-by-one this
// whole function exists to get right.
func (g Grid) resolve(w, h int) (Grid, error) {
	eff := g

	usableW := w - 2*g.Margin
	usableH := h - 2*g.Margin

	if usableW <= 0 || usableH <= 0 {
		return Grid{}, &GridError{
			Reason: fmt.Sprintf("a margin of %d leaves nothing of a %dx%d sheet", g.Margin, w, h),
		}
	}

	if g.CellW > 0 {
		// The caller gave the cell size, so the counts follow from it.
		if g.CellW > usableW || g.CellH > usableH {
			return Grid{}, &GridError{
				Reason: fmt.Sprintf("a %dx%d cell does not fit the %dx%d usable area of a %dx%d "+
					"sheet with a margin of %d", g.CellW, g.CellH, usableW, usableH, w, h, g.Margin),
			}
		}

		eff.Cols = countFor(usableW, g.CellW, g.Spacing)
		eff.Rows = countFor(usableH, g.CellH, g.Spacing)
	} else {
		// The caller gave the counts, so the cell size follows from them.
		spanW := usableW - (g.Cols-1)*g.Spacing
		spanH := usableH - (g.Rows-1)*g.Spacing

		if spanW <= 0 || spanH <= 0 {
			return Grid{}, &GridError{
				Reason: fmt.Sprintf("%d columns and %d rows with a spacing of %d leave no room "+
					"for a cell in a %dx%d sheet", g.Cols, g.Rows, g.Spacing, w, h),
			}
		}

		eff.CellW = spanW / g.Cols
		eff.CellH = spanH / g.Rows
	}

	if eff.CellW <= 0 || eff.CellH <= 0 {
		return Grid{}, &GridError{
			Reason: fmt.Sprintf("the cell size works out as %dx%d, which is not a cell",
				eff.CellW, eff.CellH),
		}
	}

	usedW := eff.Cols*eff.CellW + (eff.Cols-1)*eff.Spacing
	usedH := eff.Rows*eff.CellH + (eff.Rows-1)*eff.Spacing

	if remX, remY := usableW-usedW, usableH-usedH; remX != 0 || remY != 0 {
		return Grid{}, &GridError{
			SheetW: w, SheetH: h,
			CellW: eff.CellW, CellH: eff.CellH,
			Margin: eff.Margin, Spacing: eff.Spacing,
			RemainderX: remX, RemainderY: remY,
		}
	}

	return eff, nil
}

// countFor returns how many cells of size cell, separated by spacing, fit in
// span. It rounds down, so a span that does not tile exactly reports the count
// that does fit and resolve then refuses the remainder.
func countFor(span, cell, spacing int) int {
	if n := (span + spacing) / (cell + spacing); n > 0 {
		return n
	}

	return 1
}

// frameName is the nth frame's name. The index is zero-padded to four digits
// so the frames sort in order everywhere — in an archive listing, in a file
// manager and in an engine's own asset browser.
func frameName(prefix string, i int) string {
	return fmt.Sprintf("%s_%04d", prefix, i)
}
