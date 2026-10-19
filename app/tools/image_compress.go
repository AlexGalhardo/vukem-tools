package tools

import (
	"context"
	"encoding/json"
	"errors"
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

// NameImageCompress is the tool's stable id, the catalogue entry and the URL
// segment.
const NameImageCompress = "image-compress"

// ImageCompress makes an image smaller without changing what it is.
//
// A separate tool from image-convert rather than an option on it, because the
// two answer different questions and a person arrives with one of them. "Make
// this smaller" has no output format, no geometry and no crop — it has a
// budget and a tolerance — and burying it as a flag on a tool with fourteen
// options is how somebody ends up accidentally resizing a photograph they
// only wanted to shrink the file of.
type ImageCompress struct {
	files *files.Store
	media *Media
}

// NewImageCompress returns the tool.
func NewImageCompress(f *files.Store, m *Media) *ImageCompress {
	return &ImageCompress{files: f, media: m}
}

// Name implements jobs.Tool.
func (*ImageCompress) Name() string { return NameImageCompress }

// Ability implements jobs.Tool.
func (*ImageCompress) Ability() string { return abilities.ImageCompress }

// Timeout implements jobs.Tool.
//
// Three minutes, the same as image-convert and for the same reasons, with one
// of its own: a compression runs the encoder two or three times on the same
// image to find the smallest output, so its ceiling is a small multiple of a
// conversion's rather than the same number.
func (*ImageCompress) Timeout() time.Duration { return 3 * time.Minute }

// Accepted implements jobs.Accepter.
//
// The formats this build can read **and** this tool has a compression recipe
// for, which is a narrower list than image-convert's: a format with no recipe
// would be re-encoded with the encoder's defaults, and an encoder run with
// settings nobody chose is not compression, it is a re-encode that may well
// be larger.
func (t *ImageCompress) Accepted() []string {
	readable := t.media.ReadableImageTypes()

	var out []string

	for _, f := range vipsimage.CompressFormats() {
		if mime := f.MIME(); slices.Contains(readable, mime) {
			out = append(out, mime)
		}
	}

	return out
}

// ImageCompressOptions is the validated input of an image-compress
// conversion.
type ImageCompressOptions struct {
	// Mode is how far the pixels may move.
	Mode string `json:"mode" validate:"trim,max=16" doc:"lossless or lossy. Empty picks by format: lossless for PNG and GIF, lossy for JPEG and WebP."`
	// Quality is the lossy dial.
	Quality int `json:"quality" validate:"min=0,max=100" doc:"1 to 100, for a lossy compression. 0 uses the format's default."`
	// Effort trades encode time for size.
	Effort int `json:"effort" validate:"min=0,max=10" doc:"1 to 10, meaning depends on the format. 0 uses the highest the encoder supports."`
}

// ImageCompressModes returns the modes a client may name, for the
// documentation and the interface.
func ImageCompressModes() []string {
	out := make([]string, 0, len(vipsimage.CompressModes()))
	for _, m := range vipsimage.CompressModes() {
		out = append(out, string(m))
	}

	return out
}

// ImageCompressFormats returns the formats this tool compresses, lowercased,
// for the documentation and the interface.
func ImageCompressFormats() []string {
	out := make([]string, 0, len(vipsimage.CompressFormats()))
	for _, f := range vipsimage.CompressFormats() {
		out = append(out, string(f))
	}

	return out
}

// Normalise fills in every omitted field and refuses a value that is not one
// of the documented ones.
func (in ImageCompressOptions) Normalise() (ImageCompressOptions, error) {
	in.Mode = strings.ToLower(strings.TrimSpace(in.Mode))

	if in.Mode != "" && !slices.Contains(ImageCompressModes(), in.Mode) {
		return ImageCompressOptions{}, z.FieldError("options.mode",
			"Unknown mode. Use one of: "+strings.Join(ImageCompressModes(), ", ")+".")
	}

	if in.Quality < 0 || in.Quality > 100 {
		return ImageCompressOptions{}, z.FieldError("options.quality",
			"The quality must be between 1 and 100.")
	}

	if in.Effort < 0 || in.Effort > 10 {
		return ImageCompressOptions{}, z.FieldError("options.effort",
			"The effort must be between 1 and 10.")
	}

	return in, nil
}

// library turns the public options into the library's.
func (in ImageCompressOptions) library() vipsimage.CompressOptions {
	return vipsimage.CompressOptions{
		Mode:    vipsimage.CompressMode(in.Mode),
		Quality: in.Quality,
		Effort:  in.Effort,
	}
}

// ParseImageCompressOptions validates and decodes the options stored on a
// conversion.
func ParseImageCompressOptions(raw json.RawMessage) (ImageCompressOptions, error) {
	var in ImageCompressOptions

	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &in); err != nil {
			return ImageCompressOptions{}, z.FieldError("options", "The options are not a JSON object.")
		}
	}

	return in.Normalise()
}

// ValidateOptions implements jobs.OptionsValidator.
func (*ImageCompress) ValidateOptions(raw json.RawMessage) (json.RawMessage, error) {
	opts, err := ParseImageCompressOptions(raw)
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
func (t *ImageCompress) Run(ctx context.Context, w jobs.Work) (jobs.Outcome, error) {
	opts, err := ParseImageCompressOptions(w.Options)
	if err != nil {
		return jobs.Outcome{}, jobs.Fail(jobs.UnsupportedInput, err,
			"the stored options are not valid for this tool")
	}

	engines, ok := vipsEngine(t.media.Engines)
	if !ok {
		return jobs.Outcome{}, jobs.Failf(jobs.Internal, "the image engine is not available")
	}

	input, err := inputFile(ctx, t.files, w, "the image compressor", t.Accepted())
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

	if err := w.Report(jobs.Step{Percent: 30, Phase: "compressing"}); err != nil {
		return jobs.Outcome{}, err
	}

	result, err := vipsimage.Compress(ctx, engines, t.media.Runner, w.Dir, local, opts.library())
	if err != nil {
		return jobs.Outcome{}, classifyCompress(err)
	}

	// The sentence goes on the progress trail, which is where a person reads
	// what happened. It is the whole answer for an unchanged file: "nothing
	// was stored" would be true and useless, and a silent no-op reads as a
	// broken tool.
	if err := w.Report(jobs.Step{Percent: 85, Phase: "storing", Message: result.Did}); err != nil {
		return jobs.Outcome{}, err
	}

	out, err := storeResult(ctx, t.files, w, input, result.Path, result.Format.MIME())
	if err != nil {
		return jobs.Outcome{}, err
	}

	out.CPUMs = result.CPUTime

	return out, nil
}

// classifyCompress maps the compression errors onto the taxonomy.
//
// Only the one this package adds; everything else is image's and already
// mapped. A mode the format cannot offer is the request's problem and can
// never succeed on a retry, so it is a refusal rather than a failure.
func classifyCompress(err error) error {
	if mode, ok := errors.AsType[*vipsimage.CompressModeError](err); ok {
		return jobs.Refuse(jobs.UnsupportedInput, err, mode.Message(), mode.Error())
	}

	if unsupported, ok := errors.AsType[*vipsimage.UnsupportedError](err); ok {
		return jobs.Refuse(jobs.UnsupportedInput, err, compressUnsupportedMessage(), unsupported.Error())
	}

	return classifyImage(err)
}

// compressUnsupportedMessage names the formats this tool compresses, because
// "that format is not supported" without the list leaves somebody guessing.
func compressUnsupportedMessage() string {
	names := make([]string, 0, len(vipsimage.CompressFormats()))
	for _, f := range vipsimage.CompressFormats() {
		names = append(names, strings.ToUpper(string(f)))
	}

	return "This tool compresses " + strings.Join(names, ", ") +
		". Use image-convert to change an image into one of those first."
}
