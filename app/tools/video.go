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
	"github.com/AlexGalhardo/vukem-tools/internal/ffmpeg"
	"github.com/AlexGalhardo/vukem-tools/internal/files"
	"github.com/AlexGalhardo/vukem-tools/internal/jobs"
	"github.com/AlexGalhardo/vukem-tools/internal/video"
)

// VideoTools is the video domain's tools.
func VideoTools(f *files.Store, m *Media) []jobs.Tool {
	if !m.HasFFmpeg() {
		return nil
	}

	return []jobs.Tool{NewVideoConvert(f, m), NewVideoToGIF(f, m)}
}

// videoTypes is what the video tools read. It is a constant list rather than
// a probe because ffmpeg decodes essentially everything, and the real bound
// is what internal/detect recognises and the upload endpoint will store.
func videoTypes() []string {
	return []string{
		"video/mp4",
		"video/webm",
		"video/x-matroska",
		"video/quicktime",
		"video/x-msvideo",
	}
}

// VideoConvert transcodes a video between containers and codecs.
type VideoConvert struct {
	files *files.Store
	media *Media
}

// NewVideoConvert returns the tool.
func NewVideoConvert(f *files.Store, m *Media) *VideoConvert {
	return &VideoConvert{files: f, media: m}
}

// Name implements jobs.Tool.
func (*VideoConvert) Name() string { return "video-convert" }

// Ability implements jobs.Tool.
func (*VideoConvert) Ability() string { return abilities.VideoConvert }

// Timeout implements jobs.Tool.
//
// Thirty minutes, which is far longer than anything else here and is the
// honest bound: the benchmark in the production image encodes a six-second
// 720p clip to VP9 in about six seconds of wall clock, so a two-hour input —
// the duration limit internal/video enforces — is minutes of work at best and
// much more on a busy worker. A tighter timeout would fail legitimate
// conversions, and the duration limit is what actually bounds the cost.
func (*VideoConvert) Timeout() time.Duration { return 30 * time.Minute }

// Accepted implements jobs.Accepter.
func (*VideoConvert) Accepted() []string { return videoTypes() }

// VideoConvertOptions is the validated input of a video-convert conversion.
type VideoConvertOptions struct {
	// Container is the output container.
	Container string `json:"container" validate:"trim,max=8" doc:"mp4, webm, mkv or mov."`
	// Video is the video codec. Empty takes the container's default.
	Video string `json:"video" validate:"trim,max=8" doc:"h264, h265, vp9 or av1. Empty uses the container's default. GET /api/v1/tools/video-convert/capabilities lists what this server has."`
	// Audio is the audio codec, or mute, or copy.
	Audio string `json:"audio" validate:"trim,max=8" doc:"aac, opus, mp3, flac, vorbis, copy or mute. Empty uses the container's default."`
	// CRF is the quality on the chosen codec's own scale.
	CRF int `json:"crf" validate:"min=0,max=63" doc:"Quality on the codec's own scale; the scales are not comparable between codecs. 0 uses the codec's default."`
	// Height scales the video, keeping the aspect ratio.
	Height int `json:"height" validate:"min=0,max=4320" doc:"Scale to this height, keeping the shape. Only downwards. 0 leaves the resolution alone."`
	// FrameRate caps the frame rate.
	FrameRate float64 `json:"frame_rate" validate:"min=0,max=240" doc:"Cap the frame rate. 0 leaves it alone."`
	// AudioBitrate is the audio bit rate in kilobits per second.
	AudioBitrate int `json:"audio_bitrate" validate:"min=0,max=512" doc:"Audio bit rate in kbit/s. 0 uses the codec's default."`
}

// Normalise fills in every omitted field and refuses anything that is not
// documented.
//
// Validated here, at submission, rather than where the ffmpeg command line is
// built: a bad request fails at once with a 422 naming the field instead of
// becoming a job that fails on a worker a minute later. And every one of
// these values ends up inside ffmpeg's own option syntax, which ffmpeg parses
// itself, so an enumerable allowlist is the only constraint that holds.
//
// available is what this build can encode, so a codec the binary does not
// have is refused with the list that it does — the GPL case, where an LGPL
// build has no H.264 at all.
func (in VideoConvertOptions) Normalise(available []video.Codec) (VideoConvertOptions, error) {
	in.Container = strings.ToLower(strings.TrimSpace(in.Container))
	if in.Container == "" {
		return VideoConvertOptions{}, z.FieldError("options.container",
			"Choose an output container: "+strings.Join(containerNames(), ", ")+".")
	}

	container := video.Container(in.Container)
	if !container.Known() {
		return VideoConvertOptions{}, z.FieldError("options.container",
			"Unknown container. Use one of: "+strings.Join(containerNames(), ", ")+".")
	}

	in.Video = strings.ToLower(strings.TrimSpace(in.Video))
	if in.Video != "" {
		codec := video.Codec(in.Video)

		if !slices.Contains(video.CodecsFor(container), codec) {
			return VideoConvertOptions{}, z.FieldError("options.video",
				container.Label()+" cannot carry "+in.Video+". It takes: "+
					strings.Join(codecNames(video.CodecsFor(container)), ", ")+".")
		}

		if !slices.Contains(available, codec) {
			// Not a client mistake but still the client's problem, and the
			// answer is the list this build does have.
			return VideoConvertOptions{}, z.FieldError("options.video",
				codec.Label()+" is not available on this server. It can encode: "+
					strings.Join(codecNames(available), ", ")+".")
		}
	}

	in.Audio = strings.ToLower(strings.TrimSpace(in.Audio))
	if in.Audio != "" && !slices.Contains(video.AudioCodecsFor(container), video.AudioCodec(in.Audio)) {
		return VideoConvertOptions{}, z.FieldError("options.audio",
			container.Label()+" cannot carry "+in.Audio+". It takes: "+
				strings.Join(audioCodecNames(video.AudioCodecsFor(container)), ", ")+".")
	}

	if in.CRF < 0 || in.CRF > maxCRF {
		return VideoConvertOptions{}, z.FieldError("options.crf",
			fmt.Sprintf("The quality must be between 0 and %d.", maxCRF))
	}

	if in.Height < 0 || in.Height > maxVideoHeight {
		return VideoConvertOptions{}, z.FieldError("options.height",
			fmt.Sprintf("The height must be between 0 and %d.", maxVideoHeight))
	}

	if in.FrameRate < 0 || in.FrameRate > maxFrameRate {
		return VideoConvertOptions{}, z.FieldError("options.frame_rate",
			fmt.Sprintf("The frame rate must be between 0 and %g.", maxFrameRate))
	}

	if in.AudioBitrate < 0 || in.AudioBitrate > maxAudioBitrate {
		return VideoConvertOptions{}, z.FieldError("options.audio_bitrate",
			fmt.Sprintf("The audio bit rate must be between 0 and %d kbit/s.", maxAudioBitrate))
	}

	return in, nil
}

// The bounds the API documents and enforces. They are wider than any single
// codec's own range, which internal/video clamps to: this layer refuses
// nonsense, and the codec tables decide what is sensible.
const (
	maxCRF          = 63
	maxVideoHeight  = 4320
	maxFrameRate    = 240.0
	maxAudioBitrate = 512
)

// containerNames, codecNames and audioCodecNames render the tables for a
// message.
func containerNames() []string {
	out := make([]string, 0, len(video.Containers()))
	for _, c := range video.Containers() {
		out = append(out, string(c))
	}

	return out
}

func codecNames(codecs []video.Codec) []string {
	out := make([]string, 0, len(codecs))
	for _, c := range codecs {
		out = append(out, string(c))
	}

	if len(out) == 0 {
		return []string{"nothing, because no video engine is available"}
	}

	return out
}

func audioCodecNames(codecs []video.AudioCodec) []string {
	out := make([]string, 0, len(codecs))
	for _, c := range codecs {
		out = append(out, string(c))
	}

	return out
}

// library turns the public options into the library's.
func (in VideoConvertOptions) library() video.Options {
	return video.Options{
		Container:    video.Container(in.Container),
		Video:        video.Codec(in.Video),
		Audio:        video.AudioCodec(in.Audio),
		CRF:          in.CRF,
		Height:       in.Height,
		FrameRate:    in.FrameRate,
		AudioBitrate: in.AudioBitrate,
	}
}

// ParseVideoConvertOptions validates and decodes the options stored on a
// conversion.
func ParseVideoConvertOptions(
	raw json.RawMessage,
	available []video.Codec,
) (VideoConvertOptions, error) {
	var in VideoConvertOptions

	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &in); err != nil {
			return VideoConvertOptions{}, z.FieldError("options", "The options are not a JSON object.")
		}
	}

	return in.Normalise(available)
}

// ValidateOptions implements jobs.OptionsValidator.
func (t *VideoConvert) ValidateOptions(raw json.RawMessage) (json.RawMessage, error) {
	opts, err := ParseVideoConvertOptions(raw, t.media.WritableVideoCodecs())
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
func (t *VideoConvert) Run(ctx context.Context, w jobs.Work) (jobs.Outcome, error) {
	opts, err := ParseVideoConvertOptions(w.Options, t.media.WritableVideoCodecs())
	if err != nil {
		return jobs.Outcome{}, jobs.Fail(jobs.UnsupportedInput, err,
			"the stored options are not valid for this tool")
	}

	engines, ok := ffmpegEngine(t.media.Engines)
	if !ok {
		return jobs.Outcome{}, jobs.Failf(jobs.Internal, "the video engine is not available")
	}

	input, err := inputFile(ctx, t.files, w, "the video converter", t.Accepted())
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

	result, err := video.Convert(ctx, engines, t.media.Runner, t.media.FFmpeg, w.Dir, local,
		opts.library(), encodeProgress(w))
	if err != nil {
		return jobs.Outcome{}, classifyFFmpeg(err)
	}

	if err := w.Report(jobs.Step{Percent: 95, Phase: "storing"}); err != nil {
		return jobs.Outcome{}, err
	}

	out, err := storeResult(ctx, t.files, w, input, result.Path,
		containerMIME(video.Container(opts.Container)))
	if err != nil {
		return jobs.Outcome{}, err
	}

	out.CPUMs = result.CPUTime

	return out, nil
}

// containerMIME maps a container to the media type internal/detect reports
// for it, which is what the stored result has to be labelled with.
func containerMIME(c video.Container) string {
	switch c {
	case video.MP4:
		return "video/mp4"
	case video.WebM:
		return "video/webm"
	case video.MKV:
		return "video/x-matroska"
	case video.MOV:
		return "video/quicktime"
	default:
		return "video/mp4"
	}
}

// encodeProgress turns the engine's progress into the pipeline's.
//
// The percentages are compressed into 5 to 90 rather than passed through,
// because the encode is not the whole job: there is a probe before it and an
// upload after, and a bar that sits at 100 while a 200 MB result is still
// being written to the bucket is a bar that lies.
//
// An unknown total — a container with no duration — arrives as -1, and the
// step then carries the phase without a number rather than a made-up one.
func encodeProgress(w jobs.Work) func(ffmpeg.Progress) error {
	return func(p ffmpeg.Progress) error {
		step := jobs.Step{Phase: "converting"}

		if p.Percent >= 0 {
			step.Percent = encodeFloor + p.Percent*(encodeCeiling-encodeFloor)/100
		}

		return w.Report(step)
	}
}

// The band of the overall progress the encode occupies.
const (
	encodeFloor   = 5
	encodeCeiling = 90
)

// VideoToGIF turns a range of a video into an animated GIF.
type VideoToGIF struct {
	files *files.Store
	media *Media
}

// NewVideoToGIF returns the tool.
func NewVideoToGIF(f *files.Store, m *Media) *VideoToGIF { return &VideoToGIF{files: f, media: m} }

// Name implements jobs.Tool.
func (*VideoToGIF) Name() string { return "video-to-gif" }

// Ability implements jobs.Tool. It shares the video conversion ability: a key
// that may transcode a video may also make a GIF of one, and a separate
// ability for a narrower version of the same work would be noise.
func (*VideoToGIF) Ability() string { return abilities.VideoConvert }

// Timeout implements jobs.Tool. Shorter than a transcode because the input
// range is capped at fifteen seconds, and the benchmark puts a five-second
// 480p clip at under a second of engine time.
func (*VideoToGIF) Timeout() time.Duration { return 5 * time.Minute }

// Accepted implements jobs.Accepter.
func (*VideoToGIF) Accepted() []string { return videoTypes() }

// VideoToGIFOptions is the validated input of a video-to-gif conversion.
type VideoToGIFOptions struct {
	// FPS is the GIF's frame rate.
	FPS int `json:"fps" validate:"min=0,max=50" doc:"Frames per second. 0 uses 12, which divides 100 evenly and so plays evenly."`
	// Width scales the GIF, keeping the aspect ratio.
	Width int `json:"width" validate:"min=0,max=1920" doc:"Width in pixels. 0 uses 480: a GIF has no interframe compression, so doubling the width quadruples the file."`
	// StartSeconds and DurationSeconds select a range.
	StartSeconds    float64 `json:"start_seconds"    validate:"min=0" doc:"Where to start, in seconds."`
	DurationSeconds float64 `json:"duration_seconds" validate:"min=0" doc:"How long, in seconds. 0 takes the rest of the video, up to the 15 second limit."`
	// Dither is the dithering mode.
	Dither string `json:"dither" validate:"trim,max=20" doc:"Dithering mode. Empty uses sierra2_4a."`
}

// Normalise fills in every omitted field and refuses anything undocumented.
//
// The dithering mode is the one that matters here, and it is why this is
// validated at submission. The value goes into an ffmpeg filtergraph, which
// ffmpeg parses itself: a mode of "none,movie=/app/work/input.mp4" is a
// filter chain, not a dithering mode, and argv safety does nothing about a
// sub-language the engine interprets. An unknown mode is refused with the
// accepted list rather than replaced by the default, because somebody who
// asked for sierra3 and silently got bayer only finds out when they look
// closely at a gradient.
func (in VideoToGIFOptions) Normalise() (VideoToGIFOptions, error) {
	if in.FPS < 0 || in.FPS > maxGIFFPS {
		return VideoToGIFOptions{}, z.FieldError("options.fps",
			fmt.Sprintf("The frame rate must be between 0 and %d.", maxGIFFPS))
	}

	if in.Width < 0 || in.Width > maxGIFWidth {
		return VideoToGIFOptions{}, z.FieldError("options.width",
			fmt.Sprintf("The width must be between 0 and %d pixels.", maxGIFWidth))
	}

	if in.StartSeconds < 0 || in.StartSeconds > maxGIFStartSeconds {
		return VideoToGIFOptions{}, z.FieldError("options.start_seconds",
			"The start must be between 0 and the video's own length.")
	}

	limit := video.MaxGIFDuration.Seconds()
	if in.DurationSeconds < 0 || in.DurationSeconds > limit {
		return VideoToGIFOptions{}, z.FieldError("options.duration_seconds",
			fmt.Sprintf("A GIF can be at most %g seconds long.", limit))
	}

	in.Dither = strings.ToLower(strings.TrimSpace(in.Dither))
	if in.Dither == "" {
		return in, nil
	}

	if !video.Dither(in.Dither).Valid() {
		return VideoToGIFOptions{}, z.FieldError("options.dither",
			"Unknown dithering mode. Use one of: "+strings.Join(video.DitherNames(), ", ")+".")
	}

	return in, nil
}

// The bounds the API documents and enforces.
const (
	maxGIFFPS   = 50
	maxGIFWidth = 1920
	// maxGIFStartSeconds is the duration limit internal/video enforces on a
	// video, so a start past it could never name a frame.
	maxGIFStartSeconds = 7200
)

// library turns the public options into the library's.
func (in VideoToGIFOptions) library() video.GIFOptions {
	return video.GIFOptions{
		FPS:      in.FPS,
		Width:    in.Width,
		Start:    time.Duration(in.StartSeconds * float64(time.Second)),
		Duration: time.Duration(in.DurationSeconds * float64(time.Second)),
		Dither:   video.Dither(in.Dither),
	}
}

// ParseVideoToGIFOptions validates and decodes the options stored on a
// conversion.
func ParseVideoToGIFOptions(raw json.RawMessage) (VideoToGIFOptions, error) {
	var in VideoToGIFOptions

	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &in); err != nil {
			return VideoToGIFOptions{}, z.FieldError("options", "The options are not a JSON object.")
		}
	}

	return in.Normalise()
}

// ValidateOptions implements jobs.OptionsValidator.
func (*VideoToGIF) ValidateOptions(raw json.RawMessage) (json.RawMessage, error) {
	opts, err := ParseVideoToGIFOptions(raw)
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
func (t *VideoToGIF) Run(ctx context.Context, w jobs.Work) (jobs.Outcome, error) {
	opts, err := ParseVideoToGIFOptions(w.Options)
	if err != nil {
		return jobs.Outcome{}, jobs.Fail(jobs.UnsupportedInput, err,
			"the stored options are not valid for this tool")
	}

	engines, ok := ffmpegEngine(t.media.Engines)
	if !ok {
		return jobs.Outcome{}, jobs.Failf(jobs.Internal, "the video engine is not available")
	}

	input, err := inputFile(ctx, t.files, w, "the GIF maker", t.Accepted())
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

	result, err := video.ToGIF(ctx, engines, t.media.Runner, w.Dir, local,
		opts.library(), encodeProgress(w))
	if err != nil {
		return jobs.Outcome{}, classifyFFmpeg(err)
	}

	if err := w.Report(jobs.Step{Percent: 95, Phase: "storing"}); err != nil {
		return jobs.Outcome{}, err
	}

	out, err := storeResult(ctx, t.files, w, input, result.Path, "image/gif")
	if err != nil {
		return jobs.Outcome{}, err
	}

	out.CPUMs = result.CPUTime

	return out, nil
}
