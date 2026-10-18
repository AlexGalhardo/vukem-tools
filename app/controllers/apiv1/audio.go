package apiv1

import (
	"encoding/json"
	"slices"

	"github.com/AlexGalhardo/vukem/web"

	"github.com/AlexGalhardo/vukem-tools/app/tools"
	"github.com/AlexGalhardo/vukem-tools/internal/audio"
)

// AudioCapabilities is what this server can produce for audio.
type AudioCapabilities struct {
	// Formats is every audio format, with whether this build can write it.
	Formats []AudioFormat `json:"formats" doc:"Every output format, with whether this server can write it."`
	// Writable is the subset that can be produced.
	Writable []string `json:"writable" doc:"The formats this server can write, by name."`
	// Readable is the media types that may be submitted, which includes the
	// video types: extracting a soundtrack is the same operation.
	Readable []string `json:"readable" doc:"The media types this server accepts, including video, since extracting a soundtrack is the same conversion."`
	// SampleRates is the accepted sample rates.
	SampleRates []int `json:"sample_rates" doc:"The sample rates a conversion may ask for. 0 keeps the source's."`
	// LoudnessTarget is the figure a normalisation aims at.
	LoudnessTarget float64 `json:"loudness_target" doc:"The integrated loudness normalisation aims at, in LUFS."`
	// MaxSeconds is the longest input a conversion accepts.
	MaxSeconds float64 `json:"max_seconds" doc:"The longest recording this server converts, in seconds."`
}

// AudioFormat is one format's support in this build.
type AudioFormat struct {
	Name      string `json:"name"      doc:"The identifier to send as options.to."`
	Label     string `json:"label"     doc:"What to call it in an interface."`
	MIME      string `json:"mime"      doc:"The media type a file of this format has."`
	Extension string `json:"extension" doc:"The canonical extension, with the dot."`
	Write     bool   `json:"write"     doc:"Whether this server can produce it."`
	// Lossy says whether a bit rate means anything. A client should hide the
	// bit-rate control for a lossless format rather than send one and be
	// refused.
	Lossy bool `json:"lossy" doc:"Whether a bit rate applies. FLAC and WAV are lossless and refuse one."`
}

// SubmitAudioConvert is the body of POST /api/v1/tools/audio-convert.
type SubmitAudioConvert struct {
	FileID         string `json:"file_id"         validate:"required,trim" doc:"The id of an upload of yours, from the resumable upload endpoint."`
	IdempotencyKey string `json:"idempotency_key" validate:"trim,max=128"  doc:"Repeat a submission safely: the same key returns the same conversion."`
	// Options are the tool's own options. Only the output format is
	// required. A video input has its soundtrack extracted.
	Options tools.AudioConvertOptions `json:"options" doc:"How to convert. Only the output format is required. A video input has its soundtrack extracted."`
}

// AudioConvert submits an audio conversion:
// POST /api/v1/tools/audio-convert.
func (h *Media) AudioConvert(c *web.Ctx, in SubmitAudioConvert) (ConversionView, error) {
	opts, err := in.Options.Normalise(h.writableAudioFormats())
	if err != nil {
		return ConversionView{}, err
	}

	encoded, err := json.Marshal(opts)
	if err != nil {
		return ConversionView{}, err
	}

	return h.submit(c, submission{
		Tool:           "audio-convert",
		FileID:         in.FileID,
		IdempotencyKey: in.IdempotencyKey,
		Options:        encoded,
	})
}

// AudioCapabilities answers GET /api/v1/tools/audio-convert/formats.
func (h *Media) AudioCapabilities(_ *web.Ctx, _ struct{}) (AudioCapabilities, error) {
	writable := h.writableAudioFormats()

	out := AudioCapabilities{
		Formats:        make([]AudioFormat, 0, len(audio.Formats())),
		SampleRates:    tools.AudioSampleRates(),
		LoudnessTarget: audio.LoudnessTarget,
		MaxSeconds:     audio.MaxDuration.Seconds(),
		Readable:       audio.MIMETypes(),
	}

	for _, format := range audio.Formats() {
		can := containsFormat(writable, format)

		out.Formats = append(out.Formats, AudioFormat{
			Name:      string(format),
			Label:     format.Label(),
			MIME:      format.MIME(),
			Extension: format.Extension(),
			Write:     can,
			Lossy:     format.Lossy(),
		})

		if can {
			out.Writable = append(out.Writable, string(format))
		}
	}

	return out, nil
}

// containsFormat reports whether the format is in the list.
func containsFormat(formats []audio.Format, want audio.Format) bool {
	return slices.Contains(formats, want)
}
