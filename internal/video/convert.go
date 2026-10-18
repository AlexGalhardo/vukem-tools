package video

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"time"

	"github.com/AlexGalhardo/vukem-tools/internal/ffmpeg"
)

// Result is what a conversion produced.
type Result struct {
	// Path is the output file.
	Path string
	// Source and Output are the probes of the input and of the result. The
	// output is read back from the file that was written rather than
	// predicted: asserting that a conversion produced the right container,
	// codecs, duration and resolution is only worth anything if it reads the
	// real file.
	Source ffmpeg.Media
	Output ffmpeg.Media
	// CPUTime is the engine's own processor time in milliseconds, and MaxRSS
	// its peak resident memory in bytes.
	CPUTime int64
	MaxRSS  int64
}

// Convert transcodes a video.
//
// The order is probe, validate, then encode, and the first two are cheap: the
// probe is a few hundred milliseconds whatever the file's size, and every
// refusal this package makes happens before the encoder starts. A transcode
// is the most expensive thing this service does, so spending a process to
// avoid starting one that cannot succeed is always the right trade.
func Convert(
	ctx context.Context,
	e ffmpeg.Engine,
	r ffmpeg.Runner,
	caps ffmpeg.Capabilities,
	dir, input string,
	o Options,
	report func(ffmpeg.Progress) error,
) (Result, error) {
	source, err := ffmpeg.ProbeMedia(ctx, e, r, dir, input)
	if err != nil {
		return Result{}, err
	}

	if !source.HasVideo() {
		return Result{}, ErrNoVideoStream
	}

	if source.Duration > MaxDuration {
		return Result{}, &DurationError{Duration: source.Duration, Limit: MaxDuration}
	}

	output := filepath.Join(dir, "output"+o.Container.Extension())

	p, err := buildPlan(input, output, o, caps, source)
	if err != nil {
		return Result{}, err
	}

	result, err := ffmpeg.Run(ctx, e, r, dir, p.args, source.Duration, report)
	if err != nil {
		return Result{}, err
	}

	out, err := ffmpeg.ProbeMedia(ctx, e, r, dir, output)
	if err != nil {
		return Result{}, err
	}

	return Result{
		Path:    output,
		Source:  source,
		Output:  out,
		CPUTime: result.CPUTime.Milliseconds(),
		MaxRSS:  result.MaxRSS,
	}, nil
}

// NoVideoError means the file has sound but no picture, which makes it an
// audio file however it is named.
//
// A type rather than a plain sentinel because it carries the sentence to show
// the user, and that sentence is the useful part: it says what to do instead.
type NoVideoError struct{}

// Error implements error.
func (*NoVideoError) Error() string { return "video: the file has no video stream" }

// Message is the sentence to show the user.
func (*NoVideoError) Message() string {
	return "That file has no video in it. Use the audio converter instead."
}

// ErrNoVideoStream is the one instance, so errors.Is matches it by identity
// and errors.As matches it by type.
var ErrNoVideoStream = &NoVideoError{}

// GIFOptions is a video-to-GIF conversion.
type GIFOptions struct {
	// FPS is the frame rate of the GIF. 12 is the default: GIF stores a
	// delay per frame in hundredths of a second, so rates that do not divide
	// 100 evenly are rounded by the player and play unevenly, and 12 is both
	// smooth enough and small.
	FPS int
	// Width scales the GIF to this width, keeping the aspect ratio. Zero
	// keeps 480, because a GIF is an animation format with no interframe
	// compression worth the name: doubling the width quadruples the file.
	Width int
	// Start and Duration select a range. A zero Duration takes the whole
	// video, bounded by MaxGIFDuration.
	Start    time.Duration
	Duration time.Duration
	// Dither chooses the dithering. Empty uses a sensible default.
	Dither string
}

// MaxGIFDuration is the longest range this package will turn into a GIF.
//
// Fifteen seconds. A GIF has no interframe compression to speak of, so the
// file grows linearly with the duration and a one-minute GIF at any useful
// size is tens of megabytes — a worse result than the video it came from, and
// one nobody wants once they see it.
const MaxGIFDuration = 15 * time.Second

// The GIF defaults.
const (
	defaultGIFFPS    = 12
	defaultGIFWidth  = 480
	defaultGIFDither = "sierra2_4a"
)

// ToGIF turns a range of a video into a GIF, in two passes.
//
// Two passes rather than one, and this is the difference between a GIF that
// looks like the video and one that looks like 1995. GIF holds 256 colours,
// and ffmpeg's single-pass path quantises to a fixed web-safe palette, which
// turns a sunset into bands. The two-pass path computes a palette from the
// frames that are actually in the clip and then maps the frames onto it.
//
// Both passes run as separate processes against the same input, which is read
// twice. That is the cost, and it is worth it: the palette has to exist before
// the mapping can use it, and the alternative — one ffmpeg process with a
// filter graph that splits and reuses the stream — needs the whole clip in
// memory to compute the palette before emitting the first frame.
func ToGIF(
	ctx context.Context,
	e ffmpeg.Engine,
	r ffmpeg.Runner,
	dir, input string,
	o GIFOptions,
	report func(ffmpeg.Progress) error,
) (Result, error) {
	source, err := ffmpeg.ProbeMedia(ctx, e, r, dir, input)
	if err != nil {
		return Result{}, err
	}

	if !source.HasVideo() {
		return Result{}, ErrNoVideoStream
	}

	span := o.Duration
	if span <= 0 {
		span = source.Duration - o.Start
	}

	if span > MaxGIFDuration {
		return Result{}, &DurationError{Duration: span, Limit: MaxGIFDuration}
	}

	fps := positive(o.FPS, defaultGIFFPS)
	width := positive(o.Width, defaultGIFWidth)

	dither := o.Dither
	if dither == "" {
		dither = defaultGIFDither
	}

	// The same filter chain in both passes, so the palette is computed from
	// exactly the frames it will be applied to. A palette built from the
	// full-resolution frames and applied to scaled ones is subtly wrong, and
	// the scaling is where most of the colours come from.
	chain := fmt.Sprintf("fps=%d,scale=%d:-1:flags=lanczos", fps, width)

	palette := filepath.Join(dir, "palette.png")

	paletteArgs := append(ffmpeg.Base(), rangeArgs(o.Start, span)...)
	paletteArgs = append(paletteArgs,
		"-i", input,
		"-vf", chain+",palettegen=stats_mode=diff",
		"-frames:v", "1",
	)
	paletteArgs = append(paletteArgs, ffmpeg.Threads()...)
	paletteArgs = append(paletteArgs, palette)

	if _, err := ffmpeg.Run(ctx, e, r, dir, paletteArgs, span, nil); err != nil {
		return Result{}, err
	}

	output := filepath.Join(dir, "output.gif")

	mapArgs := append(ffmpeg.Base(), rangeArgs(o.Start, span)...)
	mapArgs = append(mapArgs,
		"-i", input,
		"-i", palette,
		"-progress", "pipe:1",
		"-lavfi", chain+"[x];[x][1:v]paletteuse=dither="+dither,
		// A GIF loops forever by convention, and a GIF that plays once is
		// almost never what somebody wanted.
		"-loop", "0",
	)
	mapArgs = append(mapArgs, ffmpeg.Threads()...)
	mapArgs = append(mapArgs, output)

	result, err := ffmpeg.Run(ctx, e, r, dir, mapArgs, span, report)
	if err != nil {
		return Result{}, err
	}

	out, err := ffmpeg.ProbeMedia(ctx, e, r, dir, output)
	if err != nil {
		return Result{}, err
	}

	return Result{
		Path:    output,
		Source:  source,
		Output:  out,
		CPUTime: result.CPUTime.Milliseconds(),
		MaxRSS:  result.MaxRSS,
	}, nil
}

// rangeArgs selects a range of the input.
//
// -ss before -i rather than after, which is the whole trick: before the input
// it is an input option and ffmpeg seeks to the nearest keyframe before
// decoding, so a clip from an hour in costs a seek. After the input it is an
// output option and ffmpeg decodes and discards the hour.
func rangeArgs(start, span time.Duration) []string {
	var args []string

	if start > 0 {
		args = append(args, "-ss", seconds(start))
	}

	if span > 0 {
		args = append(args, "-t", seconds(span))
	}

	return args
}

// seconds renders a duration as ffmpeg's fractional seconds.
func seconds(d time.Duration) string {
	return strconv.FormatFloat(d.Seconds(), 'f', 3, 64)
}

// errSpriteNeedsDuration means the file carries no duration, so the sampling
// rate a sprite sheet needs cannot be computed from it.
var errSpriteNeedsDuration = errors.New(
	"video: a sprite sheet needs a duration, and this file carries none")

// ThumbnailOptions is a single frame taken out of a video.
type ThumbnailOptions struct {
	// At is where in the video to take the frame. Zero takes one a little
	// way in rather than the first frame: the first frame of a video is very
	// often black, and a black thumbnail looks like a broken conversion.
	At time.Duration
	// Width scales the frame, keeping the aspect ratio. Zero keeps the
	// video's own width.
	Width int
}

// defaultThumbnailAt is where a thumbnail comes from when the caller names no
// time. Far enough in to be past a fade from black, near enough to be in
// every clip worth thumbnailing.
const defaultThumbnailAt = 3 * time.Second

// Thumbnail writes one frame of a video as a PNG.
func Thumbnail(
	ctx context.Context,
	e ffmpeg.Engine,
	r ffmpeg.Runner,
	dir, input string,
	o ThumbnailOptions,
) (Result, error) {
	source, err := ffmpeg.ProbeMedia(ctx, e, r, dir, input)
	if err != nil {
		return Result{}, err
	}

	if !source.HasVideo() {
		return Result{}, ErrNoVideoStream
	}

	at := o.At
	if at <= 0 {
		at = min(defaultThumbnailAt, source.Duration/2)
	}

	// A requested time past the end would produce no frame and an empty
	// file, so it is pulled back rather than refused: somebody asking for
	// the frame at 10s of a 6s clip means "near the end".
	if source.Duration > 0 && at >= source.Duration {
		at = source.Duration * 9 / 10
	}

	output := filepath.Join(dir, "thumbnail.png")

	args := append(ffmpeg.Base(), "-ss", seconds(at))
	args = append(args, "-i", input, "-frames:v", "1")

	if o.Width > 0 {
		args = append(args, "-vf", fmt.Sprintf("scale=%d:-2:flags=lanczos", o.Width))
	}

	args = append(args, ffmpeg.Threads()...)

	result, err := ffmpeg.Run(ctx, e, r, dir, append(args, output), 0, nil)
	if err != nil {
		return Result{}, err
	}

	out, err := ffmpeg.ProbeMedia(ctx, e, r, dir, output)
	if err != nil {
		return Result{}, err
	}

	return Result{
		Path:    output,
		Source:  source,
		Output:  out,
		CPUTime: result.CPUTime.Milliseconds(),
		MaxRSS:  result.MaxRSS,
	}, nil
}

// SpriteOptions is a contact sheet of frames taken across a video.
type SpriteOptions struct {
	// Columns and Rows are the grid. Zero takes the defaults.
	Columns int
	Rows    int
	// Width is the width of one tile. Zero takes the default.
	Width int
}

// The sprite-sheet defaults: a 5 by 5 grid of 320-wide tiles, which is a
// 1600x900-ish sheet for a 16:9 source — a size a browser can show at once.
const (
	defaultSpriteColumns = 5
	defaultSpriteRows    = 5
	defaultSpriteWidth   = 320
	maxSpriteTiles       = 100
)

// SpriteSheet writes a grid of frames taken evenly across the video.
//
// The sampling rate is computed from the duration so the grid covers the
// whole video rather than its first few seconds. That is the part a fixed
// `fps=1/10` gets wrong: on a thirty-second clip it fills three cells, and on
// a two-hour film it would need seven hundred.
func SpriteSheet(
	ctx context.Context,
	e ffmpeg.Engine,
	r ffmpeg.Runner,
	dir, input string,
	o SpriteOptions,
	report func(ffmpeg.Progress) error,
) (Result, error) {
	source, err := ffmpeg.ProbeMedia(ctx, e, r, dir, input)
	if err != nil {
		return Result{}, err
	}

	if !source.HasVideo() {
		return Result{}, ErrNoVideoStream
	}

	if source.Duration <= 0 {
		return Result{}, errSpriteNeedsDuration
	}

	columns := positive(o.Columns, defaultSpriteColumns)
	rows := positive(o.Rows, defaultSpriteRows)
	width := positive(o.Width, defaultSpriteWidth)

	if columns*rows > maxSpriteTiles {
		return Result{}, fmt.Errorf("video: %dx%d is more than the %d tiles this tool writes",
			columns, rows, maxSpriteTiles)
	}

	tiles := columns * rows

	// One frame per slice of the video, with the slice width in seconds as a
	// rational so ffmpeg does not round it to zero on a short clip.
	chain := fmt.Sprintf("fps=%d/%d,scale=%d:-2:flags=lanczos,tile=%dx%d",
		tiles, max(int(source.Duration.Seconds()), 1), width, columns, rows)

	output := filepath.Join(dir, "sprite.png")

	args := append(ffmpeg.Base(),
		"-i", input,
		"-progress", "pipe:1",
		"-vf", chain,
		"-frames:v", "1",
	)
	args = append(args, ffmpeg.Threads()...)
	args = append(args, output)

	result, err := ffmpeg.Run(ctx, e, r, dir, args, source.Duration, report)
	if err != nil {
		return Result{}, err
	}

	out, err := ffmpeg.ProbeMedia(ctx, e, r, dir, output)
	if err != nil {
		return Result{}, err
	}

	return Result{
		Path:    output,
		Source:  source,
		Output:  out,
		CPUTime: result.CPUTime.Milliseconds(),
		MaxRSS:  result.MaxRSS,
	}, nil
}

// ReplaceAudio muxes a new soundtrack onto a video without re-encoding the
// picture.
//
// The video is copied rather than transcoded, which is the whole point: a
// soundtrack swap should cost the price of a mux, not of a transcode, and
// re-encoding the picture to change the sound would lose quality for nothing.
// The audio is re-encoded, because the replacement arrives in whatever format
// the user had and has to end up in one the container carries.
//
// -shortest ends the output at whichever stream runs out first. Without it, a
// thirty-second song over a ten-second clip produces a file whose last twenty
// seconds are a frozen frame, which looks broken rather than deliberate.
func ReplaceAudio(
	ctx context.Context,
	e ffmpeg.Engine,
	r ffmpeg.Runner,
	caps ffmpeg.Capabilities,
	dir, video, audio string,
	container Container,
	report func(ffmpeg.Progress) error,
) (Result, error) {
	source, err := ffmpeg.ProbeMedia(ctx, e, r, dir, video)
	if err != nil {
		return Result{}, err
	}

	if !source.HasVideo() {
		return Result{}, ErrNoVideoStream
	}

	replacement, err := ffmpeg.ProbeMedia(ctx, e, r, dir, audio)
	if err != nil {
		return Result{}, err
	}

	if !replacement.HasAudio() {
		return Result{}, ErrNoReplacementAudio
	}

	spec, ok := containerByName[container]
	if !ok {
		return Result{}, fmt.Errorf("video: %q is not a container this tool writes", container)
	}

	audioSpec := audioByCodec[spec.defaultAudio]

	if !caps.Has(audioSpec.encoder) {
		return Result{}, &ffmpeg.MissingEncoderError{
			Encoder:    audioSpec.encoder,
			Codec:      audioSpec.label,
			BuildIsGPL: caps.GPL,
		}
	}

	output := filepath.Join(dir, "output"+spec.extension)

	args := append(ffmpeg.Base(),
		"-i", video,
		"-i", audio,
		"-progress", "pipe:1",
		"-map", "0:v:0",
		"-map", "1:a:0",
		"-c:v", "copy",
		"-c:a", audioSpec.encoder,
	)

	if audioSpec.bitrate > 0 {
		args = append(args, "-b:a", strconv.Itoa(audioSpec.bitrate)+"k")
	}

	args = append(args, "-shortest")
	args = append(args, spec.args...)
	args = append(args, ffmpeg.Threads()...)

	result, err := ffmpeg.Run(ctx, e, r, dir, append(args, output), source.Duration, report)
	if err != nil {
		return Result{}, err
	}

	out, err := ffmpeg.ProbeMedia(ctx, e, r, dir, output)
	if err != nil {
		return Result{}, err
	}

	return Result{
		Path:    output,
		Source:  source,
		Output:  out,
		CPUTime: result.CPUTime.Milliseconds(),
		MaxRSS:  result.MaxRSS,
	}, nil
}

// NoReplacementAudioError means the file offered as a new soundtrack has no
// audio in it.
type NoReplacementAudioError struct{}

// Error implements error.
func (*NoReplacementAudioError) Error() string {
	return "video: the replacement file has no audio stream"
}

// Message is the sentence to show the user.
func (*NoReplacementAudioError) Message() string {
	return "The file you chose as the new soundtrack has no sound in it."
}

// ErrNoReplacementAudio is the one instance.
var ErrNoReplacementAudio = &NoReplacementAudioError{}
