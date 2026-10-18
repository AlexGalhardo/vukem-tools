package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/AlexGalhardo/vukem/z"

	"github.com/AlexGalhardo/vukem-tools/app/abilities"
	"github.com/AlexGalhardo/vukem-tools/internal/files"
	vipsimage "github.com/AlexGalhardo/vukem-tools/internal/image"
	"github.com/AlexGalhardo/vukem-tools/internal/jobs"
)

// ImageTools is the image domain's tools.
//
// Without an engine it returns nothing rather than a tool that fails on its
// first conversion: a missing engine should make a tool invisible, which the
// catalogue then reports as not available, not produce a tool that accepts
// work it cannot do.
func ImageTools(f *files.Store, m *Media) []jobs.Tool {
	if !m.HasImageEngine() {
		return nil
	}

	return []jobs.Tool{NewImageConvert(f, m)}
}

// ImageConvert converts, resizes, crops, rotates and flips a raster image.
//
// One tool rather than four, because they are one libvips invocation: a
// resize is a re-encode with a target size, and the orientation and colour
// handling has to happen on the same pass whichever of them was asked for.
// Splitting them into separate tools would mean a user who wants a smaller
// WebP running two conversions and paying twice for a decode.
type ImageConvert struct {
	files *files.Store
	media *Media
}

// NewImageConvert returns the tool.
func NewImageConvert(f *files.Store, m *Media) *ImageConvert {
	return &ImageConvert{files: f, media: m}
}

// Name implements jobs.Tool. It is the stable id stored on every conversion
// and published by GET /api/v1/tools, so it is part of the public API.
func (*ImageConvert) Name() string { return "image-convert" }

// Ability implements jobs.Tool.
func (*ImageConvert) Ability() string { return abilities.ImageConvert }

// Timeout implements jobs.Tool.
//
// Three minutes. The benchmark for a 4K conversion inside the production
// image is under two seconds of engine time, so anything approaching this is
// a pathological input or a stuck worker, and either should be stopped rather
// than waited on. It is not tighter than that because an edit chains three
// libvips processes and a cold page cache makes the first one slow.
func (*ImageConvert) Timeout() time.Duration { return 3 * time.Minute }

// Accepted implements jobs.Accepter: the types this build can actually read,
// probed at boot rather than declared.
//
// Checked again at the point of use because the file and the tool are chosen
// in two separate requests: the upload endpoint accepts everything the
// service knows.
func (t *ImageConvert) Accepted() []string { return t.media.ReadableImageTypes() }

// ImageConvertOptions is the validated input of an image-convert conversion.
//
// It is a separate type from the library's options on purpose. This one is
// the public contract — the names a client sends, the bounds it is held to and
// the words the documentation uses — and it has to be able to stay still while
// the library's own options change.
type ImageConvertOptions struct {
	// To is the output format.
	To string `json:"to" validate:"trim,max=16" doc:"Output format. GET /api/v1/tools/image-convert/formats lists the ones this server can write."`
	// Quality is the lossy formats' dial.
	Quality int `json:"quality" validate:"min=0,max=100" doc:"1 to 100 for JPEG, WebP, AVIF and JPEG XL. 0 uses the format's default."`
	// Effort trades encode time for file size.
	Effort int `json:"effort" validate:"min=0,max=10" doc:"0 to 10, meaning depends on the format. 0 uses the default."`
	// Lossless asks for a lossless encode where the format has one.
	Lossless bool `json:"lossless" doc:"Lossless encoding for WebP, AVIF and JPEG XL. Ignored by formats with no lossless mode."`
	// Resize and the geometry.
	Resize string `json:"resize" validate:"trim,max=8"      doc:"fit, fill or exact. Empty leaves the size alone."`
	Width  int    `json:"width"  validate:"min=0,max=20000" doc:"Target width in pixels. 0 leaves that axis open."`
	Height int    `json:"height" validate:"min=0,max=20000" doc:"Target height in pixels. 0 leaves that axis open."`
	// Upscale allows the image to be made larger than it is.
	Upscale bool `json:"upscale" doc:"Allow making the image larger. Off by default: upscaling produces a bigger file that looks worse."`
	// Crop takes an area out of the source before anything else.
	CropLeft   int `json:"crop_left"   validate:"min=0,max=20000" doc:"Crop origin from the left, in source pixels."`
	CropTop    int `json:"crop_top"    validate:"min=0,max=20000" doc:"Crop origin from the top, in source pixels."`
	CropWidth  int `json:"crop_width"  validate:"min=0,max=20000" doc:"Crop width. 0 means no crop."`
	CropHeight int `json:"crop_height" validate:"min=0,max=20000" doc:"Crop height. 0 means no crop."`
	// Rotate and Flip.
	Rotate int    `json:"rotate" validate:"min=0,max=270" doc:"Quarter turns clockwise: 0, 90, 180 or 270."`
	Flip   string `json:"flip"   validate:"trim,max=16"   doc:"horizontal or vertical. Empty means no flip."`
	// KeepMetadata keeps EXIF, XMP and IPTC.
	KeepMetadata bool `json:"keep_metadata" doc:"Keep EXIF, XMP and IPTC. Off by default: a photograph's EXIF often carries GPS coordinates and a device serial number."`
	// KeepAnimation keeps every frame of an animated input.
	KeepAnimation bool `json:"keep_animation" doc:"Keep every frame of an animated GIF or WebP. Off by default; only applies when the output format is animated too."`
}

// The resize modes and flips a client may name, as the API documents them.
var (
	imageResizeModes = []string{
		string(vipsimage.ResizeFit),
		string(vipsimage.ResizeFill),
		string(vipsimage.ResizeExact),
	}
	imageFlips = []string{
		string(vipsimage.FlipHorizontal),
		string(vipsimage.FlipVertical),
	}
	imageRotations = []int{0, 90, 180, 270}
)

// ImageResizeModes returns the accepted resize modes, for the documentation
// and the interface.
func ImageResizeModes() []string { return slices.Clone(imageResizeModes) }

// ImageFlips returns the accepted flips.
func ImageFlips() []string { return slices.Clone(imageFlips) }

// ImageRotations returns the accepted rotations.
func ImageRotations() []int { return slices.Clone(imageRotations) }

// Normalise fills in every omitted field and refuses a value that is not one
// of the documented ones.
//
// Every enum is checked here, at submission, rather than where the engine
// command line is built. That matters for two separate reasons. A bad request
// then fails at once with a 422 naming the field, instead of being accepted,
// queued, and failing on a worker a minute later. And the values go into
// libvips's own option syntax, which libvips parses itself — argv safety says
// nothing about a sub-language the engine interprets — so an allowlist that
// can be enumerated is the only constraint that holds.
//
// The enums are checked in code rather than with a `oneof` tag because an
// omitted field must mean "the default", and `oneof` has no way to say "one
// of these, or empty".
func (in ImageConvertOptions) Normalise(writable []vipsimage.Format) (ImageConvertOptions, error) {
	in.To = strings.ToLower(strings.TrimSpace(in.To))
	if in.To == "" {
		return ImageConvertOptions{}, z.FieldError("options.to",
			"Choose an output format. This server can write: "+joinFormats(writable)+".")
	}

	if !slices.Contains(writable, vipsimage.Format(in.To)) {
		// The list comes from what the engine was probed to do, not from a
		// constant: a build without AVIF must not name AVIF as an option.
		return ImageConvertOptions{}, z.FieldError("options.to",
			"This server cannot write "+in.To+". It can write: "+joinFormats(writable)+".")
	}

	in.Resize = strings.ToLower(strings.TrimSpace(in.Resize))
	if in.Resize != "" && !slices.Contains(imageResizeModes, in.Resize) {
		return ImageConvertOptions{}, z.FieldError("options.resize",
			"Unknown resize mode. Use one of: "+strings.Join(imageResizeModes, ", ")+".")
	}

	if in.Resize != "" && in.Width == 0 && in.Height == 0 {
		return ImageConvertOptions{}, z.FieldError("options.width",
			"A resize needs a width, a height, or both.")
	}

	// fill and exact both produce exactly the box, so both axes are needed:
	// "fill a 200-wide box of unknown height" has no answer.
	if (in.Resize == string(vipsimage.ResizeFill) || in.Resize == string(vipsimage.ResizeExact)) &&
		(in.Width == 0 || in.Height == 0) {
		return ImageConvertOptions{}, z.FieldError("options.height",
			"The "+in.Resize+" mode needs both a width and a height.")
	}

	in.Flip = strings.ToLower(strings.TrimSpace(in.Flip))
	if in.Flip != "" && !slices.Contains(imageFlips, in.Flip) {
		return ImageConvertOptions{}, z.FieldError("options.flip",
			"Unknown flip. Use one of: "+strings.Join(imageFlips, ", ")+".")
	}

	if !slices.Contains(imageRotations, in.Rotate) {
		return ImageConvertOptions{}, z.FieldError("options.rotate",
			"The rotation must be 0, 90, 180 or 270 degrees.")
	}

	// A crop is either fully specified or absent. A width with no height is
	// the shape of a client bug, and guessing the missing half would crop
	// something nobody asked for.
	switch {
	case in.CropWidth == 0 && in.CropHeight == 0:
		// No crop. The origin is then meaningless and is cleared, so the
		// stored options cannot imply one.
		in.CropLeft, in.CropTop = 0, 0
	case in.CropWidth > 0 && in.CropHeight > 0:
		// A crop, validated against the real image in the worker: the
		// dimensions are not known here.
	default:
		return ImageConvertOptions{}, z.FieldError("options.crop_width",
			"A crop needs both a width and a height.")
	}

	if in.Quality < 0 || in.Quality > 100 {
		return ImageConvertOptions{}, z.FieldError("options.quality",
			"The quality must be between 1 and 100.")
	}

	if in.Effort < 0 || in.Effort > 10 {
		return ImageConvertOptions{}, z.FieldError("options.effort",
			"The effort must be between 0 and 10.")
	}

	return in, nil
}

// joinFormats renders a format list for a message.
func joinFormats(formats []vipsimage.Format) string {
	out := make([]string, 0, len(formats))
	for _, f := range formats {
		out = append(out, string(f))
	}

	if len(out) == 0 {
		return "nothing, because no image engine is available"
	}

	return strings.Join(out, ", ")
}

// library turns the public options into the library's.
func (in ImageConvertOptions) library() vipsimage.EditOptions {
	out := vipsimage.EditOptions{
		Rotate: vipsimage.Rotation(in.Rotate),
		Flip:   vipsimage.Flip(in.Flip),
		Encode: vipsimage.Options{
			To:            vipsimage.Format(in.To),
			Quality:       in.Quality,
			Effort:        in.Effort,
			Lossless:      in.Lossless,
			Resize:        vipsimage.Resize(in.Resize),
			Width:         in.Width,
			Height:        in.Height,
			Upscale:       in.Upscale,
			KeepMetadata:  in.KeepMetadata,
			KeepAnimation: in.KeepAnimation,
		},
	}

	if in.CropWidth > 0 && in.CropHeight > 0 {
		out.Crop = &vipsimage.Rect{
			Left:   in.CropLeft,
			Top:    in.CropTop,
			Width:  in.CropWidth,
			Height: in.CropHeight,
		}
	}

	return out
}

// ParseImageConvertOptions validates and decodes the options stored on a
// conversion. It is exported so the submit endpoint validates exactly what
// the worker will later read, rather than something close to it.
func ParseImageConvertOptions(
	raw json.RawMessage,
	writable []vipsimage.Format,
) (ImageConvertOptions, error) {
	var in ImageConvertOptions

	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &in); err != nil {
			return ImageConvertOptions{}, z.FieldError("options", "The options are not a JSON object.")
		}
	}

	return in.Normalise(writable)
}

// ValidateOptions implements jobs.OptionsValidator. What it returns is what is
// stored, so the worker reads the same options that were validated.
func (t *ImageConvert) ValidateOptions(raw json.RawMessage) (json.RawMessage, error) {
	opts, err := ParseImageConvertOptions(raw, t.media.WritableImageFormats())
	if err != nil {
		return nil, err
	}

	out, err := json.Marshal(opts)
	if err != nil {
		return nil, fmt.Errorf("tools: encode the normalised options: %w", err)
	}

	return out, nil
}

// Run implements jobs.Tool.
func (t *ImageConvert) Run(ctx context.Context, w jobs.Work) (jobs.Outcome, error) {
	opts, err := ParseImageConvertOptions(w.Options, t.media.WritableImageFormats())
	if err != nil {
		// Stored options that no longer validate mean the conversion was
		// accepted by an older version of this tool, or the engine lost a
		// format in an upgrade. Either way it can never succeed, so it is
		// settled rather than retried.
		return jobs.Outcome{}, jobs.Fail(jobs.UnsupportedInput, err,
			"the stored options are not valid for this tool")
	}

	engines, ok := vipsEngine(t.media.Engines)
	if !ok {
		return jobs.Outcome{}, jobs.Failf(jobs.Internal, "the image engine is not available")
	}

	input, err := inputFile(ctx, t.files, w, "the image converter", t.Accepted())
	if err != nil {
		return jobs.Outcome{}, err
	}

	if err := w.Report(jobs.Step{Percent: 10, Phase: "reading"}); err != nil {
		return jobs.Outcome{}, err
	}

	// A fixed local name, never the upload's: libvips reads its loader
	// options out of the filename, so a file named `x[shrink=8].jpg` would
	// decode at an eighth scale. See localInput.
	local, err := localInput(ctx, t.files, w, input)
	if err != nil {
		return jobs.Outcome{}, err
	}

	if err := w.Report(jobs.Step{Percent: 30, Phase: "converting"}); err != nil {
		return jobs.Outcome{}, err
	}

	result, err := vipsimage.Edit(ctx, engines, t.media.Runner, w.Dir, local, opts.library())
	if err != nil {
		return jobs.Outcome{}, classifyImage(err)
	}

	if err := w.Report(jobs.Step{Percent: 80, Phase: "storing"}); err != nil {
		return jobs.Outcome{}, err
	}

	out, err := storeResult(ctx, t.files, w, input, result.Path, result.Format.MIME())
	if err != nil {
		return jobs.Outcome{}, err
	}

	// The engine's own processor time, which is the honest figure to charge a
	// quota with: it is smaller than the wall-clock the pipeline would
	// otherwise assume.
	out.CPUMs = result.CPUTime

	return out, nil
}
