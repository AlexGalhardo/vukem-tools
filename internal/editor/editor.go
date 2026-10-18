// Package editor cuts, crops and splits video by driving ffmpeg.
//
// Two ideas shape everything here.
//
// The first is that every number a request carries is checked against the
// file rather than believed. A crop is validated against the dimensions
// ffprobe read out of the stream, not against what the client said the video
// was, because a crop beyond the frame is the cheapest way to make ffmpeg
// spend a worker producing an error.
//
// The second is that a cut should not re-encode when it does not have to.
// Trimming with `-c copy` is I/O rather than compute — two orders of
// magnitude cheaper — but it can only cut where a keyframe already is. So the
// keyframe near the cut is looked up, a copy is used when the cut lands on
// one, and a re-encode is used when it does not or when the caller asked for
// frame accuracy. Nothing here guesses: ffprobe answers both questions.
package editor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/AlexGalhardo/vukem-tools/internal/engine"
)

// Runner runs one engine under its limits. *engine.Runner satisfies it.
type Runner interface {
	Run(ctx context.Context, cmd engine.Command) (engine.Result, error)
}

// Options configures an Editor. Runner, FFmpeg and FFprobe are required.
type Options struct {
	Runner  Runner
	FFmpeg  string
	FFprobe string
	// MaxOutputDuration is the longest output an edit may produce. It is
	// checked before any work starts, which is the only way it bounds cost
	// rather than just truncating the answer.
	MaxOutputDuration time.Duration
	Log               *slog.Logger
}

// Editor drives ffmpeg and ffprobe.
type Editor struct {
	run       Runner
	ffmpeg    string
	ffprobe   string
	maxOutput time.Duration
	log       *slog.Logger
}

// New returns an editor.
func New(o Options) (*Editor, error) {
	switch {
	case o.Runner == nil:
		return nil, errors.New("editor: a runner is required")
	case o.FFmpeg == "":
		return nil, errors.New("editor: the path of ffmpeg is required")
	case o.FFprobe == "":
		return nil, errors.New("editor: the path of ffprobe is required")
	}

	if o.Log == nil {
		o.Log = slog.Default()
	}

	return &Editor{
		run:       o.Runner,
		ffmpeg:    o.FFmpeg,
		ffprobe:   o.FFprobe,
		maxOutput: o.MaxOutputDuration,
		log:       o.Log,
	}, nil
}

// MaxOutputDuration is the longest output this editor will produce, zero for
// no limit.
func (e *Editor) MaxOutputDuration() time.Duration { return e.maxOutput }

// Crop is a rectangle to keep, in pixels from the top left.
type Crop struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// Zero reports whether no crop was asked for.
func (c Crop) Zero() bool { return c == Crop{} }

// filter renders the crop as an ffmpeg filter.
func (c Crop) filter() string {
	return fmt.Sprintf("crop=%d:%d:%d:%d", c.Width, c.Height, c.X, c.Y)
}

// validate refuses a crop that does not fit inside the real frame.
//
// The message names the real dimensions, because "that crop is invalid" is
// something nobody can act on and "the video is 1280x720" is.
func (c Crop) validate(m Media) error {
	if !m.HasVideo() {
		return fail(ReasonUnsupportedInput, "That file has no video to crop.",
			"a crop was requested for a file with no video stream", nil)
	}

	if c.Width <= 0 || c.Height <= 0 || c.X < 0 || c.Y < 0 {
		return fail(ReasonOutOfRange,
			fmt.Sprintf("The area to keep must be inside the video, which is %s.", m.Dimensions()),
			fmt.Sprintf("crop %+v has a non-positive size or a negative origin", c), nil)
	}

	if c.X+c.Width > m.Width || c.Y+c.Height > m.Height {
		return fail(ReasonOutOfRange,
			fmt.Sprintf("That area reaches %dx%d, but the video is only %s. Pick an area inside it.",
				c.X+c.Width, c.Y+c.Height, m.Dimensions()),
			fmt.Sprintf("crop %+v exceeds %s", c, m.Dimensions()), nil)
	}

	return nil
}

// TrimRequest is one cut, optionally cropped.
type TrimRequest struct {
	// Dir is the working directory. Input must be inside it and the output
	// is written into it.
	Dir string
	// Input is the file to cut.
	Input string
	// Output is the file name to write, relative to Dir.
	Output string
	// Start is where the cut begins. Zero means the start of the file.
	Start time.Duration
	// Duration is how much to keep. Zero means to the end of the file.
	Duration time.Duration
	// FrameAccurate forces a re-encode so the cut lands exactly on Start
	// rather than on the nearest keyframe before it.
	FrameAccurate bool
	// Crop, when set, is the area to keep. A crop always re-encodes: there
	// is no way to change the frame size of a stream that is being copied.
	Crop Crop
	// Progress receives each update. Optional.
	Progress func(Progress)
}

// Result is a finished edit.
type Result struct {
	// Path is the absolute path of the output.
	Path string
	// Bytes is its size.
	Bytes int64
	// CPUTime is what ffmpeg actually used.
	CPUTime time.Duration
	// ReEncoded says which branch ran, so a benchmark can show the
	// difference and a caller can explain it.
	ReEncoded bool
	// Source is what ffprobe read out of the input, so a caller does not
	// have to probe it again to report the real dimensions.
	Source Media
}

// keyframeTolerance is how close a cut has to be to a keyframe for a copy to
// be honest.
//
// A copy cuts at the keyframe, not at the requested instant, so the tolerance
// is the error the caller silently accepts. 40 ms is about one frame at 25fps:
// close enough that nobody can see it, far enough that an ordinary cut on a
// keyframe is not pushed into a re-encode by floating-point rounding.
const keyframeTolerance = 40 * time.Millisecond

// Trim cuts the input and returns the file it produced.
func (e *Editor) Trim(ctx context.Context, in TrimRequest) (Result, error) {
	if in.Dir == "" || in.Input == "" || in.Output == "" {
		return Result{}, fail(ReasonEngine, "That video could not be edited.",
			"a trim needs a directory, an input and an output name", nil)
	}

	// Output is joined onto the job's directory, so it has to be a plain
	// file name. Every caller in this repository passes a constant, which is
	// exactly why this is worth checking: the field is exported, and the
	// first caller to pass something derived from a title or an upload name
	// would otherwise be writing wherever that name points.
	if !plainName(in.Output) {
		return Result{}, fail(ReasonEngine, "That video could not be edited.",
			"the output name is a path rather than a file name", nil)
	}

	source, err := e.Probe(ctx, in.Dir, in.Input)
	if err != nil {
		return Result{}, err
	}

	want, err := e.outputDuration(in, source)
	if err != nil {
		return Result{}, err
	}

	if !in.Crop.Zero() {
		if err := in.Crop.validate(source); err != nil {
			return Result{}, err
		}
	}

	reEncode := in.FrameAccurate || !in.Crop.Zero() || !e.cutLandsOnAKeyframe(ctx, in)

	out := filepath.Join(in.Dir, in.Output)

	args := e.trimArgs(in, out, reEncode)

	result, err := e.run.Run(ctx, engine.Command{
		Path:     e.ffmpeg,
		Args:     args,
		Dir:      in.Dir,
		Progress: progressReader(want, in.Progress),
	})
	if err != nil {
		return Result{}, Classify(err)
	}

	size, err := sizeOf(out)
	if err != nil {
		return Result{}, err
	}

	return Result{Path: out, Bytes: size, CPUTime: result.CPUTime, ReEncoded: reEncode, Source: source}, nil
}

// outputDuration works out how long the output will be and refuses one past
// the limit.
//
// It is checked here, before ffmpeg is started, because a limit enforced
// afterwards has already cost everything it was meant to save.
func (e *Editor) outputDuration(in TrimRequest, source Media) (time.Duration, error) {
	if in.Start < 0 {
		return 0, fail(ReasonOutOfRange, "The start of the cut cannot be negative.",
			"negative start "+in.Start.String(), nil)
	}

	if source.Duration > 0 && in.Start >= source.Duration {
		return 0, fail(ReasonOutOfRange,
			fmt.Sprintf("That start is past the end of the video, which is %s long.",
				source.Duration.Round(time.Second)),
			fmt.Sprintf("start %s is past the %s duration", in.Start, source.Duration), nil)
	}

	want := in.Duration

	if want <= 0 {
		// To the end of the file. An unknown source duration leaves this
		// zero, which only costs the progress percentage.
		want = max(source.Duration-in.Start, 0)
	} else if source.Duration > 0 {
		want = min(want, source.Duration-in.Start)
	}

	if e.maxOutput > 0 && want > e.maxOutput {
		return 0, fail(ReasonTooLong,
			fmt.Sprintf("That cut would be %s long, and this tool produces at most %s.",
				want.Round(time.Second), e.maxOutput.Round(time.Second)),
			fmt.Sprintf("requested %s against a %s limit", want, e.maxOutput), nil)
	}

	return want, nil
}

// cutLandsOnAKeyframe reports whether a copy would cut where the caller asked.
//
// A cut at the very start always lands on one, so the probe is skipped for
// it: that is the common case for "take the first thirty seconds", and paying
// for an ffprobe to learn something the container guarantees would be waste.
func (e *Editor) cutLandsOnAKeyframe(ctx context.Context, in TrimRequest) bool {
	if in.Start == 0 {
		return true
	}

	at, found := e.keyframeNear(ctx, in.Dir, in.Input, in.Start)
	if !found {
		return false
	}

	return absDuration(at-in.Start) <= keyframeTolerance
}

// trimArgs builds ffmpeg's command line.
//
// -ss before -i is the fast seek: ffmpeg jumps in the container instead of
// decoding from the start, which is what makes a copy cheap and a re-encode
// of a late segment bearable. -nostdin matters because ffmpeg reads stdin for
// keyboard commands by default, and an engine that consumes the worker's
// stdin is an engine that can hang it.
func (e *Editor) trimArgs(in TrimRequest, out string, reEncode bool) []string {
	args := []string{
		"-hide_banner",
		"-nostdin",
		"-nostats",
		"-loglevel", "error",
		"-y",
		"-progress", "pipe:1",
	}

	if in.Start > 0 {
		args = append(args, "-ss", seconds(in.Start))
	}

	args = append(args, "-i", in.Input)

	if in.Duration > 0 {
		args = append(args, "-t", seconds(in.Duration))
	}

	if !reEncode {
		// -avoid_negative_ts make_zero rebases the timestamps of the copied
		// stream. Without it a cut that starts mid-GOP produces a file whose
		// first timestamps are negative, which several players show as a
		// frozen first second.
		return append(args, "-c", "copy", "-avoid_negative_ts", "make_zero", "-movflags", "+faststart", out)
	}

	if !in.Crop.Zero() {
		args = append(args, "-vf", in.Crop.filter())
	}

	args = append(args, encodeArgs()...)

	return append(args, out)
}

// encodeArgs is the re-encode profile: H.264 at a visually transparent CRF
// with AAC audio, in a container every browser plays.
//
// -pix_fmt yuv420p is not optional. A source in yuv444p or 10-bit, which a
// screen recorder produces, encodes to an H.264 profile Safari and most
// hardware decoders refuse, and the result is a file that plays nowhere the
// user is likely to open it.
func encodeArgs() []string {
	return []string{
		"-c:v", "libx264",
		"-preset", "veryfast",
		"-crf", "20",
		"-pix_fmt", "yuv420p",
		"-c:a", "aac",
		"-b:a", "128k",
		"-movflags", "+faststart",
	}
}

// MaxClip is the longest clip the clips mode produces. It is the ROADMAP's
// figure, and the interface states it.
const MaxClip = 60 * time.Second

// SegmentRequest is one split into consecutive clips.
type SegmentRequest struct {
	// Dir is the working directory; Input must be inside it and the clips
	// are written into it.
	Dir   string
	Input string
	// Segment is how long each clip should be. Zero means MaxClip, and
	// anything longer is clamped to it.
	Segment time.Duration
	// Progress receives each update. Optional.
	Progress func(Progress)
}

// Clip is one of the pieces a split produced.
type Clip struct {
	// Index is its place in the sequence, from zero.
	Index int `json:"index"`
	// Path is the absolute path of the file.
	Path string `json:"-"`
	// Name is the file name, which is what a zip entry is called.
	Name string `json:"name"`
	// Duration is what ffprobe read back out of the clip, not what was
	// asked for.
	Duration time.Duration `json:"duration"`
	// Bytes is its size.
	Bytes int64 `json:"bytes"`
}

// clipPattern is the output template the segment muxer fills in. Three digits
// is a thousand clips, which at a minute each is sixteen hours of video —
// well past any input this service accepts.
const clipPattern = "clip%03d.mp4"

// Segment splits the input into consecutive clips of at most Segment each.
//
// It tries the cheap way first: the segment muxer copying the streams, which
// is I/O rather than compute. That muxer can only cut at a keyframe, so a
// source with sparse keyframes — a long screen recording often has one every
// ten seconds, and some have one per minute — produces clips longer than
// asked for. The clips are therefore measured, and only when one is too long
// is the whole split redone with forced keyframes, which re-encodes. That
// way the common case costs nothing and the guarantee still holds.
func (e *Editor) Segment(ctx context.Context, in SegmentRequest) ([]Clip, error) {
	if in.Dir == "" || in.Input == "" {
		return nil, fail(ReasonEngine, "That video could not be split.",
			"a split needs a directory and an input", nil)
	}

	limit := in.Segment
	if limit <= 0 || limit > MaxClip {
		limit = MaxClip
	}

	source, err := e.Probe(ctx, in.Dir, in.Input)
	if err != nil {
		return nil, err
	}

	clips, err := e.segmentPass(ctx, in, source, limit, false)
	if err != nil {
		return nil, err
	}

	if !tooLong(clips, limit) {
		return clips, nil
	}

	e.log.InfoContext(ctx, "editor: the copied clips overran, re-encoding with forced keyframes",
		"limit", limit, "clips", len(clips))

	if err := removeClips(clips); err != nil {
		return nil, err
	}

	return e.segmentPass(ctx, in, source, limit, true)
}

// segmentOvershoot is how far past the limit a copied clip may land before
// the split is redone.
//
// It is not zero because the segment muxer cuts at the first keyframe at or
// after each boundary, and a container's timestamps carry rounding of their
// own. A tenth of a second is below what anybody notices and well under the
// keyframe spacing that would make a copy wrong.
const segmentOvershoot = 100 * time.Millisecond

// tooLong reports whether any clip overran the limit.
func tooLong(clips []Clip, limit time.Duration) bool {
	for i := range clips {
		if clips[i].Duration > limit+segmentOvershoot {
			return true
		}
	}

	return false
}

// removeClips deletes a failed pass's output, so the second pass does not
// find the first one's files and report them as clips.
func removeClips(clips []Clip) error {
	for i := range clips {
		if err := os.Remove(clips[i].Path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fail(ReasonEngine, "That video could not be split.",
				"remove a discarded clip: "+err.Error(), err)
		}
	}

	return nil
}

// segmentPass runs the segment muxer once, copying or re-encoding.
func (e *Editor) segmentPass(
	ctx context.Context,
	in SegmentRequest,
	source Media,
	limit time.Duration,
	forceKeyframes bool,
) ([]Clip, error) {
	args := []string{
		"-hide_banner",
		"-nostdin",
		"-nostats",
		"-loglevel", "error",
		"-y",
		"-progress", "pipe:1",
		"-i", in.Input,
	}

	if forceKeyframes {
		// A keyframe is forced at every boundary, so the muxer has one to
		// cut at and every clip is at most `limit` long. segment_time_delta
		// absorbs the sub-frame difference between the forced keyframe's
		// timestamp and the boundary.
		args = append(args,
			"-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%s)", seconds(limit)),
		)
		args = append(args, encodeArgs()...)
		args = append(args, "-segment_time_delta", "0.05")
	} else {
		args = append(args, "-c", "copy")
	}

	args = append(args,
		"-f", "segment",
		"-segment_time", seconds(limit),
		"-reset_timestamps", "1",
		clipPattern,
	)

	if _, err := e.run.Run(ctx, engine.Command{
		Path:     e.ffmpeg,
		Args:     args,
		Dir:      in.Dir,
		Progress: progressReader(source.Duration, in.Progress),
	}); err != nil {
		return nil, Classify(err)
	}

	return e.collectClips(ctx, in.Dir)
}

// collectClips finds the clips a pass wrote and measures each one.
//
// Each clip's duration is read back with ffprobe rather than assumed from
// -segment_time, because the assumption is exactly the thing that is not
// true for a copied split, and because the reported duration is what the API
// publishes.
func (e *Editor) collectClips(ctx context.Context, dir string) ([]Clip, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fail(ReasonEngine, "That video could not be split.",
			"read the working directory: "+err.Error(), err)
	}

	var names []string

	for _, entry := range entries {
		if !entry.IsDir() && isClipName(entry.Name()) {
			names = append(names, entry.Name())
		}
	}

	if len(names) == 0 {
		return nil, fail(ReasonEngine, "That video could not be split.",
			"ffmpeg exited 0 but wrote no clip", nil)
	}

	// The names are zero-padded, so lexical order is numeric order, which is
	// the order the clips must be delivered in.
	sort.Strings(names)

	clips := make([]Clip, 0, len(names))

	for i, name := range names {
		path := filepath.Join(dir, name)

		size, err := sizeOf(path)
		if err != nil {
			return nil, err
		}

		measured, err := e.Probe(ctx, dir, path)
		if err != nil {
			return nil, err
		}

		clips = append(clips, Clip{
			Index:    i,
			Path:     path,
			Name:     name,
			Duration: measured.Duration,
			Bytes:    size,
		})
	}

	return clips, nil
}

// isClipName reports whether a file is one of the clips the pattern produces.
//
// It is matched by shape rather than by globbing, so an input called
// clip001.mp4 cannot be collected as its own output: the input is named by
// the caller and the pattern's names are always clip + three digits + .mp4.
func isClipName(name string) bool {
	const digits = 3

	if !strings.HasPrefix(name, "clip") || !strings.HasSuffix(name, ".mp4") {
		return false
	}

	middle := strings.TrimSuffix(strings.TrimPrefix(name, "clip"), ".mp4")
	if len(middle) != digits {
		return false
	}

	_, err := strconv.Atoi(middle)

	return err == nil
}

// plainName reports whether name is a bare file name: no directory, no
// traversal, no absolute path, and nothing a filesystem would read as a
// separator on either platform this builds for.
func plainName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}

	if strings.ContainsAny(name, `/\`) || filepath.IsAbs(name) {
		return false
	}

	return filepath.Base(name) == name
}

// sizeOf stats a file the engine was supposed to write.
func sizeOf(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, fail(ReasonEngine, "That video could not be edited.",
			"the engine wrote no output: "+err.Error(), err)
	}

	if info.Size() == 0 {
		return 0, fail(ReasonEngine, "That video could not be edited.",
			"the engine wrote an empty file", nil)
	}

	return info.Size(), nil
}
