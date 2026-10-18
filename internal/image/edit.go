package image

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/AlexGalhardo/vukem-tools/internal/engine"
)

// Rect is an area of an image in source pixels, measured from the top left.
type Rect struct {
	Left   int
	Top    int
	Width  int
	Height int
}

// Rotation is a quarter-turn clockwise. Only the three that libvips's rot
// operation performs are allowed: an arbitrary angle needs interpolation,
// which changes every pixel and is a different tool.
type Rotation int

// The rotations.
const (
	Rotate0   Rotation = 0
	Rotate90  Rotation = 90
	Rotate180 Rotation = 180
	Rotate270 Rotation = 270
)

// Valid reports whether r is one of the four allowed rotations.
func (r Rotation) Valid() bool {
	switch r {
	case Rotate0, Rotate90, Rotate180, Rotate270:
		return true
	default:
		return false
	}
}

// vipsAngle is how libvips's rot operation names the angle.
func (r Rotation) vipsAngle() string { return "d" + strconv.Itoa(int(r)) }

// Flip is a mirror across an axis.
type Flip string

// The flips.
const (
	FlipNone       Flip = ""
	FlipHorizontal Flip = "horizontal"
	FlipVertical   Flip = "vertical"
)

// Valid reports whether f is one of the three allowed values.
func (f Flip) Valid() bool {
	switch f {
	case FlipNone, FlipHorizontal, FlipVertical:
		return true
	default:
		return false
	}
}

// CropError reports a crop that does not fit inside the image.
//
// It carries the real dimensions, because that is the one thing the caller
// needs: "the crop is out of bounds" sends somebody guessing, and "the image
// is 1280x720" tells them what to ask for.
type CropError struct {
	Rect   Rect
	Width  int
	Height int
}

// Error implements error.
func (e *CropError) Error() string {
	return fmt.Sprintf("image: crop %dx%d at %d,%d does not fit in a %dx%d image",
		e.Rect.Width, e.Rect.Height, e.Rect.Left, e.Rect.Top, e.Width, e.Height)
}

// Message is the sentence to show the user, naming the real dimensions.
func (e *CropError) Message() string {
	return fmt.Sprintf("That crop does not fit: the image is %dx%d, and the area asked for ends at %d,%d.",
		e.Width, e.Height, e.Rect.Left+e.Rect.Width, e.Rect.Top+e.Rect.Height)
}

// EditOptions is a geometry edit followed by an encode.
//
// The operations are applied in a fixed order — crop, rotate, flip, resize,
// encode — rather than in the order the caller lists them. A fixed order is
// what makes a width mean something: "crop to this box, then fit inside 800
// wide" has one answer, while an arbitrary order would give "800 wide" two
// different meanings depending on whether the rotation came first.
type EditOptions struct {
	// Crop, when set, is taken from the source image before anything else.
	Crop *Rect
	// Rotate is a quarter-turn clockwise, applied after the crop.
	Rotate Rotation
	// Flip mirrors the image after the rotation.
	Flip Flip
	// Encode is the resize and output-format part, identical to a plain
	// conversion's. The resize is applied last, so a width is the width of
	// the finished image.
	Encode Options
}

// Edit crops, rotates, flips and re-encodes an image.
//
// Each geometry step is one libvips process writing libvips's own `.v` format,
// which is uncompressed and memory-mapped. That is deliberate: chaining
// through JPEG would re-quantise the image at every step, so a crop followed
// by a rotation would be visibly worse than either alone, and chaining through
// PNG would compress and decompress a full frame twice for nothing. The `.v`
// intermediates live in the job's own directory and go away with it.
//
// When there is no crop, rotation or flip, this is exactly Convert, and it
// says so by calling it: that path reads with shrink-on-load, and routing a
// plain resize through a `.v` intermediate would throw that away.
func Edit(ctx context.Context, e Engine, r Runner, dir, input string, o EditOptions) (Result, error) {
	if !o.Rotate.Valid() {
		return Result{}, fmt.Errorf("image: %d is not a rotation this tool applies", int(o.Rotate))
	}

	if !o.Flip.Valid() {
		return Result{}, fmt.Errorf("image: %q is not a flip this tool applies", string(o.Flip))
	}

	source, err := Probe(ctx, e, r, dir, input)
	if err != nil {
		return Result{}, err
	}

	if source.Pixels() > MaxPixels {
		return Result{}, &DimensionError{Width: source.Width, Height: source.Height}
	}

	if o.Crop == nil && o.Rotate == Rotate0 && o.Flip == FlipNone {
		return Convert(ctx, e, r, dir, input, o.Encode)
	}

	current, step := input, 0

	if o.Crop != nil {
		if err := checkCrop(*o.Crop, source); err != nil {
			return Result{}, err
		}

		current, err = e.geometry(ctx, r, dir, current, &step, "crop",
			strconv.Itoa(o.Crop.Left), strconv.Itoa(o.Crop.Top),
			strconv.Itoa(o.Crop.Width), strconv.Itoa(o.Crop.Height))
		if err != nil {
			return Result{}, err
		}
	}

	if o.Rotate != Rotate0 {
		current, err = e.geometry(ctx, r, dir, current, &step, "rot", o.Rotate.vipsAngle())
		if err != nil {
			return Result{}, err
		}
	}

	if o.Flip != FlipNone {
		current, err = e.geometry(ctx, r, dir, current, &step, "flip", string(o.Flip))
		if err != nil {
			return Result{}, err
		}
	}

	// The final encode goes through Convert, so the colour profile, the
	// metadata rule and the per-format encoder settings are applied in
	// exactly one place rather than twice.
	out, err := Convert(ctx, e, r, dir, current, o.Encode)
	if err != nil {
		return Result{}, err
	}

	// The source metadata is the original's, not the last intermediate's: a
	// caller reporting "1920x1080 became 800x450" means the file the user
	// uploaded.
	out.Source = source

	return out, nil
}

// checkCrop refuses an area that is not inside the image.
//
// Validated against the real dimensions from the header rather than against
// anything the client said, which is the point: libvips would also refuse it,
// with a message naming the working directory.
func checkCrop(c Rect, source Metadata) error {
	if c.Width <= 0 || c.Height <= 0 ||
		c.Left < 0 || c.Top < 0 ||
		c.Left+c.Width > source.Width ||
		c.Top+c.Height > source.Height {
		return &CropError{Rect: c, Width: source.Width, Height: source.Height}
	}

	return nil
}

// geometry runs one libvips geometry operation, writing a `.v` intermediate,
// and returns its path.
func (e Engine) geometry(
	ctx context.Context,
	r Runner,
	dir, input string,
	step *int,
	op string,
	args ...string,
) (string, error) {
	*step++

	out := filepath.Join(dir, fmt.Sprintf("step%d-%s.v", *step, op))

	argv := append([]string{op, input + "[access=sequential]", out}, args...)

	if _, err := r.Run(ctx, engine.Command{
		Path: e.Vips,
		Args: argv,
		Dir:  dir,
		Env:  e.env(),
	}); err != nil {
		return "", classify(err, "apply "+op)
	}

	return out, nil
}
