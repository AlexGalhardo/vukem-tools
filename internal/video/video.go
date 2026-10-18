// Package video converts video through ffmpeg.
//
// The codec tables here are the point of the package. ffmpeg will let you ask
// for VP9 in a MOV container or H.264 in WebM, produce a file, and leave you
// with something no browser plays; and it will let you ask for x264 on an
// LGPL build and fail with "Unknown encoder". Both are decided here, before a
// process starts, so the refusal names the real problem and suggests the
// alternative.
//
// Everything runs through internal/ffmpeg, which runs through internal/engine:
// a timeout, an address-space cap, a file-size cap, a minimal environment, no
// network, and a process-group kill. The last matters most for ffmpeg, which
// spawns children and ignores a plain terminate in several configurations.
package video

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/AlexGalhardo/vukem-tools/internal/cpu"
	"github.com/AlexGalhardo/vukem-tools/internal/ffmpeg"
)

// Container is an output container format.
type Container string

// The containers this package writes. The string is the public identifier.
const (
	MP4  Container = "mp4"
	WebM Container = "webm"
	MKV  Container = "mkv"
	MOV  Container = "mov"
)

// Codec is a video encoder, named as a person names it rather than as
// ffmpeg does.
type Codec string

// The video codecs.
const (
	H264 Codec = "h264"
	H265 Codec = "h265"
	VP9  Codec = "vp9"
	AV1  Codec = "av1"
)

// Dither is a paletteuse dithering mode.
//
// It is a typed enum rather than a string for a reason that is not obvious
// from the outside: `-lavfi` is its own language, and ffmpeg parses that
// string itself. internal/engine passes argv directly, so there is no shell
// to inject into, but argv safety says nothing about a sub-language the
// engine interprets on its own. A dither value of
// "none,movie=/app/work/something" is a filter chain, not a dither mode — the
// comma, the semicolon and the brackets are all syntax inside a filtergraph.
//
// The only constraint that holds for a sub-language is an allowlist that can
// be enumerated, which is what this is. The same rule applies to everything
// this package splices into -lavfi, -vf, -af, -filter_complex or a codec
// parameter string: either it is an integer, or it is one of these.
type Dither string

// The dithering modes, read out of `ffmpeg -h filter=paletteuse` in the
// production image and each one confirmed to encode there, rather than copied
// from documentation: getting the list wrong means refusing a mode that works.
const (
	DitherNone           Dither = "none"
	DitherBayer          Dither = "bayer"
	DitherHeckbert       Dither = "heckbert"
	DitherFloydSteinberg Dither = "floyd_steinberg"
	DitherSierra2        Dither = "sierra2"
	DitherSierra2Lite    Dither = "sierra2_4a"
	DitherSierra3        Dither = "sierra3"
	DitherBurkes         Dither = "burkes"
	DitherAtkinson       Dither = "atkinson"
)

// dithers is the closed set, in the order the engine lists it.
var dithers = []Dither{
	DitherNone,
	DitherBayer,
	DitherHeckbert,
	DitherFloydSteinberg,
	DitherSierra2,
	DitherSierra2Lite,
	DitherSierra3,
	DitherBurkes,
	DitherAtkinson,
}

// Dithers returns every accepted mode, for the API documentation and the
// interface.
func Dithers() []Dither { return slices.Clone(dithers) }

// Valid reports whether d is one of the accepted modes. The empty value is
// not valid: "use the default" is the caller's choice to make explicitly,
// through Normalise, rather than something this type decides.
func (d Dither) Valid() bool { return slices.Contains(dithers, d) }

// DitherNames returns the accepted modes as strings, for a message.
func DitherNames() []string {
	out := make([]string, 0, len(dithers))
	for _, d := range dithers {
		out = append(out, string(d))
	}

	return out
}

// UnknownDitherError reports a dithering mode that is not in the allowlist.
//
// A refusal rather than a silent fall back to the default, and that is the
// important half. Quietly substituting a default means somebody who asked for
// sierra3 and got bayer has no way to find out, and the difference shows up
// as banding on a gradient they will not look at closely until much later.
type UnknownDitherError struct {
	// Asked is what the caller sent. It is echoed back in the public
	// message, which is safe: it is the caller's own input, and it is the
	// only thing that makes the message useful.
	Asked string
}

// Error implements error.
func (e *UnknownDitherError) Error() string {
	return "video: " + e.Asked + " is not a dithering mode"
}

// Message is the sentence to show the user: what was wrong and what is
// accepted.
func (e *UnknownDitherError) Message() string {
	return "There is no dithering mode called " + quote(e.Asked) + ". Use one of: " +
		strings.Join(DitherNames(), ", ") + "."
}

// quote renders a value for a message without letting it run into the
// surrounding sentence.
func quote(s string) string {
	if s == "" {
		return `""`
	}

	return `"` + s + `"`
}

// AudioCodec is an audio encoder.
type AudioCodec string

// The audio codecs a video may carry.
const (
	AAC    AudioCodec = "aac"
	Opus   AudioCodec = "opus"
	MP3    AudioCodec = "mp3"
	FLAC   AudioCodec = "flac"
	Vorbis AudioCodec = "vorbis"
	// Copy keeps the input's audio stream untouched, which is both the
	// fastest and the only lossless option.
	Copy AudioCodec = "copy"
	// Mute drops the audio entirely.
	Mute AudioCodec = "mute"
)

// videoSpec describes one video codec: the ffmpeg encoder, whether it needs a
// GPL build, and the quality ladder.
type videoSpec struct {
	codec Codec
	// encoder is the ffmpeg encoder name.
	encoder string
	// label is what a message calls it.
	label string
	// gplOnly marks an encoder that exists only in a build configured with
	// --enable-gpl. x264 and x265 are both GPL, which is why this project
	// ships Debian's GPL build and why an LGPL build has to fail clearly
	// rather than cryptically.
	gplOnly bool
	// crf is the quality ladder: the default and the range a client may ask
	// for. The scales are not comparable between codecs — CRF 23 on x264 and
	// CRF 23 on VP9 are different pictures — which is why each codec carries
	// its own.
	crf, minCRF, maxCRF int
	// args are the codec's own flags beyond the CRF, as ffmpeg spells them.
	args []string
	// needsLogicalProcessors marks an encoder that builds its own thread
	// pool and ignores ffmpeg's -threads, so the pool has to be bounded
	// through the encoder's own parameter string.
	needsLogicalProcessors bool
}

// videoSpecs is the video codec table.
//
// The CRF defaults come from docs/web-research.md § 4.1, picked at the
// "visually indistinguishable at a sensible size" end of each published
// range rather than the smallest: somebody converting a video expects it to
// still look like the video.
var videoSpecs = []videoSpec{
	{
		codec: H264, encoder: "libx264", label: "H.264", gplOnly: true,
		crf: 23, minCRF: 14, maxCRF: 35,
		// medium is the preset where x264's speed and size curves cross;
		// yuv420p because anything else is not playable in a browser, and
		// ffmpeg happily produces yuv444p from an RGB source.
		args: []string{"-preset", "medium", "-pix_fmt", "yuv420p"},
	},
	{
		codec: H265, encoder: "libx265", label: "H.265", gplOnly: true,
		crf: 28, minCRF: 18, maxCRF: 40,
		// tag:v hvc1 rather than the default hev1: Safari and QuickTime play
		// hvc1 and ignore hev1, which is the single most common "the file
		// does not work on my Mac" report for HEVC in MP4.
		args: []string{"-preset", "medium", "-pix_fmt", "yuv420p", "-tag:v", "hvc1"},
	},
	{
		codec: VP9, encoder: "libvpx-vp9", label: "VP9",
		crf: 32, minCRF: 20, maxCRF: 45,
		// -b:v 0 is not optional: without it libvpx treats the CRF as an
		// upper bound on a bitrate-targeted encode and the quality setting
		// does almost nothing. row-mt uses more than one core per frame.
		args: []string{"-b:v", "0", "-row-mt", "1", "-pix_fmt", "yuv420p"},
	},
	{
		codec: AV1, encoder: "libsvtav1", label: "AV1",
		crf: 32, minCRF: 20, maxCRF: 50,
		// preset 6 is the published balance for SVT-AV1 on CPU; lower is
		// much slower for a few per cent, higher gives up most of AV1's
		// advantage.
		//
		// lp is not a tuning knob, it is what makes AV1 work at all here.
		// SVT-AV1 builds its own thread pool and ignores ffmpeg's -threads
		// entirely, sizing the pool from the host's core count — sixteen in
		// the container this was measured in, whatever the CPU quota says.
		// Under the engine's RLIMIT_NPROC that pool cannot be created, and
		// SVT fails with "Failed to create thread: Resource temporarily
		// unavailable" after printing a banner that mentions memory, so it
		// reads as an out-of-memory failure and is not one. lp bounds the
		// pool; it is filled in per run from the container's CPU quota.
		needsLogicalProcessors: true,
		args:                   []string{"-preset", "6", "-pix_fmt", "yuv420p"},
	},
}

// audioSpec describes one audio codec for a video's soundtrack.
type audioSpec struct {
	codec   AudioCodec
	encoder string
	label   string
	// bitrate is the default in kilobits per second, zero for a codec whose
	// rate is not set that way.
	bitrate int
}

// audioSpecs is the audio codec table for video soundtracks.
var audioSpecs = []audioSpec{
	{AAC, "aac", "AAC", 128},
	{Opus, "libopus", "Opus", 96},
	{MP3, "libmp3lame", "MP3", 192},
	{FLAC, "flac", "FLAC", 0},
	{Vorbis, "libvorbis", "Vorbis", 128},
}

// containerSpec describes one container: the codecs it may carry and the
// muxer flags it needs.
type containerSpec struct {
	container Container
	extension string
	label     string
	// video and audio are the codecs this package will put in it. They are
	// narrower than what ffmpeg will mux, deliberately: ffmpeg will write
	// VP9 into a MOV, and nothing plays it.
	video []Codec
	audio []AudioCodec
	// defaultVideo and defaultAudio are what the container gets when the
	// client names no codec, which is the common case.
	defaultVideo Codec
	defaultAudio AudioCodec
	// args are muxer flags.
	args []string
}

// containerSpecs is the container table.
var containerSpecs = []containerSpec{
	{
		container: MP4, extension: ".mp4", label: "MP4",
		video: []Codec{H264, H265, AV1},
		audio: []AudioCodec{AAC, MP3, Copy, Mute},
		// H.264 and AAC because an MP4 is what somebody asks for when they
		// want a file that plays everywhere, and that pair is the only one
		// that actually does.
		defaultVideo: H264, defaultAudio: AAC,
		// faststart moves the index to the front of the file, which is what
		// makes an MP4 start playing before it has finished downloading.
		args: []string{"-movflags", "+faststart"},
	},
	{
		container: WebM, extension: ".webm", label: "WebM",
		video:        []Codec{VP9, AV1},
		audio:        []AudioCodec{Opus, Vorbis, Copy, Mute},
		defaultVideo: VP9, defaultAudio: Opus,
	},
	{
		container: MKV, extension: ".mkv", label: "Matroska",
		// Matroska carries anything, and that is its reason for being here.
		video:        []Codec{H264, H265, VP9, AV1},
		audio:        []AudioCodec{AAC, Opus, MP3, FLAC, Vorbis, Copy, Mute},
		defaultVideo: H264, defaultAudio: AAC,
	},
	{
		container: MOV, extension: ".mov", label: "QuickTime",
		video:        []Codec{H264, H265},
		audio:        []AudioCodec{AAC, Copy, Mute},
		defaultVideo: H264, defaultAudio: AAC,
		args: []string{"-movflags", "+faststart"},
	},
}

// Lookups built once at load, so a codec added to a table is reachable
// without touching a second map.
var (
	videoByCodec = func() map[Codec]videoSpec {
		m := make(map[Codec]videoSpec, len(videoSpecs))
		for i := range videoSpecs {
			m[videoSpecs[i].codec] = videoSpecs[i]
		}

		return m
	}()

	audioByCodec = func() map[AudioCodec]audioSpec {
		m := make(map[AudioCodec]audioSpec, len(audioSpecs))
		for i := range audioSpecs {
			m[audioSpecs[i].codec] = audioSpecs[i]
		}

		return m
	}()

	containerByName = func() map[Container]containerSpec {
		m := make(map[Container]containerSpec, len(containerSpecs))
		for i := range containerSpecs {
			m[containerSpecs[i].container] = containerSpecs[i]
		}

		return m
	}()
)

// Extension returns the canonical extension of a container, including the
// dot.
func (c Container) Extension() string { return containerByName[c].extension }

// Label returns what a message should call the container.
func (c Container) Label() string {
	if s, ok := containerByName[c]; ok {
		return s.label
	}

	return string(c)
}

// Known reports whether c is a container this package writes.
func (c Container) Known() bool {
	_, ok := containerByName[c]
	return ok
}

// Label returns what a message should call the codec.
func (v Codec) Label() string {
	if s, ok := videoByCodec[v]; ok {
		return s.label
	}

	return string(v)
}

// Label returns what a message should call the audio codec, including the two
// values that are not codecs at all.
func (a AudioCodec) Label() string {
	switch a {
	case Copy:
		return "the original track"
	case Mute:
		return "no sound"
	default:
		if s, ok := audioByCodec[a]; ok {
			return s.label
		}

		return string(a)
	}
}

// Containers returns every container this package writes, in table order.
func Containers() []Container {
	out := make([]Container, 0, len(containerSpecs))
	for i := range containerSpecs {
		out = append(out, containerSpecs[i].container)
	}

	return out
}

// AvailableCodecs returns the video codecs this build can actually encode.
//
// This is where the GPL question surfaces for the interface: an ffmpeg built
// without --enable-gpl has no x264 and no x265, so H.264 and H.265 must not
// be offered there. Publishing this rather than the full table is what keeps
// the interface from advertising a codec the binary does not have.
func AvailableCodecs(caps ffmpeg.Capabilities) []Codec {
	var out []Codec

	for i := range videoSpecs {
		if caps.Has(videoSpecs[i].encoder) {
			out = append(out, videoSpecs[i].codec)
		}
	}

	return out
}

// CodecsFor returns the video codecs a container may carry.
func CodecsFor(c Container) []Codec {
	return slices.Clone(containerByName[c].video)
}

// AudioCodecsFor returns the audio codecs a container may carry.
func AudioCodecsFor(c Container) []AudioCodec {
	return slices.Clone(containerByName[c].audio)
}

// IncompatibleError reports a codec a container will not carry usefully.
type IncompatibleError struct {
	Container Container
	// Video and Audio: exactly one is set, naming the codec that does not
	// belong.
	Video Codec
	Audio AudioCodec
	// Allowed lists what the container does carry, for the message.
	Allowed []string
}

// Error implements error.
func (e *IncompatibleError) Error() string {
	return fmt.Sprintf("video: %s does not carry %s%s", e.Container, e.Video, e.Audio)
}

// Message is the sentence to show the user. It names the alternatives,
// because a refusal without them leaves somebody guessing through four
// codecs, and it names the codec the way a person does — "VP9", not the
// "vp9" identifier the API happens to use.
func (e *IncompatibleError) Message() string {
	what := e.Video.Label()
	if e.Video == "" {
		what = e.Audio.Label()
	}

	return fmt.Sprintf("%s cannot carry %s. This container takes: %s.",
		e.Container.Label(), what, strings.Join(e.Allowed, ", "))
}

// DurationError reports an input longer than the limit.
type DurationError struct {
	Duration time.Duration
	Limit    time.Duration
}

// Error implements error.
func (e *DurationError) Error() string {
	return fmt.Sprintf("video: the input is %s, past the %s limit", e.Duration, e.Limit)
}

// Message is the sentence to show the user, naming both figures.
func (e *DurationError) Message() string {
	return fmt.Sprintf("That video is %s long, and the limit for this tool is %s.",
		round(e.Duration), round(e.Limit))
}

// round trims a duration to whole seconds, which is the precision a message
// should carry.
func round(d time.Duration) time.Duration { return d.Round(time.Second) }

// Options is one video conversion.
type Options struct {
	// Container is the output container. Required.
	Container Container
	// Video is the video codec. Empty takes the container's default.
	Video Codec
	// Audio is the audio codec. Empty takes the container's default; Mute
	// drops the sound and Copy keeps the input's stream.
	Audio AudioCodec
	// CRF is the quality, on the chosen codec's own scale. Zero takes the
	// codec's default.
	CRF int
	// Height scales the video to this height, keeping the aspect ratio.
	// Zero leaves the resolution alone. Only downscaling: upscaling a video
	// produces a larger file that looks worse, which is never what somebody
	// who typed 720 wanted.
	Height int
	// FrameRate caps the frame rate. Zero leaves it alone.
	FrameRate float64
	// AudioBitrate is the audio bit rate in kilobits per second. Zero takes
	// the codec's default.
	AudioBitrate int
}

// MaxDuration is the longest input this package converts.
//
// Two hours. It is enforced from the probe before any work starts, because
// the cost of a transcode is linear in the duration and a six-hour input on a
// shared worker is an outage rather than a slow conversion.
const MaxDuration = 2 * time.Hour

// plan is a validated conversion: the ffmpeg arguments and what they will
// produce.
type plan struct {
	args      []string
	container containerSpec
	video     videoSpec
	audio     audioSpec
	// mute and copyAudio are the two audio choices that are not an encoder.
	mute      bool
	copyAudio bool
}

// buildPlan validates the options against the tables and the build, and
// returns the arguments.
//
// Every refusal happens here, before a process starts. That is the whole
// design: ffmpeg's own refusals arrive after a process has been spawned, name
// the encoder rather than the format, and carry the command line into the
// message.
func buildPlan(input, output string, o Options, caps ffmpeg.Capabilities, source ffmpeg.Media) (plan, error) {
	container, ok := containerByName[o.Container]
	if !ok {
		return plan{}, fmt.Errorf("video: %q is not a container this tool writes", o.Container)
	}

	videoCodec := o.Video
	if videoCodec == "" {
		videoCodec = container.defaultVideo
	}

	if !slices.Contains(container.video, videoCodec) {
		return plan{}, &IncompatibleError{
			Container: o.Container,
			Video:     videoCodec,
			Allowed:   labelsOfVideo(container.video),
		}
	}

	videoSpec := videoByCodec[videoCodec]

	if !caps.Has(videoSpec.encoder) {
		return plan{}, &ffmpeg.MissingEncoderError{
			Encoder:    videoSpec.encoder,
			Codec:      videoSpec.label,
			GPLOnly:    videoSpec.gplOnly,
			BuildIsGPL: caps.GPL,
		}
	}

	audioCodec := o.Audio
	if audioCodec == "" {
		audioCodec = container.defaultAudio
	}

	if !slices.Contains(container.audio, audioCodec) {
		return plan{}, &IncompatibleError{
			Container: o.Container,
			Audio:     audioCodec,
			Allowed:   labelsOfAudio(container.audio),
		}
	}

	p := plan{
		container: container,
		video:     videoSpec,
		mute:      audioCodec == Mute || !source.HasAudio(),
		copyAudio: audioCodec == Copy,
	}

	if !p.mute && !p.copyAudio {
		p.audio = audioByCodec[audioCodec]

		if !caps.Has(p.audio.encoder) {
			return plan{}, &ffmpeg.MissingEncoderError{
				Encoder:    p.audio.encoder,
				Codec:      p.audio.label,
				BuildIsGPL: caps.GPL,
			}
		}
	}

	p.args = assemble(input, output, o, p)

	return p, nil
}

// assemble builds the ffmpeg argument list for a validated plan.
func assemble(input, output string, o Options, p plan) []string {
	// -progress on stdout is what every percentage is read from; stderr
	// stays the error channel.
	args := append(ffmpeg.Base(), "-i", input, "-progress", "pipe:1")

	args = append(args, "-c:v", p.video.encoder,
		"-crf", strconv.Itoa(clamp(positive(o.CRF, p.video.crf), p.video.minCRF, p.video.maxCRF)))
	args = append(args, p.video.args...)

	if p.video.needsLogicalProcessors {
		// An integer derived from the container's own quota, never anything
		// a caller chose: this is a codec parameter string, which the
		// encoder parses itself, and the only safe value in a sub-language
		// is one that cannot carry its syntax.
		args = append(args, "-svtav1-params",
			"lp="+strconv.Itoa(min(cpu.Limit(), maxAV1LogicalProcessors)))
	}

	if o.Height > 0 {
		// -2 rather than -1 on the open axis: every codec here needs even
		// dimensions, and -1 produces an odd width on a 1080x1920 source,
		// which ffmpeg then refuses. min() keeps it to downscaling only.
		args = append(args, "-vf",
			fmt.Sprintf("scale=-2:'min(%d,ih)':flags=lanczos", o.Height))
	}

	if o.FrameRate > 0 {
		args = append(args, "-r", strconv.FormatFloat(o.FrameRate, 'f', -1, 64))
	}

	switch {
	case p.mute:
		args = append(args, "-an")
	case p.copyAudio:
		args = append(args, "-c:a", "copy")
	default:
		args = append(args, "-c:a", p.audio.encoder)

		if rate := positive(o.AudioBitrate, p.audio.bitrate); rate > 0 {
			args = append(args, "-b:a", strconv.Itoa(rate)+"k")
		}
	}

	// Only the first video and the first audio stream. Without this, a file
	// with three audio tracks and a subtitle produces an output ffmpeg may
	// not be able to mux, and a file with an embedded cover image makes
	// ffmpeg treat the cover as a second video stream.
	args = append(args, "-map", "0:v:0")

	if !p.mute {
		args = append(args, "-map", "0:a:0?")
	}

	args = append(args, p.container.args...)

	// The encoder's thread count, which has to come after the inputs: see
	// ffmpeg.Threads for why putting it only in the global position bounds
	// the decode and leaves the encoder unbounded.
	args = append(args, ffmpeg.Threads()...)

	return append(args, output)
}

// maxAV1LogicalProcessors caps SVT-AV1's own thread pool, and the cap is
// arithmetic rather than taste.
//
// RLIMIT_NPROC is a limit per real user, not per process: the worker's own
// threads count against the same budget as the engine's. Peak thread counts
// measured in the production image, with ffmpeg's own -threads at 2:
//
//	x264            12 threads
//	SVT-AV1 lp=1    25 threads
//	SVT-AV1 lp=2    56 threads
//	SVT-AV1 lp=4    79 threads
//
// The engine's process limit is 64 (app/services/engines.go), so lp=2 leaves
// eight for the entire worker process, which has more than that, and AV1
// fails with "Failed to create thread" after a banner that mentions memory.
// lp=1 fits with room to spare.
//
// Raising this is safe only together with that limit; the two numbers belong
// to each other, and this comment is where the arithmetic lives.
const maxAV1LogicalProcessors = 1

// labelsOfVideo and labelsOfAudio render a container's codec list for a
// message.
func labelsOfVideo(codecs []Codec) []string {
	out := make([]string, 0, len(codecs))
	for _, c := range codecs {
		out = append(out, c.Label())
	}

	return out
}

func labelsOfAudio(codecs []AudioCodec) []string {
	out := make([]string, 0, len(codecs))
	for _, c := range codecs {
		out = append(out, c.Label())
	}

	return out
}

// positive returns v when positive, and fallback otherwise.
func positive(v, fallback int) int {
	if v > 0 {
		return v
	}

	return fallback
}

// clamp bounds v to [lo, hi].
func clamp(v, lo, hi int) int { return min(max(v, lo), hi) }
