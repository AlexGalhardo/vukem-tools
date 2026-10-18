package spritesheet

import (
	"image"
	"image/color"
	"strings"
	"testing"
)

// autoBoxes are the five frames the irregular fixture holds: three different
// sizes, deliberately not on any grid, with one pair whose vertical ranges
// overlap so the reading order has to group them into a row rather than sort
// by y alone.
var autoBoxes = []image.Rectangle{
	image.Rect(2, 3, 10, 11),   // row 1, left: 8x8
	image.Rect(14, 4, 19, 9),   // row 1, middle: 5x5, starts lower than its neighbour
	image.Rect(23, 3, 35, 15),  // row 1, right: 12x12
	image.Rect(3, 20, 9, 26),   // row 2, left: 6x6
	image.Rect(13, 21, 29, 29), // row 2, right: 16x8
}

// irregularSheet draws autoBoxes onto a transparent sheet, each a flat colour.
func irregularSheet() *image.NRGBA {
	sheet := image.NewNRGBA(image.Rect(0, 0, 40, 32))
	pal := fixturePalette(len(autoBoxes))

	for i, box := range autoBoxes {
		for y := box.Min.Y; y < box.Max.Y; y++ {
			for x := box.Min.X; x < box.Max.X; x++ {
				sheet.SetNRGBA(x, y, pal[i])
			}
		}
	}

	return sheet
}

// opaqueSheet is irregularSheet with an opaque background, so there is no
// alpha to detect frames from.
func opaqueSheet(bg color.NRGBA) *image.NRGBA {
	sheet := irregularSheet()

	for y := range sheet.Bounds().Dy() {
		for x := range sheet.Bounds().Dx() {
			if sheet.NRGBAAt(x, y).A == 0 {
				sheet.SetNRGBA(x, y, bg)
			}
		}
	}

	return sheet
}

// TestAutoDetectsNonUniformFrames is the answer to "what about a sheet whose
// frames are not a uniform grid": exact bounding boxes, in reading order.
func TestAutoDetectsNonUniformFrames(t *testing.T) {
	frames, err := Slice(t.Context(), irregularSheet(), SliceOptions{Mode: ModeAuto})
	if err != nil {
		t.Fatalf("Slice: %v", err)
	}

	if len(frames) != len(autoBoxes) {
		t.Fatalf("found %d frames, want %d", len(frames), len(autoBoxes))
	}

	for i, f := range frames {
		if f.Rect != autoBoxes[i] {
			t.Errorf("frame %d is %v, want %v", i, f.Rect, autoBoxes[i])
		}
	}
}

// TestAutoReadingOrderGroupsRows pins the ordering rule rather than leaving it
// implied by the fixture: the middle frame of row one starts one pixel lower
// than its left neighbour, so sorting by y alone would put it after frames it
// sits beside. Reading order means rows first, then x within the row.
func TestAutoReadingOrderGroupsRows(t *testing.T) {
	frames, err := Slice(t.Context(), irregularSheet(), SliceOptions{Mode: ModeAuto})
	if err != nil {
		t.Fatalf("Slice: %v", err)
	}

	// The first three are row one, left to right; the last two are row two.
	for i := 1; i < 3; i++ {
		if frames[i].Rect.Min.X <= frames[i-1].Rect.Min.X {
			t.Errorf("row one is not left to right: frame %d starts at x=%d, frame %d at x=%d",
				i-1, frames[i-1].Rect.Min.X, i, frames[i].Rect.Min.X)
		}
	}

	if frames[3].Rect.Min.Y <= frames[2].Rect.Min.Y {
		t.Errorf("row two does not come after row one: %v then %v", frames[2].Rect, frames[3].Rect)
	}

	if frames[4].Rect.Min.X <= frames[3].Rect.Min.X {
		t.Errorf("row two is not left to right: %v then %v", frames[3].Rect, frames[4].Rect)
	}
}

// TestAutoRefusesOpaqueSheetWithoutBackground is the honest refusal. An opaque
// sheet carries no signal saying what is a frame and what is gap, so guessing
// would produce frames that are wrong in a way the person cannot see. The
// message has to name both ways forward, because a refusal that does not tell
// you what to do instead is just a failure.
func TestAutoRefusesOpaqueSheetWithoutBackground(t *testing.T) {
	_, err := Slice(t.Context(), opaqueSheet(color.NRGBA{R: 0x10, G: 0x10, B: 0x10, A: 0xff}),
		SliceOptions{Mode: ModeAuto})
	if err == nil {
		t.Fatal("an opaque sheet with no declared background was accepted")
	}

	var be *BackgroundError
	if !asError(err, &be) {
		t.Fatalf("error is %T, want *BackgroundError", err)
	}

	msg := strings.ToLower(err.Error())
	for _, want := range []string{"background", "grid"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not name %q as a way forward", msg, want)
		}
	}
}

// TestAutoWithDeclaredBackground checks the way out actually works: the same
// opaque sheet, with the background colour named, gives the same five frames.
func TestAutoWithDeclaredBackground(t *testing.T) {
	bg := color.NRGBA{R: 0x10, G: 0x10, B: 0x10, A: 0xff}

	frames, err := Slice(t.Context(), opaqueSheet(bg), SliceOptions{
		Mode:       ModeAuto,
		Background: &bg,
	})
	if err != nil {
		t.Fatalf("Slice: %v", err)
	}

	if len(frames) != len(autoBoxes) {
		t.Fatalf("found %d frames, want %d", len(frames), len(autoBoxes))
	}

	for i, f := range frames {
		if f.Rect != autoBoxes[i] {
			t.Errorf("frame %d is %v, want %v", i, f.Rect, autoBoxes[i])
		}
	}
}

// TestAutoDropsNoiseAndReportsIt checks a stray pixel is not delivered as a
// frame, and that the count of what was dropped is reported rather than
// swallowed: a person whose sheet has specks needs to know the tool ignored
// them.
func TestAutoDropsNoiseAndReportsIt(t *testing.T) {
	sheet := irregularSheet()
	// A single stray pixel, far from every frame.
	sheet.SetNRGBA(38, 30, color.NRGBA{R: 0xff, G: 0, B: 0, A: 0xff})

	frames, err := Slice(t.Context(), sheet, SliceOptions{Mode: ModeAuto, MinFrame: 4})
	if err != nil {
		t.Fatalf("Slice: %v", err)
	}

	if len(frames) != len(autoBoxes) {
		t.Fatalf("found %d frames, want %d: the stray pixel was not dropped",
			len(frames), len(autoBoxes))
	}

	res, err := Detect(t.Context(), sheet, SliceOptions{Mode: ModeAuto, MinFrame: 4})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}

	if res.Dropped != 1 {
		t.Errorf("reported %d dropped components, want 1", res.Dropped)
	}
}

// TestAutoFindsDiagonallyTouchingFrames checks the labelling is 8-connected.
// Pixel art joins at corners all the time — a diagonal line is a staircase of
// corner-touching pixels — and 4-connected labelling would split one frame
// into a cloud of fragments.
func TestAutoFindsDiagonallyTouchingFrames(t *testing.T) {
	sheet := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	c := color.NRGBA{R: 0x40, G: 0x80, B: 0xc0, A: 0xff}

	// A staircase: each pixel touches the next only at a corner.
	for i := range 8 {
		sheet.SetNRGBA(2+i, 2+i, c)
	}

	frames, err := Slice(t.Context(), sheet, SliceOptions{Mode: ModeAuto})
	if err != nil {
		t.Fatalf("Slice: %v", err)
	}

	if len(frames) != 1 {
		t.Fatalf("found %d frames, want 1: the labelling is not 8-connected", len(frames))
	}

	if want := image.Rect(2, 2, 10, 10); frames[0].Rect != want {
		t.Errorf("frame is %v, want %v", frames[0].Rect, want)
	}
}
