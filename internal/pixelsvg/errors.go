package pixelsvg

import (
	"errors"
	"fmt"
	"strconv"
)

// ErrNilImage is returned when no image was given to convert.
var ErrNilImage = errors.New("pixelsvg: nil image")

// ErrEmptyImage is returned for an image whose width or height is zero.
var ErrEmptyImage = errors.New("pixelsvg: image has no pixels")

// DimensionError reports an image refused for being larger than [MaxDimension].
type DimensionError struct {
	// Width and Height are the actual dimensions of the refused image.
	Width, Height int
	// Limit is the largest accepted width and height.
	Limit int
}

// Error implements error.
func (e *DimensionError) Error() string {
	return fmt.Sprintf("pixelsvg: image is %dx%d pixels, over the %dx%d limit",
		e.Width, e.Height, e.Limit, e.Limit)
}

// ColorCountError reports an image refused for holding more distinct colours
// than the conversion allows.
type ColorCountError struct {
	// Count is the number of distinct colours actually found.
	Count int
	// Limit is the largest accepted number of distinct colours.
	Limit int
}

// Error implements error.
func (e *ColorCountError) Error() string {
	return fmt.Sprintf("pixelsvg: image has %d distinct colours, over the limit of %d; "+
		"set Options.Quantizer to %q to reduce them", e.Count, e.Limit, QuantizerMedianCut)
}

// InputSizeError reports encoded image data refused for being longer than
// [MaxInputBytes].
type InputSizeError struct {
	// Limit is the largest accepted length of the encoded image, in bytes.
	Limit int64
}

// Error implements error.
func (e *InputSizeError) Error() string {
	return "pixelsvg: encoded image is longer than the " + strconv.FormatInt(e.Limit, 10) + " byte limit"
}

// OptionError reports a value in [Options] that the conversion cannot accept.
type OptionError struct {
	// Field is the name of the offending Options field.
	Field string
	// Value is the value that was given.
	Value string
	// Want describes the values that are accepted.
	Want string
}

// Error implements error.
func (e *OptionError) Error() string {
	return "pixelsvg: Options." + e.Field + " is " + e.Value + ", want " + e.Want
}
