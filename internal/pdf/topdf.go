package pdf

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// imageTimeout bounds converting one image to the format a PDF carries.
const imageTimeout = 5 * time.Minute

// MaxImagesPerPDF caps how many images one conversion may bind.
//
// Each image is a page and a separate engine invocation, so this bounds both
// the result and the work. Fifty is well past a scanned document somebody
// assembles by hand, and a larger set is a job for an archive tool.
const MaxImagesPerPDF = 50

// Orientation is how a page is turned.
type Orientation string

// The orientations. "auto" is the default and the only one most people want:
// a landscape photograph on a portrait page wastes half the sheet.
const (
	// OrientAuto turns each page to match its image.
	OrientAuto Orientation = "auto"
	// OrientPortrait makes every page taller than it is wide.
	OrientPortrait Orientation = "portrait"
	// OrientLandscape makes every page wider than it is tall.
	OrientLandscape Orientation = "landscape"
)

var orientations = []Orientation{OrientAuto, OrientPortrait, OrientLandscape}

// Valid reports whether o is an orientation this package knows.
func (o Orientation) Valid() bool { return slices.Contains(orientations, o) }

// Orientations returns every orientation a client may name.
func Orientations() []string {
	out := make([]string, 0, len(orientations))
	for _, o := range orientations {
		out = append(out, string(o))
	}

	return out
}

// Fit is how an image is placed on its page.
type Fit string

// The fits.
const (
	// FitContain scales the image to fit inside the margins, keeping its
	// proportions, which is what "put my photographs in a PDF" means.
	FitContain Fit = "contain"
	// FitFill scales it to cover the page, cropping the overflow.
	FitFill Fit = "fill"
	// FitExact makes each page exactly the image's own size, so the result
	// has no margin and no empty space. It ignores the page size.
	FitExact Fit = "exact"
)

var fits = []Fit{FitContain, FitFill, FitExact}

// Valid reports whether f is a fit this package knows.
func (f Fit) Valid() bool { return slices.Contains(fits, f) }

// Fits returns every fit a client may name.
func Fits() []string {
	out := make([]string, 0, len(fits))
	for _, f := range fits {
		out = append(out, string(f))
	}

	return out
}

// ImagesRequest is one images-to-PDF conversion.
type ImagesRequest struct {
	Dir string
	// Inputs are the image files, in the order they should appear.
	Inputs []string
	// JPEG marks the inputs that are already JPEG, parallel to Inputs. A
	// JPEG is embedded without being touched; anything else is converted
	// once. The caller detects the type from the bytes — this package does
	// not guess from the extension.
	JPEG []bool
	// Output is the PDF.
	Output string
	// PageSize is a name from PageSizeNames, ignored when Fit is exact.
	PageSize string
	// Orientation defaults to auto.
	Orientation Orientation
	// Fit defaults to contain.
	Fit Fit
	// Margin is the space around the image in points, ignored by exact.
	Margin float64
	// Quality is the JPEG quality used when an input has to be converted.
	Quality int
	// Progress, when set, is called as each image is prepared.
	Progress func(done, total int)
}

// MaxImageMargin is the largest margin a page may have, in points. Two
// inches; more than that and the image would be a stamp in the middle of a
// sheet.
const MaxImageMargin = 144

// defaultImageQuality is the quality an input that is not already JPEG is
// converted at. 90 is visually lossless for a photograph and well short of
// the size an uncompressed save would reach.
const defaultImageQuality = 90

// ImagesResult is what the conversion produced.
type ImagesResult struct {
	// Pages is the page count, which is the number of images.
	Pages int
	// Reencoded is how many inputs had to be converted because they were
	// not already JPEG. The number matters to a user: an input that was
	// re-encoded is an input whose quality changed.
	Reencoded int
}

// FromImages binds images into a PDF, one image per page.
//
// A JPEG input is embedded exactly as it arrived, as a DCTDecode stream: the
// bytes in the PDF are the bytes the user uploaded, re-encoded zero times.
// That is both the fastest path and the only lossless one, and it is why the
// caller passes which inputs are already JPEG rather than letting this
// function convert everything for uniformity.
//
// Anything else — PNG, WebP, TIFF, AVIF — goes through libvips once, to a
// JPEG. libvips is the engine rather than Go's image packages for a reason
// that matters on a small container: `image.Decode` holds the whole
// decompressed bitmap, which for a 50-megapixel TIFF is 150 MB, while
// libvips streams the conversion and never holds the image. Transparency is
// flattened onto white explicitly, because JPEG has no alpha and libvips'
// default background is black — a logo with a transparent background would
// otherwise arrive as a logo on a black square.
func (e *Engines) FromImages(ctx context.Context, req ImagesRequest) (ImagesResult, error) {
	if err := req.validate(); err != nil {
		return ImagesResult{}, err
	}

	page, err := req.page()
	if err != nil {
		return ImagesResult{}, err
	}

	specs := make([]PageSpec, 0, len(req.Inputs))
	temporary := make([]string, 0, len(req.Inputs))

	defer func() { removeAll(temporary) }()

	result := ImagesResult{Pages: len(req.Inputs)}

	for i, input := range req.Inputs {
		jpeg := input

		if !req.JPEG[i] {
			converted, err := e.toJPEG(ctx, req, input, i)
			if err != nil {
				return ImagesResult{}, err
			}

			jpeg = converted
			temporary = append(temporary, converted)
			result.Reencoded++
		}

		info, err := JPEGInfo(jpeg)
		if err != nil {
			return ImagesResult{}, &OptionError{
				Field:  "file_ids",
				Reason: "Image " + strconv.Itoa(i+1) + " could not be read as a picture.",
			}
		}

		specs = append(specs, layoutImage(jpeg, info, page, req))

		if req.Progress != nil {
			req.Progress(i+1, len(req.Inputs))
		}
	}

	file, err := os.Create(req.Output)
	if err != nil {
		return ImagesResult{}, fmt.Errorf("pdf: create the document: %w", err)
	}

	writeErr := Write(file, specs)

	if err := file.Close(); err != nil && writeErr == nil {
		writeErr = fmt.Errorf("pdf: close the document: %w", err)
	}

	if writeErr != nil {
		removeAll([]string{req.Output})

		return ImagesResult{}, writeErr
	}

	return result, nil
}

// validate fills in the defaults and refuses an option this package cannot
// act on.
func (r *ImagesRequest) validate() error {
	switch {
	case len(r.Inputs) == 0:
		return &OptionError{Field: "file_ids", Reason: "Choose at least one image."}
	case len(r.Inputs) > MaxImagesPerPDF:
		return &OptionError{
			Field:  "file_ids",
			Reason: "At most " + strconv.Itoa(MaxImagesPerPDF) + " images in one document.",
		}
	case len(r.JPEG) != len(r.Inputs):
		return errors.New("pdf: the JPEG flags do not match the inputs")
	}

	if r.Orientation == "" {
		r.Orientation = OrientAuto
	}

	if !r.Orientation.Valid() {
		return &OptionError{
			Field:  "orientation",
			Reason: "The orientation is one of: " + strings.Join(Orientations(), ", ") + ".",
		}
	}

	if r.Fit == "" {
		r.Fit = FitContain
	}

	if !r.Fit.Valid() {
		return &OptionError{Field: "fit", Reason: "The fit is one of: " + strings.Join(Fits(), ", ") + "."}
	}

	if r.Margin < 0 || r.Margin > MaxImageMargin {
		return &OptionError{
			Field:  "margin",
			Reason: "The margin must be between 0 and " + strconv.Itoa(MaxImageMargin) + " points.",
		}
	}

	if r.Quality == 0 {
		r.Quality = defaultImageQuality
	}

	if r.Quality < 1 || r.Quality > 100 {
		return &OptionError{Field: "quality", Reason: "The quality must be between 1 and 100."}
	}

	return nil
}

// page resolves the page size.
func (r ImagesRequest) page() (Box, error) {
	if r.Fit == FitExact {
		return Box{}, nil
	}

	name := r.PageSize
	if name == "" {
		name = "a4"
	}

	box, ok := PageSize(name)
	if !ok {
		return Box{}, &OptionError{
			Field:  "page_size",
			Reason: "The page size is one of: " + strings.Join(PageSizeNames(), ", ") + ".",
		}
	}

	return box, nil
}

// toJPEG converts one input image to a JPEG this package can embed.
func (e *Engines) toJPEG(ctx context.Context, req ImagesRequest, input string, index int) (string, error) {
	out := filepath.Join(req.Dir, "image-"+strconv.Itoa(index)+".jpg")

	// flatten rather than jpegsave: an image with an alpha channel saved
	// straight to JPEG is composited against libvips' default background,
	// which is black. flatten takes the background explicitly, and on an
	// image with no alpha it is a pass-through — so one command covers both
	// and neither case needs a guess about which one this is.
	_, err := e.run(ctx, command{
		engine: EngineVips,
		args: []string{
			"flatten", input, out + "[Q=" + strconv.Itoa(req.Quality) + ",optimize_coding]",
			"--background", "255",
		},
		dir:     req.Dir,
		timeout: imageTimeout,
	})
	if err != nil {
		if _, ok := errors.AsType[*EngineError](err); ok {
			return "", &OptionError{
				Field:  "file_ids",
				Reason: "Image " + strconv.Itoa(index+1) + " could not be converted. It may be damaged.",
			}
		}

		return "", err
	}

	return out, nil
}

// layoutImage decides one image's page and its box on it.
func layoutImage(jpeg string, info ImageInfo, page Box, req ImagesRequest) PageSpec {
	stamp := ImageStamp{
		JPEGPath:    jpeg,
		PixelWidth:  info.Width,
		PixelHeight: info.Height,
		Gray:        info.Gray,
	}

	if req.Fit == FitExact {
		// 72 points to the inch and one pixel to the point: the page is the
		// image's own size, which is what somebody asking for no margin
		// means.
		box := Box{URX: float64(info.Width), URY: float64(info.Height)}
		stamp.Box = box

		return PageSpec{Box: box, Images: []ImageStamp{stamp}}
	}

	sheet := orientPage(page, info, req.Orientation)
	area := Box{
		LLX: sheet.LLX + req.Margin,
		LLY: sheet.LLY + req.Margin,
		URX: sheet.URX - req.Margin,
		URY: sheet.URY - req.Margin,
	}

	stamp.Box = fitBox(area, info, req.Fit)

	return PageSpec{Box: sheet, Images: []ImageStamp{stamp}}
}

// orientPage turns the sheet to match the orientation asked for.
func orientPage(page Box, info ImageInfo, orientation Orientation) Box {
	landscape := page.Width() > page.Height()

	want := landscape

	switch orientation {
	case OrientPortrait:
		want = false
	case OrientLandscape:
		want = true
	case OrientAuto:
		want = info.Width > info.Height
	}

	if want == landscape {
		return page
	}

	return Box{LLX: page.LLX, LLY: page.LLY, URX: page.LLX + page.Height(), URY: page.LLY + page.Width()}
}

// fitBox scales the image into the area, keeping its proportions.
//
// Both fits keep the aspect ratio: "fill" covers the area and lets the
// overflow run past it, "contain" fits inside and leaves the rest of the
// sheet empty. Stretching is not an option, because nobody wants their
// photographs stretched and offering it would mean somebody choosing it by
// accident.
func fitBox(area Box, info ImageInfo, fit Fit) Box {
	if info.Width <= 0 || info.Height <= 0 || area.Width() <= 0 || area.Height() <= 0 {
		return area
	}

	byWidth := area.Width() / float64(info.Width)
	byHeight := area.Height() / float64(info.Height)

	scale := min(byWidth, byHeight)
	if fit == FitFill {
		scale = max(byWidth, byHeight)
	}

	width := float64(info.Width) * scale
	height := float64(info.Height) * scale

	// Centred on the area either way: a contained image with the space at
	// the bottom and a filled image cropped only on one side would both look
	// like mistakes.
	x := area.LLX + (area.Width()-width)/2
	y := area.LLY + (area.Height()-height)/2

	return Box{LLX: x, LLY: y, URX: x + width, URY: y + height}
}
