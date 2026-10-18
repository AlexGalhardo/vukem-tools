package editor

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/AlexGalhardo/vukem-tools/internal/engine"
)

// Reason is why an edit failed, in this package's own vocabulary.
//
// As in internal/downloader, it is not the job error taxonomy: this package
// must be testable with a fake runner and nothing else, so it reports what it
// observed and app/tools maps that onto the taxonomy.
type Reason string

// The reasons an edit can fail.
const (
	// ReasonUnsupportedInput means ffprobe could not make sense of the file,
	// or it has no stream the edit needs.
	ReasonUnsupportedInput Reason = "unsupported_input"
	// ReasonOutOfRange means the request asks for something the stream does
	// not contain: a crop beyond the frame, or a start past the end.
	ReasonOutOfRange Reason = "out_of_range"
	// ReasonTooLong means the output would be longer than the limit.
	ReasonTooLong Reason = "too_long"
	// ReasonDiskFull means the working directory ran out of space.
	ReasonDiskFull Reason = "disk_full"
	// ReasonOutOfMemory means ffmpeg hit its memory limit.
	ReasonOutOfMemory Reason = "out_of_memory"
	// ReasonTimeout means ffmpeg was still running at its deadline.
	ReasonTimeout Reason = "timeout"
	// ReasonCancelled means the context was cancelled.
	ReasonCancelled Reason = "cancelled"
	// ReasonEngine means ffmpeg failed for a reason none of the above
	// explains.
	ReasonEngine Reason = "engine"
)

// EditError is an edit that did not happen.
type EditError struct {
	// Reason is the classification.
	Reason Reason
	// Public is the sentence to show whoever asked for the edit. For a
	// refusal it names the real numbers — the stream's dimensions, the
	// limit — because that is the only form of the message somebody can act
	// on.
	Public string
	// Detail is owner-only: it may hold ffmpeg's own message, which carries
	// the input path and the whole filter graph.
	Detail string
	// Err is the underlying error, if any.
	Err error
}

// Error implements error.
func (e *EditError) Error() string {
	switch {
	case e.Detail != "" && e.Err != nil:
		return string(e.Reason) + ": " + e.Detail + ": " + e.Err.Error()
	case e.Detail != "":
		return string(e.Reason) + ": " + e.Detail
	case e.Err != nil:
		return string(e.Reason) + ": " + e.Err.Error()
	default:
		return string(e.Reason)
	}
}

// Unwrap implements errors.Unwrap.
func (e *EditError) Unwrap() error { return e.Err }

// fail builds an EditError.
func fail(reason Reason, public, detail string, err error) *EditError {
	return &EditError{Reason: reason, Public: public, Detail: detail, Err: err}
}

// signature is one ffmpeg stderr fingerprint and what it means.
type signature struct {
	needles []string
	reason  Reason
	public  string
}

// signatures is the fingerprint table, most specific first. ffmpeg exits 1
// for all of these, so the exit code says nothing and the message is the only
// evidence there is.
var signatures = []signature{
	{
		needles: []string{"no space left on device", "disk quota exceeded"},
		reason:  ReasonDiskFull,
		public:  "The server is out of working space. Try again shortly.",
	},
	{
		needles: []string{"cannot allocate memory", "out of memory", "av_buffer_allocz"},
		reason:  ReasonOutOfMemory,
		public:  "That edit needed more memory than it is allowed.",
	},
	{
		needles: []string{
			"invalid data found when processing input", "moov atom not found",
			"does not contain any stream", "unknown format", "invalid argument",
			"decoder (codec none) not found",
		},
		reason: ReasonUnsupportedInput,
		public: "That file is not a video this tool can read.",
	},
	{
		needles: []string{"width and height must be", "invalid too big or non positive size", "crop"},
		reason:  ReasonOutOfRange,
		public:  "The area you asked to keep does not fit inside the video.",
	},
}

// Classify turns any error from an edit into an EditError.
//
// The deadline and the cancellation are read first: a killed ffmpeg also
// exits non-zero, and reporting that as "your file is broken" would send the
// user to look at a file that is fine.
func Classify(err error) *EditError {
	if err == nil {
		return nil
	}

	if already, ok := errors.AsType[*EditError](err); ok {
		return already
	}

	switch {
	case errors.Is(err, engine.ErrTimeout):
		return fail(ReasonTimeout, "That edit took too long and was stopped.",
			"ffmpeg exceeded its deadline", err)
	case errors.Is(err, context.Canceled):
		return fail(ReasonCancelled, "The edit was cancelled.", "the context was cancelled", err)
	}

	if exit, ok := errors.AsType[*engine.ExitError](err); ok {
		if f := classifyStderr(exit.Stderr, err); f != nil {
			return f
		}

		return fail(ReasonEngine, "That video could not be edited.",
			"ffmpeg exited "+strconv.Itoa(exit.Code), err)
	}

	return fail(ReasonEngine, "That video could not be edited.", "the editor failed", err)
}

// classifyStderr reads the reason out of what ffmpeg printed, or nil when
// nothing matched.
func classifyStderr(stderr string, err error) *EditError {
	lower := strings.ToLower(stderr)

	for _, s := range signatures {
		for _, needle := range s.needles {
			if strings.Contains(lower, needle) {
				return fail(s.reason, s.public, "ffmpeg: "+firstLine(stderr), err)
			}
		}
	}

	return nil
}

// firstLine is the part of an engine's error worth putting in a detail.
func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(line)
}
