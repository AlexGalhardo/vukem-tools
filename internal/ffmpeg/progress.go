package ffmpeg

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AlexGalhardo/vukem-tools/internal/engine"
)

// Progress is one report from a running conversion.
type Progress struct {
	// Percent is 0 to 100, computed from the output time against the input's
	// duration. It is -1 when the duration is unknown, so a caller can show
	// "working" rather than a made-up number.
	Percent int
	// OutTime is how far into the output the encoder has reached.
	OutTime time.Duration
	// Frame is the frame count, zero for audio.
	Frame int64
	// Speed is ffmpeg's own figure, e.g. "1.85x". It is useful in a log: a
	// speed well under 1x on a short file means the preset is wrong.
	Speed string
	// Done is set on the final report, which ffmpeg marks with
	// "progress=end".
	Done bool
}

// Run executes ffmpeg and reports progress from its structured stream.
//
// The progress comes from `-progress pipe:1`, which prints key=value lines
// and ends each block with "progress=continue" or "progress=end". That is the
// documented machine-readable stream; ffmpeg's stderr stats line is for
// humans, is not stable between versions, and is what the engine rules
// forbid scraping.
//
// report may return an error to stop the conversion — the owner cancelled it.
// That error cannot be returned from inside the stream reader, so it is kept
// and the process is cancelled, which kills ffmpeg's whole process group; Run
// then returns that error rather than the kill's own "signal: killed".
func Run(
	ctx context.Context,
	e Engine,
	r Runner,
	dir string,
	args []string,
	total time.Duration,
	report func(Progress) error,
) (engine.Result, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		mu       sync.Mutex
		reported error
		state    progressState
	)

	cmd := engine.Command{
		Path: e.FFmpeg,
		Args: args,
		Dir:  dir,
		Env:  Env(),
	}

	if report != nil {
		cmd.Progress = func(line string) {
			update, ok := state.feed(line, total)
			if !ok {
				return
			}

			mu.Lock()
			defer mu.Unlock()

			if reported != nil {
				return
			}

			if err := report(update); err != nil {
				reported = err

				// Stop the engine now rather than at the next block: a
				// cancelled conversion of a long video would otherwise keep
				// a worker and a CPU for minutes after nobody wanted the
				// result.
				cancel()
			}
		}
	}

	result, err := r.Run(ctx, cmd)

	mu.Lock()
	defer mu.Unlock()

	// The caller's own error wins over the kill it caused. Returning
	// "killed" instead would classify an owner's cancellation as an engine
	// failure.
	if reported != nil {
		return result, reported
	}

	if err != nil {
		return result, Classify(err, "run the conversion")
	}

	return result, nil
}

// progressState accumulates one block of ffmpeg's progress output.
//
// ffmpeg prints a block of key=value lines and then a "progress=" line that
// closes it. Reporting per line would send five reports per block and four of
// them incomplete, so a report is emitted only when a block closes.
type progressState struct {
	outTime time.Duration
	frame   int64
	speed   string
}

// feed consumes one line and reports whether a complete block just closed.
func (s *progressState) feed(line string, total time.Duration) (Progress, bool) {
	key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
	if !ok {
		return Progress{}, false
	}

	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)

	switch key {
	case "out_time_us", "out_time_ms":
		// Both keys are microseconds. out_time_ms is misnamed in ffmpeg and
		// has been for years; reading it as milliseconds gives a progress
		// percentage a thousand times too small, which looks like a stuck
		// conversion.
		if micros, err := strconv.ParseInt(value, 10, 64); err == nil && micros >= 0 {
			s.outTime = time.Duration(micros) * time.Microsecond
		}

		return Progress{}, false
	case "frame":
		if frames, err := strconv.ParseInt(value, 10, 64); err == nil {
			s.frame = frames
		}

		return Progress{}, false
	case "speed":
		s.speed = value

		return Progress{}, false
	case "progress":
		return Progress{
			Percent: percentOf(s.outTime, total),
			OutTime: s.outTime,
			Frame:   s.frame,
			Speed:   s.speed,
			Done:    value == "end",
		}, true
	default:
		return Progress{}, false
	}
}

// percentOf computes the percentage, or -1 when there is no total.
//
// Clamped to 99 while running rather than allowed to reach 100: ffmpeg's
// out_time can pass the container's reported duration by a frame or two, and
// a progress bar that reaches 100 and then keeps going is worse than one that
// waits.
func percentOf(done, total time.Duration) int {
	if total <= 0 {
		return -1
	}

	percent := int(done * 100 / total)

	return min(max(percent, 0), 99)
}
