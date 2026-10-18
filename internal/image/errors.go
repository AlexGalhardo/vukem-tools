package image

import (
	"errors"
	"fmt"
	"strings"

	"github.com/AlexGalhardo/vukem-tools/internal/engine"
)

// EngineError is a libvips failure, with the engine's own message kept for
// the owner.
//
// Stderr is on the error rather than in its message so a caller decides what
// reaches a user: libvips prints the whole command line and the absolute path
// of the working directory in several of its errors, and neither belongs in
// an API response.
type EngineError struct {
	// Doing is what was being attempted, in the owner's words.
	Doing string
	// Stderr is the tail of what libvips printed.
	Stderr string
	// Kind classifies the failure from that output.
	Kind EngineErrorKind
	Err  error
}

// EngineErrorKind is why libvips failed, as far as its output can say.
type EngineErrorKind string

// The kinds. They exist to be mapped onto the job error taxonomy by the tool
// layer: this package does not import internal/jobs, so that a conversion can
// be benchmarked without a queue.
const (
	// KindUnreadable means libvips could not decode the input.
	KindUnreadable EngineErrorKind = "unreadable"
	// KindUnsupported means the format or an option is not in this build.
	KindUnsupported EngineErrorKind = "unsupported"
	// KindOutOfMemory means an allocation failed, which under RLIMIT_AS is
	// what a decompression bomb looks like.
	KindOutOfMemory EngineErrorKind = "out_of_memory"
	// KindDiskFull means a write failed for want of space, including
	// RLIMIT_FSIZE stopping a runaway output.
	KindDiskFull EngineErrorKind = "disk_full"
	// KindTimeout means the engine was killed at its deadline.
	KindTimeout EngineErrorKind = "timeout"
	// KindOther is a failure the output does not explain.
	KindOther EngineErrorKind = "other"
)

// Error implements error.
func (e *EngineError) Error() string {
	if e.Stderr == "" {
		return fmt.Sprintf("image: %s: %s", e.Doing, e.Kind)
	}

	return fmt.Sprintf("image: %s: %s: %s", e.Doing, e.Kind, firstLine(e.Stderr))
}

// Unwrap implements errors.Unwrap.
func (e *EngineError) Unwrap() error { return e.Err }

// classify turns a runner error into an EngineError with a kind.
//
// The kind comes from libvips's own words, because the exit code does not
// distinguish them: libvips exits 1 for a missing file, an unsupported
// format, a failed allocation and a corrupt JPEG alike. Matching the message
// is the only signal there is, so the patterns are kept narrow and anything
// unmatched stays KindOther rather than being guessed into a user-facing
// "your file is broken".
func classify(err error, doing string) error {
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

	return &EngineError{
		Doing:  doing,
		Stderr: exit.Stderr,
		Kind:   kindOf(exit.Stderr),
		Err:    err,
	}
}

// kindOf reads libvips's stderr for a cause.
func kindOf(stderr string) EngineErrorKind {
	lower := strings.ToLower(stderr)

	switch {
	case containsAny(lower, "out of memory", "cannot allocate", "insufficient memory", "vips_malloc"):
		return KindOutOfMemory
	case containsAny(lower, "no space left", "file too large", "disk full", "write error"):
		return KindDiskFull
	case containsAny(lower, "is not a known file format", "no known loader", "unable to load",
		"unsupported", "not a supported", "no such operation", "unknown field"):
		return KindUnsupported
	case containsAny(lower, "bad extension", "premature end", "corrupt", "not a jpeg", "invalid",
		"truncated", "error reading", "bogus", "unable to read"):
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
