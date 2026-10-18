package apiv1

import (
	"encoding/json"
	"slices"

	"github.com/AlexGalhardo/vukem/web"

	"github.com/AlexGalhardo/vukem-tools/app/tools"
	"github.com/AlexGalhardo/vukem-tools/internal/video"
)

// VideoCapabilities is what this server can produce for video.
//
// The codec list is probed rather than declared, and the reason is licensing
// rather than taste: x264 and x265 are GPL-only, so an ffmpeg built without
// --enable-gpl has no H.264 and no H.265 encoder at all. A client that built
// its picker from a constant would offer them on such a deploy and get a
// refusal; building it from this cannot.
type VideoCapabilities struct {
	// Containers is every container, with the codecs each one carries here.
	Containers []VideoContainer `json:"containers" doc:"Every output container, with the codecs it carries."`
	// Codecs is the video codecs this build can actually encode.
	Codecs []string `json:"codecs" doc:"The video codecs this server can encode. An ffmpeg built without GPL support has no H.264 or H.265."`
	// GPLBuild says whether the engine is the GPL build. It is published so
	// a client can explain the absence rather than only report it.
	GPLBuild bool `json:"gpl_build" doc:"Whether the video engine is built with GPL support, which H.264 and H.265 require."`
	// Dithers is the accepted dithering modes for a GIF.
	Dithers []string `json:"dithers" doc:"The dithering modes video-to-gif accepts."`
	// MaxGIFSeconds is the longest range a GIF may cover.
	MaxGIFSeconds float64 `json:"max_gif_seconds" doc:"The longest range video-to-gif will convert, in seconds."`
	// MaxSeconds is the longest input a conversion accepts.
	MaxSeconds float64 `json:"max_seconds" doc:"The longest video this server converts, in seconds."`
}

// VideoContainer is one container and what it carries.
type VideoContainer struct {
	Name        string   `json:"name"         doc:"The identifier to send as options.container."`
	Label       string   `json:"label"        doc:"What to call it in an interface."`
	Extension   string   `json:"extension"    doc:"The canonical extension, with the dot."`
	VideoCodecs []string `json:"video_codecs" doc:"The video codecs this container carries here."`
	AudioCodecs []string `json:"audio_codecs" doc:"The audio codecs this container carries here, plus copy and mute."`
}

// SubmitVideoConvert is the body of POST /api/v1/tools/video-convert.
type SubmitVideoConvert struct {
	FileID         string `json:"file_id"         validate:"required,trim" doc:"The id of an upload of yours, from the resumable upload endpoint."`
	IdempotencyKey string `json:"idempotency_key" validate:"trim,max=128"  doc:"Repeat a submission safely: the same key returns the same conversion."`
	// Options are the tool's own options. Only the container is required.
	Options tools.VideoConvertOptions `json:"options" doc:"How to convert. Only the container is required; the codecs default to the ones that container plays everywhere."`
}

// VideoConvert submits a video conversion:
// POST /api/v1/tools/video-convert.
func (h *Media) VideoConvert(c *web.Ctx, in SubmitVideoConvert) (ConversionView, error) {
	opts, err := in.Options.Normalise(h.writableVideoCodecs())
	if err != nil {
		return ConversionView{}, err
	}

	encoded, err := json.Marshal(opts)
	if err != nil {
		return ConversionView{}, err
	}

	return h.submit(c, submission{
		Tool:           "video-convert",
		FileID:         in.FileID,
		IdempotencyKey: in.IdempotencyKey,
		Options:        encoded,
	})
}

// SubmitVideoToGIF is the body of POST /api/v1/tools/video-to-gif.
type SubmitVideoToGIF struct {
	FileID         string `json:"file_id"         validate:"required,trim" doc:"The id of an upload of yours, from the resumable upload endpoint."`
	IdempotencyKey string `json:"idempotency_key" validate:"trim,max=128"  doc:"Repeat a submission safely: the same key returns the same conversion."`
	// Options are the tool's own options. Every field is optional.
	Options tools.VideoToGIFOptions `json:"options" doc:"Which part of the video, and how. Every field is optional."`
}

// VideoToGIF submits a video-to-GIF conversion:
// POST /api/v1/tools/video-to-gif.
func (h *Media) VideoToGIF(c *web.Ctx, in SubmitVideoToGIF) (ConversionView, error) {
	opts, err := in.Options.Normalise()
	if err != nil {
		return ConversionView{}, err
	}

	encoded, err := json.Marshal(opts)
	if err != nil {
		return ConversionView{}, err
	}

	return h.submit(c, submission{
		Tool:           "video-to-gif",
		FileID:         in.FileID,
		IdempotencyKey: in.IdempotencyKey,
		Options:        encoded,
	})
}

// VideoCapabilities answers GET /api/v1/tools/video-convert/capabilities.
func (h *Media) VideoCapabilities(_ *web.Ctx, _ struct{}) (VideoCapabilities, error) {
	available := h.writableVideoCodecs()

	out := VideoCapabilities{
		Containers:    make([]VideoContainer, 0, len(video.Containers())),
		Codecs:        make([]string, 0, len(available)),
		Dithers:       make([]string, 0, len(video.Dithers())),
		MaxGIFSeconds: video.MaxGIFDuration.Seconds(),
		MaxSeconds:    video.MaxDuration.Seconds(),
	}

	if h.media != nil {
		out.GPLBuild = h.media.FFmpeg.GPL
	}

	for _, codec := range available {
		out.Codecs = append(out.Codecs, string(codec))
	}

	out.Dithers = append(out.Dithers, video.DitherNames()...)

	for _, container := range video.Containers() {
		entry := VideoContainer{
			Name:      string(container),
			Label:     container.Label(),
			Extension: container.Extension(),
		}

		// Only the codecs this build has: a container's table says what it
		// can carry, and the build says what can be made, and a client
		// should see the intersection rather than work it out.
		for _, codec := range video.CodecsFor(container) {
			if containsCodec(available, codec) {
				entry.VideoCodecs = append(entry.VideoCodecs, string(codec))
			}
		}

		for _, codec := range video.AudioCodecsFor(container) {
			entry.AudioCodecs = append(entry.AudioCodecs, string(codec))
		}

		out.Containers = append(out.Containers, entry)
	}

	return out, nil
}

// containsCodec reports whether the codec is in the list.
func containsCodec(codecs []video.Codec, want video.Codec) bool {
	return slices.Contains(codecs, want)
}
