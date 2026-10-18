package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/AlexGalhardo/vukem/z"

	"github.com/AlexGalhardo/vukem-tools/app/abilities"
	"github.com/AlexGalhardo/vukem-tools/app/models"
	"github.com/AlexGalhardo/vukem-tools/internal/detect"
	"github.com/AlexGalhardo/vukem-tools/internal/files"
	"github.com/AlexGalhardo/vukem-tools/internal/jobs"
	"github.com/AlexGalhardo/vukem-tools/internal/pixelsvg"
)

// PixelSVG turns pixel art into SVG. The conversion itself lives in
// internal/pixelsvg, which knows nothing about jobs, storage or HTTP; this is
// the layer that reads the input out of the bucket, reports progress and puts
// the result back.
type PixelSVG struct {
	files *files.Store
}

// NewPixelSVG returns the tool.
func NewPixelSVG(f *files.Store) *PixelSVG { return &PixelSVG{files: f} }

// Name implements jobs.Tool. It is the stable id stored on every conversion
// and published by GET /api/v1/tools, so it is part of the public API.
func (*PixelSVG) Name() string { return "pixels-to-svg" }

// Ability implements jobs.Tool.
func (*PixelSVG) Ability() string { return abilities.PixelsSVG }

// Timeout implements jobs.Tool.
//
// Two minutes is generous: the benchmark for the slowest mode at the maximum
// 2048x2048 is well under a second, so anything approaching this bound is a
// pathological input or a stuck worker, and either should be stopped rather
// than waited on.
func (*PixelSVG) Timeout() time.Duration { return 2 * time.Minute }

// Accepted lists the types this tool reads. It is checked again here, at the
// point of use, because the file and the tool are chosen in two separate
// requests: the upload endpoint accepts everything the service knows.
func (*PixelSVG) Accepted() []string {
	return []string{"image/png", "image/gif", "image/bmp", "image/jpeg"}
}

// PixelSVGOptions is the validated input of a pixels-to-svg conversion.
//
// It is a separate type from pixelsvg.Options on purpose. This one is the
// public contract — the names a client sends, the bounds it is held to and
// the words the documentation uses — and it must be able to stay still while
// the library's own options change.
type PixelSVGOptions struct {
	// Mode selects the geometry: one rectangle per pixel, merged rectangles,
	// or traced outlines. rect is the default because it is both the
	// smallest and the fastest for ordinary sprites.
	Mode string `json:"mode" validate:"trim,oneof=square rect contour" doc:"square, rect or contour. Default rect."`
	// Alpha decides what happens to partly transparent pixels.
	Alpha string `json:"alpha" validate:"trim,oneof=opacity drop" doc:"opacity keeps partial transparency, drop removes it. Default opacity."`
	// MaxColors caps the palette. 0 means the library's cap; -1 means no cap,
	// which refuses an image with more colours rather than quantising it.
	MaxColors int `json:"max_colors" validate:"min=-1,max=256" doc:"Palette cap, 1 to 256. 0 uses the default, -1 means no reduction."`
	// Tolerance merges colours closer together than this distance.
	Tolerance int `json:"tolerance" validate:"min=0,max=255" doc:"Merge colours closer than this (0 to 255). 0 disables merging."`
	// Scale multiplies only the root element's width and height, so the
	// geometry is identical at any scale and the file does not grow.
	Scale int `json:"scale" validate:"min=0,max=64" doc:"Multiplies the rendered size. 0 and 1 both mean actual size."`
	// Simplify is the contour simplification epsilon in pixels, used only by
	// contour mode.
	Simplify float64 `json:"simplify" validate:"min=0,max=16" doc:"Contour simplification in pixels, contour mode only. 0 keeps it pixel-exact."`
}

// PixelSVGSchema validates the options a client sends.
//
// The oneof rules accept only the documented values, and empty is not one of
// them, so the schema fills the defaults before validating rather than
// letting a client discover that omitting a field is an error.
func PixelSVGSchema() z.Schema[PixelSVGOptions] { return z.For[PixelSVGOptions]() }

// defaults returns in with every omitted field set to its default.
func (in PixelSVGOptions) defaults() PixelSVGOptions {
	if in.Mode == "" {
		in.Mode = string(pixelsvg.ModeRect)
	}

	if in.Alpha == "" {
		in.Alpha = string(pixelsvg.AlphaOpacity)
	}

	return in
}

// library turns the public options into the library's.
func (in PixelSVGOptions) library() pixelsvg.Options {
	return pixelsvg.Options{
		Mode:              pixelsvg.Mode(in.Mode),
		Alpha:             pixelsvg.AlphaMode(in.Alpha),
		MaxColors:         in.MaxColors,
		Tolerance:         in.Tolerance,
		Scale:             in.Scale,
		SimplifyTolerance: in.Simplify,
	}
}

// ParsePixelSVGOptions validates and decodes the options stored on a
// conversion. It is exported so the submit endpoint validates exactly what
// the worker will later read, rather than something close to it.
func ParsePixelSVGOptions(raw json.RawMessage) (PixelSVGOptions, error) {
	var in PixelSVGOptions

	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &in); err != nil {
			return PixelSVGOptions{}, z.FieldError("options", "The options are not a JSON object.")
		}
	}

	in = in.defaults()

	out, err := PixelSVGSchema().Parse(in)
	if err != nil {
		return PixelSVGOptions{}, err
	}

	return out, nil
}

// Run implements jobs.Tool.
func (t *PixelSVG) Run(ctx context.Context, w jobs.Work) (jobs.Outcome, error) {
	opts, err := ParsePixelSVGOptions(w.Options)
	if err != nil {
		// Stored options that no longer validate mean the conversion was
		// accepted by an older version of this tool. It will never succeed,
		// so it is the input's problem, not a transient one.
		return jobs.Outcome{}, jobs.Fail(jobs.UnsupportedInput, err, "the stored options are not valid for this tool")
	}

	input, err := t.input(ctx, w)
	if err != nil {
		return jobs.Outcome{}, err
	}

	if err := w.Report(jobs.Step{Percent: 10, Phase: "reading"}); err != nil {
		return jobs.Outcome{}, err
	}

	local, err := t.fetch(ctx, w, input)
	if err != nil {
		return jobs.Outcome{}, err
	}

	if err := w.Report(jobs.Step{Percent: 30, Phase: "tracing"}); err != nil {
		return jobs.Outcome{}, err
	}

	svg, err := t.convert(ctx, w, local, opts)
	if err != nil {
		return jobs.Outcome{}, err
	}

	if err := w.Report(jobs.Step{Percent: 80, Phase: "storing"}); err != nil {
		return jobs.Outcome{}, err
	}

	return t.store(ctx, w, input, svg)
}

// input returns the file this conversion is to read, refusing a type this
// tool cannot decode.
func (t *PixelSVG) input(ctx context.Context, w jobs.Work) (models.File, error) {
	inputs, err := t.files.ForConversion(ctx, w.ConversionID, models.FileInput)
	if err != nil {
		return models.File{}, jobs.Fail(jobs.Internal, err, "look up the conversion's input")
	}

	if len(inputs) == 0 {
		// Nothing is attached, and nothing ever will be: the submit endpoint
		// attaches the input in the same transaction that creates the
		// conversion.
		return models.File{}, jobs.Failf(jobs.Internal, "conversion %s has no input file", w.ConversionID)
	}

	in := inputs[0]

	if slices.Contains(t.Accepted(), in.MIME) {
		return in, nil
	}

	kind, _ := detect.KindOf(in.MIME)
	if kind.Label == "" {
		kind.Label = in.MIME
	}

	return models.File{}, jobs.Failf(jobs.UnsupportedInput,
		"%s is not a type pixels-to-svg reads", kind.Label)
}

// fetch copies the input out of the bucket into the job's own directory.
//
// It is copied rather than streamed straight into the conversion because the
// library reads the header, then decodes from the start: that needs two
// passes, and two passes over a bucket means paying for the object twice and
// failing halfway if the connection drops. A local file is also what makes
// the conversion's own timing measurable rather than network-bound.
func (t *PixelSVG) fetch(ctx context.Context, w jobs.Work, in models.File) (string, error) {
	body, err := t.files.Open(ctx, in)
	if err != nil {
		return "", jobs.Fail(jobs.Internal, err, "open the input")
	}

	defer func() { _ = body.Close() }()

	path := filepath.Join(w.Dir, "input"+filepath.Ext(in.Name))

	dst, err := os.Create(path)
	if err != nil {
		return "", jobs.Fail(jobs.DiskFull, err, "create the local input file")
	}

	if _, err := io.Copy(dst, body); err != nil {
		_ = dst.Close()

		return "", jobs.Fail(jobs.DiskFull, err, "write the local input file")
	}

	if err := dst.Close(); err != nil {
		return "", jobs.Fail(jobs.DiskFull, err, "close the local input file")
	}

	return path, nil
}

// convert runs the library, writing the SVG to the job's directory.
func (t *PixelSVG) convert(
	ctx context.Context,
	w jobs.Work,
	input string,
	opts PixelSVGOptions,
) (string, error) {
	src, err := os.Open(input)
	if err != nil {
		return "", jobs.Fail(jobs.Internal, err, "reopen the local input")
	}

	defer func() { _ = src.Close() }()

	out := filepath.Join(w.Dir, "output.svg")

	dst, err := os.Create(out)
	if err != nil {
		return "", jobs.Fail(jobs.DiskFull, err, "create the output file")
	}

	_, convErr := pixelsvg.Convert(ctx, dst, src, opts.library())

	if err := dst.Close(); err != nil && convErr == nil {
		return "", jobs.Fail(jobs.DiskFull, err, "close the output file")
	}

	if convErr != nil {
		return "", classify(convErr)
	}

	return out, nil
}

// classify maps the library's errors onto the taxonomy.
//
// The library's errors are about the input — too large, too many colours, not
// an image it reads — so almost all of them are the user's to fix, and
// retrying would waste a worker to reach the same answer. Only something
// unrecognised is treated as transient.
func classify(err error) error {
	var (
		dimension *pixelsvg.DimensionError
		colors    *pixelsvg.ColorCountError
		size      *pixelsvg.InputSizeError
		option    *pixelsvg.OptionError
	)

	switch {
	case errors.As(err, &dimension):
		return jobs.Fail(jobs.InputTooLarge, err,
			fmt.Sprintf("the image is larger than %dx%d pixels", pixelsvg.MaxDimension, pixelsvg.MaxDimension))
	case errors.As(err, &size):
		return jobs.Fail(jobs.InputTooLarge, err, "the image file is too large to decode")
	case errors.As(err, &colors):
		return jobs.Fail(jobs.UnsupportedInput, err, "the image has more colours than the palette cap allows")
	case errors.As(err, &option):
		return jobs.Fail(jobs.UnsupportedInput, err, "the options are not valid for this image")
	case errors.Is(err, pixelsvg.ErrEmptyImage), errors.Is(err, pixelsvg.ErrNilImage):
		return jobs.Fail(jobs.UnsupportedInput, err, "the image is empty")
	case strings.Contains(err.Error(), "image:"):
		// The standard library's decoders report a bad or unsupported image
		// this way; there is no sentinel to match on.
		return jobs.Fail(jobs.UnsupportedInput, err, "the image could not be decoded")
	default:
		return jobs.Fail(jobs.EngineFailure, err, "the conversion failed")
	}
}

// store puts the SVG in the bucket and records it against the conversion.
func (t *PixelSVG) store(
	ctx context.Context,
	w jobs.Work,
	input models.File,
	svg string,
) (jobs.Outcome, error) {
	body, err := os.Open(svg)
	if err != nil {
		return jobs.Outcome{}, jobs.Fail(jobs.Internal, err, "open the result to store it")
	}

	defer func() { _ = body.Close() }()

	kind, ok := detect.KindOf("image/svg+xml")
	if !ok {
		return jobs.Outcome{}, jobs.Failf(jobs.Internal, "image/svg+xml is not a recognised type")
	}

	stored, err := t.files.Put(ctx, files.Incoming{
		UserID:       w.UserID,
		ConversionID: w.ConversionID,
		Role:         models.FileOutput,
		// The result is named after the input, so a user downloading several
		// conversions can tell them apart.
		Name: strings.TrimSuffix(input.Name, filepath.Ext(input.Name)) + ".svg",
		Kind: kind,
	}, body)
	if err != nil {
		return jobs.Outcome{}, jobs.Fail(jobs.Internal, err, "store the result")
	}

	return jobs.Outcome{BytesOut: stored.SizeBytes, CPUMs: 0}, nil
}

// ValidateOptions implements jobs.OptionsValidator. What it returns is what
// is stored, so the worker reads the same options that were validated.
func (*PixelSVG) ValidateOptions(raw json.RawMessage) (json.RawMessage, error) {
	opts, err := ParsePixelSVGOptions(raw)
	if err != nil {
		return nil, err
	}

	out, err := json.Marshal(opts)
	if err != nil {
		return nil, fmt.Errorf("tools: encode the normalised options: %w", err)
	}

	return out, nil
}
