package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/AlexGalhardo/vukem/z"

	"github.com/AlexGalhardo/vukem-tools/app/abilities"
	"github.com/AlexGalhardo/vukem-tools/internal/audio"
	"github.com/AlexGalhardo/vukem-tools/internal/files"
	"github.com/AlexGalhardo/vukem-tools/internal/jobs"
)

// AudioTools is the audio domain's tools.
func AudioTools(f *files.Store, m *Media) []jobs.Tool {
	if !m.HasFFmpeg() {
		return nil
	}

	return []jobs.Tool{NewAudioConvert(f, m)}
}

// AudioConvert converts audio between formats, and extracts the soundtrack of
// a video.
//
// One tool for both, because they are one ffmpeg invocation: extracting a
// soundtrack is a conversion whose input happens to carry a picture, and -vn
// is the only difference. A separate "extract audio" tool would be a second
// place for the channel, sample-rate and loudness handling to drift.
type AudioConvert struct {
	files *files.Store
	media *Media
}

// NewAudioConvert returns the tool.
func NewAudioConvert(f *files.Store, m *Media) *AudioConvert {
	return &AudioConvert{files: f, media: m}
}

// Name implements jobs.Tool.
func (*AudioConvert) Name() string { return "audio-convert" }

// Ability implements jobs.Tool.
func (*AudioConvert) Ability() string { return abilities.AudioConvert }

// Timeout implements jobs.Tool.
//
// Fifteen minutes. The duration limit internal/audio enforces is four hours,
// and the benchmark encodes thirty seconds to MP3 in half a second of engine
// time, so four hours is a few minutes of work; a two-pass loudness
// normalisation decodes it twice. This bound is what stops a stuck worker,
// and the duration limit is what bounds the cost.
func (*AudioConvert) Timeout() time.Duration { return 15 * time.Minute }

// Accepted implements jobs.Accepter: every audio type the service knows plus
// every video type, because extracting a soundtrack is the same operation.
func (*AudioConvert) Accepted() []string { return audio.MIMETypes() }

// AudioConvertOptions is the validated input of an audio-convert conversion.
type AudioConvertOptions struct {
	// To is the output format.
	To string `json:"to" validate:"trim,max=8" doc:"mp3, wav, flac, opus or aac. GET /api/v1/tools/audio-convert/formats lists what this server can write."`
	// Bitrate is the bit rate in kilobits per second, for a lossy format.
	Bitrate int `json:"bitrate" validate:"min=0,max=320" doc:"Bit rate in kbit/s for MP3, Opus and AAC. 0 uses the format's default. Ignored by FLAC and WAV, which are lossless."`
	// SampleRate resamples the audio.
	SampleRate int `json:"sample_rate" validate:"min=0,max=192000" doc:"Resample, in hertz. 0 keeps the source's rate."`
	// Channels forces the channel count.
	Channels int `json:"channels" validate:"min=0,max=2" doc:"1 for mono, 2 for stereo. 0 keeps the source's."`
	// NormaliseLoudness applies EBU R128 loudness normalisation.
	//
	// The Go field is not called Normalise because the options type's own
	// validator is, and a field and a method cannot share a name. The JSON
	// name, which is the public contract, is unaffected.
	NormaliseLoudness bool `json:"normalise" doc:"Normalise the loudness to -16 LUFS, the figure streaming platforms use."`
	// TwoPass measures the whole file first and then applies the exact
	// correction.
	TwoPass bool `json:"two_pass" doc:"Measure the whole recording before correcting it. Costs a second decode and is the only way to hit the target reliably."`
}

// The sample rates a client may name.
//
// An allowlist rather than a range, because an arbitrary rate is almost
// always a mistake: every encoder here supports a fixed set, and asking for
// 44101 Hz gets a resampler doing real work to produce a file no player
// benefits from. These are the rates the codecs in this build actually take.
var audioSampleRates = []int{8000, 11025, 16000, 22050, 24000, 32000, 44100, 48000, 88200, 96000}

// AudioSampleRates returns the accepted sample rates, for the documentation
// and the interface.
func AudioSampleRates() []int { return slices.Clone(audioSampleRates) }

// Normalise fills in every omitted field and refuses anything undocumented.
//
// Checked at submission rather than where the ffmpeg command line is built,
// so a bad request fails at once with a 422 naming the field instead of
// becoming a job that fails on a worker. available is what this build can
// encode, so a format the binary lacks is refused with the list it has.
func (in AudioConvertOptions) Normalise(available []audio.Format) (AudioConvertOptions, error) {
	in.To = strings.ToLower(strings.TrimSpace(in.To))
	if in.To == "" {
		return AudioConvertOptions{}, z.FieldError("options.to",
			"Choose an output format. This server can write: "+joinAudioFormats(available)+".")
	}

	format := audio.Format(in.To)

	if !format.Known() {
		return AudioConvertOptions{}, z.FieldError("options.to",
			"Unknown format. Use one of: "+joinAudioFormats(audio.Formats())+".")
	}

	if !slices.Contains(available, format) {
		return AudioConvertOptions{}, z.FieldError("options.to",
			"This server cannot write "+in.To+". It can write: "+joinAudioFormats(available)+".")
	}

	if in.Bitrate < 0 || in.Bitrate > maxBitrate {
		return AudioConvertOptions{}, z.FieldError("options.bitrate",
			fmt.Sprintf("The bit rate must be between 0 and %d kbit/s.", maxBitrate))
	}

	// A bit rate on a lossless format is a category error. It is refused
	// rather than ignored, because a client that sets 128k and asks for FLAC
	// has misunderstood something, and silently producing a 900 kbit/s file
	// would leave them thinking the option worked.
	if in.Bitrate > 0 && !format.Lossy() {
		return AudioConvertOptions{}, z.FieldError("options.bitrate",
			format.Label()+" is lossless, so it has no bit rate to set. Leave it at 0, or "+
				"choose MP3, Opus or AAC.")
	}

	if in.SampleRate != 0 && !slices.Contains(audioSampleRates, in.SampleRate) {
		return AudioConvertOptions{}, z.FieldError("options.sample_rate",
			"Unknown sample rate. Use one of: "+joinInts(audioSampleRates)+", or 0 to keep the source's.")
	}

	if in.Channels < 0 || in.Channels > 2 {
		return AudioConvertOptions{}, z.FieldError("options.channels",
			"The channel count must be 1 for mono, 2 for stereo, or 0 to keep the source's.")
	}

	// Two passes without normalisation is nothing: there is no second pass
	// to be exact about. Refusing it is clearer than quietly ignoring it.
	if in.TwoPass && !in.NormaliseLoudness {
		return AudioConvertOptions{}, z.FieldError("options.two_pass",
			"Two passes only apply to loudness normalisation. Set normalise as well, or leave this off.")
	}

	return in, nil
}

// maxBitrate is the widest bound the API documents; each format's own range
// is narrower and internal/audio clamps to it.
const maxBitrate = 320

// joinAudioFormats renders a format list for a message.
func joinAudioFormats(formats []audio.Format) string {
	out := make([]string, 0, len(formats))
	for _, f := range formats {
		out = append(out, string(f))
	}

	if len(out) == 0 {
		return "nothing, because no audio engine is available"
	}

	return strings.Join(out, ", ")
}

// joinInts renders an integer list for a message.
func joinInts(values []int) string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		out = append(out, strconv.Itoa(v))
	}

	return strings.Join(out, ", ")
}

// library turns the public options into the library's.
func (in AudioConvertOptions) library() audio.Options {
	return audio.Options{
		To:               audio.Format(in.To),
		Bitrate:          in.Bitrate,
		SampleRate:       in.SampleRate,
		Channels:         in.Channels,
		Normalise:        in.NormaliseLoudness,
		TwoPassNormalise: in.TwoPass,
	}
}

// ParseAudioConvertOptions validates and decodes the options stored on a
// conversion.
func ParseAudioConvertOptions(
	raw json.RawMessage,
	available []audio.Format,
) (AudioConvertOptions, error) {
	var in AudioConvertOptions

	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &in); err != nil {
			return AudioConvertOptions{}, z.FieldError("options", "The options are not a JSON object.")
		}
	}

	return in.Normalise(available)
}

// ValidateOptions implements jobs.OptionsValidator.
func (t *AudioConvert) ValidateOptions(raw json.RawMessage) (json.RawMessage, error) {
	opts, err := ParseAudioConvertOptions(raw, t.media.WritableAudioFormats())
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
func (t *AudioConvert) Run(ctx context.Context, w jobs.Work) (jobs.Outcome, error) {
	opts, err := ParseAudioConvertOptions(w.Options, t.media.WritableAudioFormats())
	if err != nil {
		return jobs.Outcome{}, jobs.Fail(jobs.UnsupportedInput, err,
			"the stored options are not valid for this tool")
	}

	engines, ok := ffmpegEngine(t.media.Engines)
	if !ok {
		return jobs.Outcome{}, jobs.Failf(jobs.Internal, "the audio engine is not available")
	}

	input, err := inputFile(ctx, t.files, w, "the audio converter", t.Accepted())
	if err != nil {
		return jobs.Outcome{}, err
	}

	if err := w.Report(jobs.Step{Percent: 2, Phase: "reading"}); err != nil {
		return jobs.Outcome{}, err
	}

	local, err := localInput(ctx, t.files, w, input)
	if err != nil {
		return jobs.Outcome{}, err
	}

	result, err := audio.Convert(ctx, engines, t.media.Runner, t.media.FFmpeg, w.Dir, local,
		opts.library(), encodeProgress(w))
	if err != nil {
		return jobs.Outcome{}, classifyFFmpeg(err)
	}

	if err := w.Report(jobs.Step{Percent: 95, Phase: "storing"}); err != nil {
		return jobs.Outcome{}, err
	}

	out, err := storeResult(ctx, t.files, w, input, result.Path, result.Format.MIME())
	if err != nil {
		return jobs.Outcome{}, err
	}

	out.CPUMs = result.CPUTime

	return out, nil
}
