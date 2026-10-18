package compressor

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/AlexGalhardo/vukem-tools/internal/engine"
)

// Reason is why a compression failed, in this package's own vocabulary.
type Reason string

// The reasons a compression can fail.
const (
	// ReasonUnsupportedInput means the engine could not read the file.
	ReasonUnsupportedInput Reason = "unsupported_input"
	// ReasonEncrypted means a PDF is password-protected, which Ghostscript
	// will not rewrite.
	ReasonEncrypted Reason = "encrypted"
	// ReasonDiskFull means the working directory ran out of space.
	ReasonDiskFull Reason = "disk_full"
	// ReasonOutOfMemory means the engine hit its memory limit.
	ReasonOutOfMemory Reason = "out_of_memory"
	// ReasonTimeout means the engine was still running at its deadline.
	ReasonTimeout Reason = "timeout"
	// ReasonCancelled means the context was cancelled.
	ReasonCancelled Reason = "cancelled"
	// ReasonEngine means the engine failed for a reason none of the above
	// explains.
	ReasonEngine Reason = "engine"
)

// CompressError is a compression that did not happen.
type CompressError struct {
	// Reason is the classification.
	Reason Reason
	// Public is the sentence to show whoever asked for the compression.
	Public string
	// Detail is owner-only: it may hold the engine's own message, which
	// carries the input path and the whole command line.
	Detail string
	// Err is the underlying error, if any.
	Err error
}

// Error implements error.
func (e *CompressError) Error() string {
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
func (e *CompressError) Unwrap() error { return e.Err }

// fail builds a CompressError.
func fail(reason Reason, public, detail string, err error) *CompressError {
	return &CompressError{Reason: reason, Public: public, Detail: detail, Err: err}
}

// signature is one stderr fingerprint and what it means.
type signature struct {
	needles []string
	reason  Reason
	public  string
}

// signatures covers both engines, because both exit 1 for everything and the
// message is the only evidence there is. Ghostscript's wording is nothing
// like ffmpeg's, which is why the table is here rather than shared with
// internal/editor.
var signatures = []signature{
	{
		needles: []string{"no space left on device", "disk quota exceeded"},
		reason:  ReasonDiskFull,
		public:  "The server is out of working space. Try again shortly.",
	},
	{
		needles: []string{"cannot allocate memory", "out of memory", "vmerror", "limitcheck"},
		reason:  ReasonOutOfMemory,
		public:  "That file needed more memory to compress than it is allowed.",
	},
	{
		needles: []string{"this file requires a password", "invalidfileaccess", "password"},
		reason:  ReasonEncrypted,
		public:  "That PDF is password-protected, so it cannot be compressed.",
	},
	{
		needles: []string{
			"invalid data found when processing input", "moov atom not found",
			"unknown format", "does not contain any stream",
			"/undefined in", "syntaxerror", "unrecoverable error",
			"not a pdf file", "could not open the file",
		},
		reason: ReasonUnsupportedInput,
		public: "That file is not one this tool can compress.",
	},
}

// Classify turns any error from a compression into a CompressError.
func Classify(err error) *CompressError {
	if err == nil {
		return nil
	}

	if already, ok := errors.AsType[*CompressError](err); ok {
		return already
	}

	switch {
	case errors.Is(err, engine.ErrTimeout):
		return fail(ReasonTimeout, "Compressing that file took too long and was stopped.",
			"the engine exceeded its deadline", err)
	case errors.Is(err, context.Canceled):
		return fail(ReasonCancelled, "The compression was cancelled.", "the context was cancelled", err)
	}

	if exit, ok := errors.AsType[*engine.ExitError](err); ok {
		if f := classifyStderr(exit.Stderr, err); f != nil {
			return f
		}

		return fail(ReasonEngine, "That file could not be compressed.",
			"the engine exited "+strconv.Itoa(exit.Code), err)
	}

	return fail(ReasonEngine, "That file could not be compressed.", "the compressor failed", err)
}

// classifyStderr reads the reason out of what the engine printed, or nil when
// nothing matched.
func classifyStderr(stderr string, err error) *CompressError {
	lower := strings.ToLower(stderr)

	for _, s := range signatures {
		for _, needle := range s.needles {
			if strings.Contains(lower, needle) {
				return fail(s.reason, s.public, "engine: "+firstLine(stderr), err)
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
