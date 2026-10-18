package editor

import (
	"strconv"
	"strings"
	"time"
)

// Progress is how far along an edit is.
type Progress struct {
	// OutTime is how much output ffmpeg has written.
	OutTime time.Duration
	// Percent is OutTime against the expected output duration, or -1 when
	// that duration is unknown.
	Percent int
	// Done is set on the last update, when ffmpeg reports progress=end.
	Done bool
}

// ParseProgress reads one line of ffmpeg's `-progress pipe:1` stream.
//
// The progress stream is key=value lines, one block per update, ending with
// `progress=continue` or `progress=end`. It is read instead of stderr because
// stderr is a status line redrawn with carriage returns whose format has
// changed between ffmpeg releases, and because `-nostats` switches it off
// entirely — which is what this package does.
//
// Only the two keys that matter produce an update: out_time_us for a
// position, progress=end for the finish. Everything else — bitrate, fps,
// speed, frame — is deliberately ignored rather than carried around.
func ParseProgress(line string) (Progress, bool) {
	key, value, found := strings.Cut(strings.TrimSpace(line), "=")
	if !found {
		return Progress{}, false
	}

	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)

	switch key {
	case "out_time_us", "out_time_ms":
		// Both keys are microseconds. out_time_ms is a long-standing
		// misnomer in ffmpeg: it has always held microseconds, and reading it
		// as milliseconds puts the progress bar a thousand times too far
		// along.
		micros, err := strconv.ParseInt(value, 10, 64)
		if err != nil || micros < 0 {
			return Progress{}, false
		}

		return Progress{OutTime: time.Duration(micros) * time.Microsecond, Percent: -1}, true
	case "progress":
		if value != "end" {
			return Progress{}, false
		}

		return Progress{Percent: 100, Done: true}, true
	default:
		return Progress{}, false
	}
}

// progressReader returns the line handler for engine.Command.Progress, or nil
// when nobody is listening.
//
// total is the expected output duration, which is what turns a position into
// a percentage. Zero means unknown, and the percentage is then -1 rather than
// a number nobody should believe.
func progressReader(total time.Duration, report func(Progress)) func(string) {
	if report == nil {
		return nil
	}

	return func(line string) {
		p, ok := ParseProgress(line)
		if !ok {
			return
		}

		if !p.Done && total > 0 {
			p.Percent = min(max(int(float64(p.OutTime)/float64(total)*100), 0), 100)
		}

		report(p)
	}
}
