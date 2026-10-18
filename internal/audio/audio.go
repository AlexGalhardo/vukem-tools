// Package audio converts audio through ffmpeg.
//
// It is separate from internal/video because the decisions are different, not
// because the engine is. A video conversion is dominated by a quality ladder
// per codec; an audio conversion is dominated by the difference between a
// lossy codec, where a bit rate is the dial, and a lossless one, where asking
// for a bit rate is a category error. Mixing the two tables produced options
// that were meaningless for half the formats.
//
// Everything runs through internal/ffmpeg, which runs through internal/engine.
package audio

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/AlexGalhardo/vukem-tools/internal/ffmpeg"
)

// Format is an output audio format.
type Format string

// The formats this package writes. The string is the public identifier.
const (
	MP3  Format = "mp3"
	WAV  Format = "wav"
	FLAC Format = "flac"
	Opus Format = "opus"
	AAC  Format = "aac"
)

// spec describes one audio format.
type spec struct {
	format Format
	// encoder is the ffmpeg encoder name.
	encoder string
	// container is the ffmpeg muxer, which is not always the extension:
	// Opus goes in an Ogg container and AAC in an MP4 one.
	container string
	extension string
	label     string
	mime      string
	// lossy says whether a bit rate means anything. For FLAC and WAV it does
	// not: a bit rate on a lossless format is either ignored or a way to
	// make the file wrong.
	lossy bool
	// bitrate is the default in kilobits per second for a lossy format.
	bitrate int
	// minBitrate and maxBitrate bound what a client may ask for. The ranges
	// are per codec because the useful range is: Opus is transparent at 96k
	// where MP3 needs 192k, and asking for 320k Opus wastes half the file.
	minBitrate, maxBitrate int
	// args are the codec's own flags.
	args []string
}

// specs is the format table.
var specs = []spec{
	{
		format: MP3, encoder: "libmp3lame", container: "mp3", extension: ".mp3",
		label: "MP3", mime: "audio/mpeg",
		lossy: true, bitrate: 192, minBitrate: 64, maxBitrate: 320,
	},
	{
		format: WAV, encoder: "pcm_s16le", container: "wav", extension: ".wav",
		label: "WAV", mime: "audio/wav",
		// 16-bit little-endian PCM at the source's own rate: the format
		// everything reads, and the one a WAV is asked for in order to get.
		lossy: false,
	},
	{
		format: FLAC, encoder: "flac", container: "flac", extension: ".flac",
		label: "FLAC", mime: "audio/flac",
		lossy: false,
		// Level 8 is FLAC's own maximum, and the cost over the default 5 is
		// encode time rather than quality: the output is bit-identical on
		// decode either way.
		args: []string{"-compression_level", "8"},
	},
	{
		format: Opus, encoder: "libopus", container: "ogg", extension: ".opus",
		label: "Opus", mime: "audio/ogg",
		lossy: true, bitrate: 96, minBitrate: 32, maxBitrate: 256,
	},
	{
		format: AAC, encoder: "aac", container: "ipod", extension: ".m4a",
		label: "AAC", mime: "audio/aac",
		// The ipod muxer rather than mp4: it writes an .m4a that every
		// player and every phone reads, where a bare MP4 holding only audio
		// confuses several of them.
		lossy: true, bitrate: 160, minBitrate: 64, maxBitrate: 320,
	},
}

// byFormat indexes specs, built once at load.
var byFormat = func() map[Format]spec {
	m := make(map[Format]spec, len(specs))
	for i := range specs {
		m[specs[i].format] = specs[i]
	}

	return m
}()

// Extension returns the canonical extension, including the dot.
func (f Format) Extension() string { return byFormat[f].extension }

// MIME returns the media type.
func (f Format) MIME() string { return byFormat[f].mime }

// Label returns what a message should call the format.
func (f Format) Label() string {
	if s, ok := byFormat[f]; ok {
		return s.label
	}

	return string(f)
}

// Lossy reports whether a bit rate means anything for this format.
func (f Format) Lossy() bool { return byFormat[f].lossy }

// Known reports whether f is a format this package writes.
func (f Format) Known() bool {
	_, ok := byFormat[f]
	return ok
}

// Formats returns every format this package writes, in table order.
func Formats() []Format {
	out := make([]Format, 0, len(specs))
	for i := range specs {
		out = append(out, specs[i].format)
	}

	return out
}

// Available returns the formats this build can actually encode.
func Available(caps ffmpeg.Capabilities) []Format {
	var out []Format

	for i := range specs {
		if caps.Has(specs[i].encoder) {
			out = append(out, specs[i].format)
		}
	}

	return out
}

// MaxDuration is the longest input this package converts. Four hours: audio
// is an order of magnitude cheaper per second than video, and a long
// recording or an audiobook chapter is a legitimate input.
const MaxDuration = 4 * time.Hour

// DurationError reports an input longer than the limit.
type DurationError struct {
	Duration time.Duration
	Limit    time.Duration
}

// Error implements error.
func (e *DurationError) Error() string {
	return fmt.Sprintf("audio: the input is %s, past the %s limit", e.Duration, e.Limit)
}

// Message is the sentence to show the user, naming both figures.
func (e *DurationError) Message() string {
	return fmt.Sprintf("That recording is %s long, and the limit for this tool is %s.",
		e.Duration.Round(time.Second), e.Limit.Round(time.Second))
}

// NoAudioError means the file carries no audio stream.
type NoAudioError struct{}

// Error implements error.
func (*NoAudioError) Error() string { return "audio: the file has no audio stream" }

// Message is the sentence to show the user.
func (*NoAudioError) Message() string {
	return "That file has no sound in it, so there is nothing to convert."
}

// ErrNoAudioStream is the one instance, so errors.Is matches by identity.
var ErrNoAudioStream = &NoAudioError{}

// Options is one audio conversion.
type Options struct {
	// To is the output format. Required.
	To Format
	// Bitrate is the bit rate in kilobits per second, for a lossy format.
	// Zero takes the format's default; it is ignored for a lossless one
	// rather than refused, because a client that sets a bit rate and then
	// asks for FLAC is asking for FLAC.
	Bitrate int
	// SampleRate resamples the audio, in hertz. Zero keeps the source's.
	SampleRate int
	// Channels forces the channel count: 1 for mono, 2 for stereo. Zero
	// keeps the source's.
	Channels int
	// Normalise applies EBU R128 loudness normalisation.
	Normalise bool
	// TwoPassNormalise measures the whole file first and then applies the
	// exact correction. It costs a second decode and is the only way to hit
	// the target reliably; the single pass is a moving average that
	// undershoots on anything with a quiet opening.
	TwoPassNormalise bool
}

// Loudness targets, from EBU R128 as docs/web-research.md records them.
//
// -16 LUFS rather than broadcast's -23: the figure streaming platforms and
// podcast players normalise to, which is what somebody converting a file for
// the web actually wants.
const (
	LoudnessTarget   = -16.0
	LoudnessTruePeak = -1.5
	LoudnessRange    = 11.0
)

// loudnormFilter is the measurement pass's filter, which prints its findings
// as JSON rather than applying anything.
func loudnormFilter() string {
	return fmt.Sprintf("loudnorm=I=%g:TP=%g:LRA=%g:print_format=json",
		LoudnessTarget, LoudnessTruePeak, LoudnessRange)
}

// MissingFormatError reports a format this build cannot encode.
type MissingFormatError struct {
	Format Format
	// Available lists what this build can do, for the message.
	Available []string
}

// Error implements error.
func (e *MissingFormatError) Error() string {
	return "audio: this build has no encoder for " + string(e.Format)
}

// Message is the sentence to show the user, naming the alternatives.
func (e *MissingFormatError) Message() string {
	return e.Format.Label() + " is not available on this server. These are: " +
		strings.Join(e.Available, ", ") + "."
}

// plan is a validated conversion.
type plan struct {
	args []string
	spec spec
}

// buildPlan validates the options and returns the arguments.
func buildPlan(
	input, output string,
	o Options,
	caps ffmpeg.Capabilities,
	measured string,
) (plan, error) {
	s, ok := byFormat[o.To]
	if !ok {
		return plan{}, fmt.Errorf("audio: %q is not a format this tool writes", o.To)
	}

	if !caps.Has(s.encoder) {
		return plan{}, &MissingFormatError{Format: o.To, Available: labelsOf(Available(caps))}
	}

	// No video, ever: a music file with embedded cover art has a video
	// stream, and without -vn ffmpeg tries to carry it into a format that
	// cannot hold it.
	args := append(ffmpeg.Base(),
		"-i", input,
		"-progress", "pipe:1",
		"-vn", "-map", "0:a:0",
		"-c:a", s.encoder,
	)

	if s.lossy {
		rate := clamp(positive(o.Bitrate, s.bitrate), s.minBitrate, s.maxBitrate)
		args = append(args, "-b:a", strconv.Itoa(rate)+"k")
	}

	args = append(args, s.args...)

	if o.SampleRate > 0 {
		args = append(args, "-ar", strconv.Itoa(o.SampleRate))
	}

	if o.Channels > 0 {
		args = append(args, "-ac", strconv.Itoa(o.Channels))
	}

	if filter := normaliseFilter(o, measured); filter != "" {
		args = append(args, "-af", filter)
	}

	// The muxer is named rather than inferred from the extension: .m4a could
	// be several muxers, and the one that writes a file every player reads
	// is not the one ffmpeg guesses.
	args = append(args, "-f", s.container)

	// The encoder's thread count, after the input: see ffmpeg.Threads.
	args = append(args, ffmpeg.Threads()...)

	return plan{args: append(args, output), spec: s}, nil
}

// normaliseFilter returns the loudness filter, which differs between the two
// passes.
//
// The second pass is handed the first pass's measurements, which is what
// makes it exact: with them, loudnorm applies one known gain to the whole
// file; without them it tracks a moving average and undershoots the target on
// anything with a quiet opening.
func normaliseFilter(o Options, measured string) string {
	if !o.Normalise {
		return ""
	}

	base := fmt.Sprintf("loudnorm=I=%g:TP=%g:LRA=%g",
		LoudnessTarget, LoudnessTruePeak, LoudnessRange)

	if measured == "" {
		return base
	}

	return base + ":" + measured
}

// labelsOf renders a format list for a message.
func labelsOf(formats []Format) []string {
	out := make([]string, 0, len(formats))
	for _, f := range formats {
		out = append(out, f.Label())
	}

	return out
}

// MIMETypes returns the media types this package reads, which is what a
// tool's accepted list should be: every audio type the service knows plus
// every video type, because extracting a soundtrack is the same operation.
func MIMETypes() []string {
	out := []string{
		"audio/mpeg", "audio/wav", "audio/flac", "audio/ogg", "audio/aac",
		"video/mp4", "video/webm", "video/x-matroska", "video/quicktime", "video/x-msvideo",
	}

	slices.Sort(out)

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
