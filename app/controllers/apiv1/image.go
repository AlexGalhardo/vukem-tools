package apiv1

import (
	"encoding/json"

	"github.com/AlexGalhardo/vukem/web"

	"github.com/AlexGalhardo/vukem-tools/app/tools"
)

// MediaCapabilities is what the server can actually convert.
//
// It is published because the frontend must not hold a format list of its
// own, which is a roadmap requirement and also the only way the list can be
// true: Debian's libvips has heifsave but libheif ships no encoder, so AVIF
// looks available and is not. Asking the engine at boot and publishing the
// answer makes an unavailable format invisible rather than offered and then
// refused.
type MediaCapabilities struct {
	// Formats is every format this build can read or write, with which.
	Formats []ImageFormat `json:"formats" doc:"Every image format, with whether this server can read it, write it, or both."`
	// Writable is the subset that can be produced, which is what an output
	// picker should show.
	Writable []string `json:"writable" doc:"The formats this server can write, by name."`
	// Readable is the media types that may be submitted.
	Readable []string `json:"readable" doc:"The media types this server accepts as input."`
	// ResizeModes, Flips and Rotations are the accepted enum values, so a
	// client builds its controls from the server rather than from a list it
	// keeps in sync by hand.
	ResizeModes []string `json:"resize_modes" doc:"fit, fill or exact."`
	Flips       []string `json:"flips"        doc:"horizontal or vertical."`
	Rotations   []int    `json:"rotations"    doc:"The quarter turns a client may ask for."`
}

// ImageFormat is one format's support in this build.
type ImageFormat struct {
	Name      string `json:"name"      doc:"The identifier to send as options.to."`
	Label     string `json:"label"     doc:"What to call it in an interface."`
	MIME      string `json:"mime"      doc:"The media type a file of this format has."`
	Extension string `json:"extension" doc:"The canonical extension, with the dot."`
	Read      bool   `json:"read"      doc:"Whether this server can read it."`
	Write     bool   `json:"write"     doc:"Whether this server can produce it."`
	Animated  bool   `json:"animated"  doc:"Whether the format can hold more than one frame."`
}

// SubmitImageConvert is the body of POST /api/v1/tools/image-convert.
//
// One submit route per tool rather than one generic route carrying opaque
// JSON, for the reason SubmitPixelSVG records: the typed-handler validator
// derives its schema from the Go type, so neither json.RawMessage nor
// map[string]any can be an input field at all, and the OpenAPI document then
// describes each tool's options exactly instead of saying "an object".
type SubmitImageConvert struct {
	// FileID is an upload of this caller's that is not already attached to a
	// conversion.
	FileID string `json:"file_id" validate:"required,trim" doc:"The id of an upload of yours, from the resumable upload endpoint."`
	// IdempotencyKey makes a repeated submission return the first conversion
	// instead of starting a second one. Scoped to the caller.
	IdempotencyKey string `json:"idempotency_key" validate:"trim,max=128" doc:"Repeat a submission safely: the same key returns the same conversion."`
	// Options are the tool's own options.
	Options tools.ImageConvertOptions `json:"options" doc:"How to convert. Only the output format is required."`
}

// ImageConvert submits an image conversion:
// POST /api/v1/tools/image-convert.
func (h *Media) ImageConvert(c *web.Ctx, in SubmitImageConvert) (ConversionView, error) {
	// Normalised here as well as in the tool's ValidateOptions, because this
	// is where a bad value becomes a 422 naming the field. The pipeline
	// validates again before it stores, which is what guarantees the worker
	// reads exactly what was accepted.
	opts, err := in.Options.Normalise(h.writableImageFormats())
	if err != nil {
		return ConversionView{}, err
	}

	encoded, err := json.Marshal(opts)
	if err != nil {
		return ConversionView{}, err
	}

	return h.submit(c, submission{
		Tool:           "image-convert",
		FileID:         in.FileID,
		IdempotencyKey: in.IdempotencyKey,
		Options:        encoded,
	})
}

// ImageCapabilities answers GET /api/v1/tools/image-convert/formats.
func (h *Media) ImageCapabilities(_ *web.Ctx, _ struct{}) (MediaCapabilities, error) {
	caps := h.imageCapabilities()

	out := MediaCapabilities{
		Formats:     make([]ImageFormat, 0, len(caps)),
		ResizeModes: tools.ImageResizeModes(),
		Flips:       tools.ImageFlips(),
		Rotations:   tools.ImageRotations(),
	}

	for _, c := range caps {
		out.Formats = append(out.Formats, ImageFormat{
			Name:      string(c.Format),
			Label:     c.Label,
			MIME:      c.MIME,
			Extension: c.Extension,
			Read:      c.Read,
			Write:     c.Write,
			Animated:  c.Animated,
		})

		if c.Write {
			out.Writable = append(out.Writable, string(c.Format))
		}
	}

	out.Readable = h.readableImageTypes()

	return out, nil
}
