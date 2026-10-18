package spritesheet

import (
	"errors"
	"fmt"
	"strconv"
)

// ErrNilImage is returned when no image was given.
var ErrNilImage = errors.New("spritesheet: nil image")

// ErrEmptyImage is returned for an image whose width or height is zero.
var ErrEmptyImage = errors.New("spritesheet: image has no pixels")

// DimensionError reports a sheet refused for being too large, either per side
// or by area.
//
// It carries the area limit rather than the side limit when the area is what
// was exceeded, because those are two different refusals and a message naming
// the wrong one sends the person off to resize the wrong thing.
type DimensionError struct {
	// Width and Height are the declared dimensions of the refused sheet.
	Width, Height int
	// Limit is the limit that was exceeded, in pixels of area when Area is
	// true and in pixels per side otherwise.
	Limit int
	// Area says which limit Limit is.
	Area bool
}

// Error implements error.
func (e *DimensionError) Error() string {
	if e.Area {
		return fmt.Sprintf("spritesheet: the sheet is %dx%d, which is %d pixels, over the limit of %d",
			e.Width, e.Height, e.Width*e.Height, e.Limit)
	}

	return fmt.Sprintf("spritesheet: the sheet is %dx%d, over the %d pixel limit on either side",
		e.Width, e.Height, e.Limit)
}

// Message is the sentence to show the person. It states the limit as a number,
// because somebody who has hit a cap needs to know what it is to get under it.
func (e *DimensionError) Message() string {
	if e.Area {
		return fmt.Sprintf("That sheet is %d×%d pixels. The limit is %d pixels in total, "+
			"which is %d×%d — resize it or split it into smaller sheets.",
			e.Width, e.Height, e.Limit, squareSideOf(e.Limit), squareSideOf(e.Limit))
	}

	return fmt.Sprintf("That sheet is %d×%d pixels. Neither side may be over %d.",
		e.Width, e.Height, e.Limit)
}

// InputSizeError reports encoded image data refused for being longer than
// [MaxInputBytes].
type InputSizeError struct {
	// Limit is the largest accepted length of the encoded sheet, in bytes.
	Limit int64
}

// Error implements error.
func (e *InputSizeError) Error() string {
	return "spritesheet: the encoded sheet is longer than the " +
		strconv.FormatInt(e.Limit, 10) + " byte limit"
}

// Message is the sentence to show the person.
func (e *InputSizeError) Message() string {
	return fmt.Sprintf("That file is larger than %d MB, which is the limit for a sheet.",
		e.Limit>>20)
}

// GridError reports a grid that cannot describe the sheet it was given.
//
// It carries every number that went into the decision, because "the grid does
// not fit" is useless and "40 pixels wide, with a 1 pixel margin and 7 pixel
// cells 2 pixels apart, leaves 3 over" is something a person can act on.
type GridError struct {
	// SheetW and SheetH are the sheet's real dimensions.
	SheetW, SheetH int
	// CellW and CellH are the cell size that was used or derived.
	CellW, CellH int
	// Margin and Spacing are the gutters that were applied.
	Margin, Spacing int
	// RemainderX and RemainderY are the pixels left over on each axis.
	RemainderX, RemainderY int
	// Reason names the specific problem when it is not a remainder.
	Reason string
}

// Error implements error.
func (e *GridError) Error() string {
	if e.Reason != "" {
		return "spritesheet: " + e.Reason
	}

	return fmt.Sprintf(
		"spritesheet: a %dx%d grid of cells with a margin of %d and a spacing of %d "+
			"does not tile a %dx%d sheet: the remainder is %d horizontally and %d vertically",
		e.CellW, e.CellH, e.Margin, e.Spacing, e.SheetW, e.SheetH, e.RemainderX, e.RemainderY)
}

// Message is the sentence to show the person.
func (e *GridError) Message() string {
	if e.Reason != "" {
		return "That grid cannot be used: " + e.Reason + "."
	}

	return fmt.Sprintf("A %d×%d cell with a %d pixel margin and %d pixel spacing does not "+
		"divide a %d×%d sheet: %d pixels are left over across and %d down. "+
		"Check the cell size, or use automatic detection instead.",
		e.CellW, e.CellH, e.Margin, e.Spacing, e.SheetW, e.SheetH, e.RemainderX, e.RemainderY)
}

// FrameCountError reports a request for more frames than one conversion may
// produce.
type FrameCountError struct {
	// Count is how many frames were asked for.
	Count int
	// Limit is the most this tool will produce.
	Limit int
}

// Error implements error.
func (e *FrameCountError) Error() string {
	return fmt.Sprintf("spritesheet: %d frames were asked for, over the limit of %d",
		e.Count, e.Limit)
}

// Message is the sentence to show the person.
func (e *FrameCountError) Message() string {
	return fmt.Sprintf("That would produce %d frames. The limit is %d per sheet.",
		e.Count, e.Limit)
}

// BackgroundError reports a sheet that automatic detection cannot read,
// because it has no transparency and no background colour was named.
//
// This is a refusal rather than a guess on purpose: picking the most common
// colour and hoping would produce frames that are subtly wrong, and subtly
// wrong frames are worse than none.
type BackgroundError struct{}

// Error implements error.
func (*BackgroundError) Error() string {
	return "spritesheet: the sheet is fully opaque, so automatic detection has no background " +
		"to separate frames from; name a background colour or use grid mode"
}

// Message is the sentence to show the person. It names both ways forward: a
// refusal that does not say what to do instead is just a failure.
func (*BackgroundError) Message() string {
	return "This sheet has no transparency, so the frames cannot be found automatically. " +
		"Either name the background colour, or switch to grid mode and give the cell size."
}

// OptionError reports a value in [SliceOptions] the conversion cannot accept.
type OptionError struct {
	// Field is the name of the offending field.
	Field string
	// Value is what was given.
	Value string
	// Want describes what is accepted.
	Want string
}

// Error implements error.
func (e *OptionError) Error() string {
	return "spritesheet: " + e.Field + " is " + e.Value + ", want " + e.Want
}

// Message is the sentence to show the person.
func (e *OptionError) Message() string {
	return "The " + e.Field + " option is " + e.Value + ". It must be " + e.Want + "."
}
