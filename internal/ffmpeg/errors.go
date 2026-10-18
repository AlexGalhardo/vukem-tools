package ffmpeg

import (
	"errors"
	"fmt"
	"strings"

	"github.com/AlexGalhardo/vukem-tools/internal/engine"
)

// ErrorKind is why ffmpeg failed, as far as its output can say.
//
// These exist to be mapped onto the job error taxonomy by the tool layer:
// this package does not import internal/jobs, so a conversion can be
// benchmarked without a queue.
type ErrorKind string

// The kinds.
const (
	// KindUnreadable means ffmpeg could not decode the input.
	KindUnreadable ErrorKind = "unreadable"
	// KindUnsupported means an encoder, a container or an option is not in
	// this build.
	KindUnsupported ErrorKind = "unsupported"
	// KindOutOfMemory means an allocation failed, which under RLIMIT_AS is
	// what an absurd resolution looks like.
	KindOutOfMemory ErrorKind = "out_of_memory"
	// KindDiskFull means a write failed for want of space, including
	// RLIMIT_FSIZE stopping a runaway output.
	KindDiskFull ErrorKind = "disk_full"
	// KindTimeout means the engine was killed at its deadline.
	KindTimeout ErrorKind = "timeout"
	// KindResourceLimit means the engine could not get a resource the
	// server's own limits withhold — in practice a thread, when an encoder
	// builds a pool sized from the host's core count and runs into
	// RLIMIT_NPROC.
	//
	// It is separate from KindOutOfMemory on purpose, and the separation is
	// not cosmetic: SVT-AV1 reports this after a banner that mentions
	// memory, so it reads as an out-of-memory failure, and treating it as
	// one would retry a conversion that can never succeed on a worker
	// configured this way and would tell the user their file was too big.
	// It is the server's configuration, so it must not be blamed on the
	// file.
	KindResourceLimit ErrorKind = "resource_limit"
	// KindOther is a failure the output does not explain.
	KindOther ErrorKind = "other"
)

// EngineError is an ffmpeg failure with the engine's own message kept for the
// owner.
//
// Stderr is on the error rather than in its message because ffmpeg's errors
// routinely carry the whole command line, the absolute input path and the
// working directory, and none of that belongs in an API response.
type EngineError struct {
	// Doing is what was being attempted, in the owner's words.
	Doing string
	// Stderr is the tail of what ffmpeg printed.
	Stderr string
	Kind   ErrorKind
	Err    error
}

// Error implements error.
func (e *EngineError) Error() string {
	if e.Stderr == "" {
		return fmt.Sprintf("ffmpeg: %s: %s", e.Doing, e.Kind)
	}

	return fmt.Sprintf("ffmpeg: %s: %s: %s", e.Doing, e.Kind, firstLine(e.Stderr))
}

// Unwrap implements errors.Unwrap.
func (e *EngineError) Unwrap() error { return e.Err }

// Classify turns a runner error into an EngineError with a kind.
//
// The kind comes from ffmpeg's own words, because the exit code does not
// distinguish them: ffmpeg exits 1 for a missing file, a corrupt stream, an
// unknown encoder and a failed allocation alike. So the patterns are kept
// narrow, and anything unmatched stays KindOther rather than being guessed
// into a user-facing "your file is broken".
func Classify(err error, doing string) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, engine.ErrTimeout) {
		return &EngineError{Doing: doing, Kind: KindTimeout, Err: err}
	}

	var exit *engine.ExitError
	if !errors.As(err, &exit) {
		return &EngineError{Doing: doing, Kind: KindOther, Err: err}
	}

	return &EngineError{Doing: doing, Stderr: exit.Stderr, Kind: kindOf(exit.Stderr), Err: err}
}

// kindOf reads ffmpeg's stderr for a cause.
func kindOf(stderr string) ErrorKind {
	lower := strings.ToLower(stderr)

	switch {
	// Checked before the memory patterns, because an encoder that cannot
	// create a thread often prints both and this is the real cause.
	case containsAny(lower, "failed to create thread", "resource temporarily unavailable",
		"cannot create thread", "unable to create thread"):
		return KindResourceLimit
	case containsAny(lower, "cannot allocate memory", "out of memory", "failed to allocate"):
		return KindOutOfMemory
	case containsAny(lower, "no space left", "file too large", "write error", "error writing trailer"):
		return KindDiskFull
	case containsAny(lower, "unknown encoder", "unknown decoder", "encoder not found",
		"requested output format", "unable to find a suitable output format",
		"codec not currently supported in container", "unrecognized option",
		"option not found", "protocol not found"):
		return KindUnsupported
	case containsAny(lower, "invalid data found", "moov atom not found", "could not find codec parameters",
		"end of file", "invalid argument", "error opening input", "no such file",
		"does not contain any stream", "header missing", "truncat"):
		return KindUnreadable
	default:
		return KindOther
	}
}

// containsAny reports whether s holds any of the needles.
func containsAny(s string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(s, n) {
			return true
		}
	}

	return false
}

// firstLine is the part of an engine's error worth putting in a message.
func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(line)
}

// MissingEncoderError reports an encoder this build does not have.
//
// It distinguishes the GPL case because that is the one with an answer: an
// LGPL build has no x264 and no x265, and telling somebody "H.264 is not
// available on this build, which is not built with GPL support" is something
// they can act on, while "Unknown encoder 'libx264'" is not.
type MissingEncoderError struct {
	// Encoder is the ffmpeg encoder name, e.g. "libx264".
	Encoder string
	// Codec is what a person calls it, e.g. "H.264".
	Codec string
	// GPLOnly marks an encoder that only exists in a GPL build.
	GPLOnly bool
	// BuildIsGPL is what this build actually is, so the message can tell
	// "the engine is too old" from "the engine was built without GPL".
	BuildIsGPL bool
}

// Error implements error.
func (e *MissingEncoderError) Error() string {
	return fmt.Sprintf("ffmpeg: this build has no %s encoder (gpl-only=%v, build-is-gpl=%v)",
		e.Encoder, e.GPLOnly, e.BuildIsGPL)
}

// Message is the sentence to show the user.
func (e *MissingEncoderError) Message() string {
	if e.GPLOnly && !e.BuildIsGPL {
		return e.Codec + " is not available: this server's video engine is not built with GPL " +
			"support, which " + e.Codec + " requires. Choose VP9 or AV1 instead."
	}

	return e.Codec + " is not available on this server's video engine. Choose another format."
}
