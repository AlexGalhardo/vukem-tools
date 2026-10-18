package audio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/AlexGalhardo/vukem-tools/internal/engine"
	"github.com/AlexGalhardo/vukem-tools/internal/ffmpeg"
)

// Result is what a conversion produced.
type Result struct {
	// Path is the output file.
	Path string
	// Format is what was written.
	Format Format
	// Source and Output are the probes of the input and the result. The
	// output is read back from the file rather than predicted.
	Source ffmpeg.Media
	Output ffmpeg.Media
	// Loudness, when a two-pass normalisation ran, is what the first pass
	// measured. It is worth reporting: "this was 9 LU too quiet" is the
	// answer to "why does it sound different".
	Loudness *Loudness
	// CPUTime is the engine's processor time in milliseconds, MaxRSS its
	// peak resident memory in bytes.
	CPUTime int64
	MaxRSS  int64
}

// Convert re-encodes audio, optionally normalising its loudness.
//
// Extracting the soundtrack of a video is the same operation and the same
// code path: the input is probed, the video is dropped with -vn, and the
// audio stream is encoded. There is no separate "extract" because there is no
// separate work, and a second entry point would be a second place for the
// channel and sample-rate handling to drift.
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

	if !source.HasAudio() {
		return Result{}, ErrNoAudioStream
	}

	if source.Duration > MaxDuration {
		return Result{}, &DurationError{Duration: source.Duration, Limit: MaxDuration}
	}

	var (
		measured string
		loudness *Loudness
	)

	if o.Normalise && o.TwoPassNormalise {
		loudness, err = measureLoudness(ctx, e, r, dir, input)
		if err != nil {
			return Result{}, err
		}

		measured = loudness.filterArgs()
	}

	output := filepath.Join(dir, "output"+o.To.Extension())

	p, err := buildPlan(input, output, o, caps, measured)
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
		Path:     output,
		Format:   o.To,
		Source:   source,
		Output:   out,
		Loudness: loudness,
		CPUTime:  result.CPUTime.Milliseconds(),
		MaxRSS:   result.MaxRSS,
	}, nil
}

// Loudness is what the measurement pass found, in LUFS and LU.
type Loudness struct {
	// InputIntegrated is the measured loudness of the whole file.
	InputIntegrated float64 `json:"input_i"`
	// InputTruePeak is the highest true peak, in dBTP.
	InputTruePeak float64 `json:"input_tp"`
	// InputLRA is the loudness range.
	InputLRA float64 `json:"input_lra"`
	// InputThreshold is the gating threshold the measurement used.
	InputThreshold float64 `json:"input_thresh"`
	// TargetOffset is the correction the second pass will apply.
	TargetOffset float64 `json:"target_offset"`
}

// Correction is how far the file is from the target, in LU. Positive means it
// has to be made louder.
func (l Loudness) Correction() float64 { return LoudnessTarget - l.InputIntegrated }

// filterArgs renders the measurements as loudnorm's own options, which is how
// the second pass is told what the first one found.
func (l Loudness) filterArgs() string {
	return fmt.Sprintf("measured_I=%g:measured_TP=%g:measured_LRA=%g:measured_thresh=%g:offset=%g:linear=true",
		l.InputIntegrated, l.InputTruePeak, l.InputLRA, l.InputThreshold, l.TargetOffset)
}

// loudnormJSON is what ffmpeg's loudnorm prints: every field a string, as
// with ffprobe.
type loudnormJSON struct {
	InputI         string `json:"input_i"`
	InputTP        string `json:"input_tp"`
	InputLRA       string `json:"input_lra"`
	InputThreshold string `json:"input_thresh"`
	TargetOffset   string `json:"target_offset"`
}

// measureLoudness runs the first of the two loudness passes.
//
// It decodes the whole file and writes nothing: the output goes to the null
// muxer, so the pass costs a decode and no disc. The measurements come out on
// stderr as JSON, which is the only thing loudnorm offers — there is no
// structured stream for it — so stderr is read here rather than scraped for
// progress, which is a different thing and still forbidden.
func measureLoudness(
	ctx context.Context,
	e ffmpeg.Engine,
	r ffmpeg.Runner,
	dir, input string,
) (*Loudness, error) {
	args := append(ffmpeg.Base(),
		"-i", input,
		"-vn",
		"-map", "0:a:0",
		"-af", loudnormFilter(),
		"-f", "null",
	)
	args = append(args, ffmpeg.Threads()...)
	args = append(args, nullSink)

	// loudnorm prints its JSON at the info level, so the base arguments'
	// `-loglevel error` would hide the one thing this pass exists to
	// produce.
	args = withLogLevel(args, "info")

	result, err := r.Run(ctx, engine.Command{Path: e.FFmpeg, Args: args, Dir: dir, Env: ffmpeg.Env()})
	if err != nil {
		return nil, ffmpeg.Classify(err, "measure the loudness")
	}

	return parseLoudness(result.Stderr)
}

// nullSink is ffmpeg's discard output. The name is a platform detail: ffmpeg
// accepts "-" with the null muxer on every platform, which avoids /dev/null
// on Linux and NUL on Windows.
const nullSink = "-"

// withLogLevel replaces the log level in an argument list built by
// ffmpeg.Base.
//
// Replaced rather than appended: ffmpeg takes the last occurrence, so
// appending would work by accident, and an argument list with two
// -loglevel flags is one somebody will later "fix" into the wrong one.
func withLogLevel(args []string, level string) []string {
	out := slicesClone(args)

	for i := range out {
		if out[i] == "-loglevel" && i+1 < len(out) {
			out[i+1] = level

			return out
		}
	}

	return append(out, "-loglevel", level)
}

// slicesClone copies a slice so the caller's own arguments are not modified.
func slicesClone(in []string) []string {
	out := make([]string, len(in))
	copy(out, in)

	return out
}

// ErrNoMeasurement means loudnorm printed nothing this package could read,
// which is what a build without the filter looks like.
var ErrNoMeasurement = errors.New("audio: the loudness pass reported no measurement")

// parseLoudness pulls loudnorm's JSON block out of ffmpeg's stderr.
//
// The block is the last JSON object in the output: loudnorm prints it after
// everything else, and a file that triggered a warning would otherwise have
// the warning parsed instead.
func parseLoudness(stderr string) (*Loudness, error) {
	start := strings.LastIndex(stderr, "{")
	end := strings.LastIndex(stderr, "}")

	if start < 0 || end <= start {
		return nil, ErrNoMeasurement
	}

	var raw loudnormJSON
	if err := json.Unmarshal([]byte(stderr[start:end+1]), &raw); err != nil {
		return nil, fmt.Errorf("audio: read the loudness measurement: %w", err)
	}

	out := &Loudness{
		InputIntegrated: number(raw.InputI),
		InputTruePeak:   number(raw.InputTP),
		InputLRA:        number(raw.InputLRA),
		InputThreshold:  number(raw.InputThreshold),
		TargetOffset:    number(raw.TargetOffset),
	}

	// A file of pure silence measures as -inf, which loudnorm prints as
	// "-inf" and which no amount of gain will fix. Normalising it would
	// multiply nothing by infinity.
	if out.InputIntegrated == 0 && raw.InputI != "0" && raw.InputI != "0.0" {
		return nil, ErrNoMeasurement
	}

	return out, nil
}

// number reads one of loudnorm's string-encoded floats.
func number(raw string) float64 {
	var value float64

	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0
	}

	if _, err := fmt.Sscanf(trimmed, "%g", &value); err != nil {
		return 0
	}

	return value
}
