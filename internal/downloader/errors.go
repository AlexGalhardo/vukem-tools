package downloader

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/AlexGalhardo/vukem-tools/internal/engine"
)

// Reason is why a download failed, in the downloader's own vocabulary.
//
// It is deliberately not the job error taxonomy. This package knows nothing
// about jobs, conversions or HTTP — it has to be testable with nothing but a
// fake runner — so it reports what it observed and app/tools/download.go maps
// that onto the taxonomy. Keeping the two apart is also what makes "private
// video" and "age-restricted" distinguishable at all: both end up as
// `unsupported_input` in the taxonomy, and the sentence the user reads is the
// only place the difference survives.
type Reason string

// The reasons a download can fail.
const (
	// ReasonUnsupportedURL means the URL is not one of the four platforms,
	// or yt-dlp has no extractor for it.
	ReasonUnsupportedURL Reason = "unsupported_url"
	// ReasonPrivate means the video exists but is not public.
	ReasonPrivate Reason = "private"
	// ReasonAgeRestricted means the platform wants a signed-in adult.
	ReasonAgeRestricted Reason = "age_restricted"
	// ReasonPlatformBlock means the platform refused this server: a bot
	// check, a rate limit, a geographic block or a required PO token.
	ReasonPlatformBlock Reason = "platform_block"
	// ReasonUnavailable means the video is gone, removed or never existed.
	ReasonUnavailable Reason = "unavailable"
	// ReasonTooLarge means the download is past the size cap, from the
	// estimate or from the finished file.
	ReasonTooLarge Reason = "too_large"
	// ReasonDiskFull means the working directory ran out of space.
	ReasonDiskFull Reason = "disk_full"
	// ReasonOutOfMemory means yt-dlp or the ffmpeg it drives hit its memory
	// limit.
	ReasonOutOfMemory Reason = "out_of_memory"
	// ReasonTimeout means the download was still running at its deadline.
	ReasonTimeout Reason = "timeout"
	// ReasonCancelled means the context was cancelled.
	ReasonCancelled Reason = "cancelled"
	// ReasonEngine means yt-dlp failed for a reason none of the above
	// explains.
	ReasonEngine Reason = "engine"
)

// FetchError is a download that did not happen, with the reason and the
// owner-only detail.
type FetchError struct {
	// Reason is the classification.
	Reason Reason
	// Public is the sentence to show whoever asked for the download. It
	// names the platform and the problem and nothing else: no path, no
	// argument, no stderr.
	Public string
	// Detail is owner-only. It may hold the engine's own message, which
	// routinely contains the URL and the whole command line.
	Detail string
	// Err is the underlying error, if any.
	Err error
}

// Error implements error. It reports the reason and the owner-only detail, so
// a logged failure says which kind it was.
func (f *FetchError) Error() string {
	switch {
	case f.Detail != "" && f.Err != nil:
		return string(f.Reason) + ": " + f.Detail + ": " + f.Err.Error()
	case f.Detail != "":
		return string(f.Reason) + ": " + f.Detail
	case f.Err != nil:
		return string(f.Reason) + ": " + f.Err.Error()
	default:
		return string(f.Reason)
	}
}

// Unwrap implements errors.Unwrap.
func (f *FetchError) Unwrap() error { return f.Err }

// fail builds a FetchError.
func fail(reason Reason, public, detail string, err error) *FetchError {
	return &FetchError{Reason: reason, Public: public, Detail: detail, Err: err}
}

// signature is one stderr fingerprint and what it means.
//
// Matching on the engine's message is unavoidable: yt-dlp exits 1 for every
// one of these, so the exit code says nothing. The order matters — the first
// match wins — and the specific causes come before the general ones, because
// "Sign in to confirm your age" also contains "Sign in", which is the bot
// check's phrase.
type signature struct {
	needles []string
	reason  Reason
	public  string
}

// signatures is the fingerprint table, most specific first.
//
// Every entry's needles were taken from yt-dlp's own error strings. They are
// lowercased before comparison, so a platform changing the capitalisation of
// its message does not silently drop a classification into ReasonEngine.
var signatures = []signature{
	{
		needles: []string{
			"sign in to confirm your age",
			"age-restricted",
			"age restricted",
			"inappropriate for some users",
		},
		reason: ReasonAgeRestricted,
		public: "That video is age-restricted, so it can only be downloaded by a signed-in account.",
	},
	{
		needles: []string{"private video", "this video is private", "this post is private", "is a private account"},
		reason:  ReasonPrivate,
		public:  "That video is private, so it cannot be downloaded.",
	},
	{
		needles: []string{
			"sign in to confirm you're not a bot", "sign in to confirm you are not a bot",
			"http error 429", "too many requests", "rate-limit", "rate limit",
			"available in your country", "blocked it in your country", "geo restricted",
			"video is not available from your location",
			"http error 403", "po token", "failed to extract any player response",
			"login required", "requires authentication",
		},
		reason: ReasonPlatformBlock,
		public: "The platform refused this server. That usually clears by itself; try again later.",
	},
	{
		needles: []string{"unsupported url", "no suitable extractor", "is not a valid url"},
		reason:  ReasonUnsupportedURL,
		public:  "That URL is not one this service can download from.",
	},
	{
		needles: []string{
			"video unavailable", "this video is no longer available", "removed by the uploader",
			"has been terminated", "does not exist", "not found", "http error 404", "http error 410",
		},
		reason: ReasonUnavailable,
		public: "That video is not available any more.",
	},
	{
		needles: []string{"larger than max-filesize", "file is larger than max", "max-filesize"},
		reason:  ReasonTooLarge,
		public:  "That video is larger than this service will download.",
	},
	{
		needles: []string{"no space left on device", "disk quota exceeded"},
		reason:  ReasonDiskFull,
		public:  "The server is out of working space. Try again shortly.",
	},
	{
		needles: []string{"memoryerror", "cannot allocate memory", "out of memory"},
		reason:  ReasonOutOfMemory,
		public:  "That download needed more memory than it is allowed.",
	},
}

// Classify turns any error from a download into a FetchError.
//
// A FetchError passes straight through, so a refusal this package made for
// itself — the size cap from the estimate, an unsupported URL — keeps the
// sentence it chose. Everything else is read off the engine: the deadline and
// the cancellation first, because a killed program also exits non-zero and
// reporting that as "the platform refused us" would send the user looking in
// the wrong place.
func Classify(err error) *FetchError {
	if err == nil {
		return nil
	}

	if already, ok := errors.AsType[*FetchError](err); ok {
		return already
	}

	switch {
	case errors.Is(err, engine.ErrTimeout):
		return fail(ReasonTimeout, "That download took too long and was stopped.", "yt-dlp exceeded its deadline", err)
	case errors.Is(err, context.Canceled):
		return fail(ReasonCancelled, "The download was cancelled.", "the context was cancelled", err)
	}

	if exit, ok := errors.AsType[*engine.ExitError](err); ok {
		if f := classifyStderr(exit.Stderr, err); f != nil {
			return f
		}

		return fail(ReasonEngine, "That video could not be downloaded.",
			"yt-dlp exited "+strconv.Itoa(exit.Code), err)
	}

	return fail(ReasonEngine, "That video could not be downloaded.", "the downloader failed", err)
}

// classifyStderr reads the reason out of what yt-dlp printed, or reports nil
// when nothing matched.
func classifyStderr(stderr string, err error) *FetchError {
	lower := strings.ToLower(stderr)

	for _, s := range signatures {
		for _, needle := range s.needles {
			if strings.Contains(lower, needle) {
				return fail(s.reason, s.public, "yt-dlp: "+firstLine(stderr), err)
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
