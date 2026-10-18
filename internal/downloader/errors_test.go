package downloader_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/AlexGalhardo/vukem-tools/internal/downloader"
	"github.com/AlexGalhardo/vukem-tools/internal/engine"
)

// stderrFixtures are yt-dlp's own messages, one per reason the ROADMAP names.
//
// They are kept as literal engine output rather than as the words this
// package matches on, so the test fails when a classification stops working
// rather than when a needle is reworded.
var stderrFixtures = []struct {
	name   string
	stderr string
	reason downloader.Reason
}{
	{
		name:   "age restricted",
		stderr: "ERROR: [youtube] abc: Sign in to confirm your age. This video may be inappropriate for some users.",
		reason: downloader.ReasonAgeRestricted,
	},
	{
		name:   "private",
		stderr: "ERROR: [youtube] abc: Private video. Sign in if you've been granted access to this video",
		reason: downloader.ReasonPrivate,
	},
	{
		name:   "private instagram account",
		stderr: "ERROR: [instagram] abc: This post is private",
		reason: downloader.ReasonPrivate,
	},
	{
		name:   "platform block, bot check",
		stderr: "ERROR: [youtube] abc: Sign in to confirm you're not a bot. Use --cookies-from-browser",
		reason: downloader.ReasonPlatformBlock,
	},
	{
		name:   "platform block, rate limited",
		stderr: "ERROR: unable to download video data: HTTP Error 429: Too Many Requests",
		reason: downloader.ReasonPlatformBlock,
	},
	{
		name:   "platform block, geography",
		stderr: "ERROR: [youtube] abc: The uploader has not made this video available in your country",
		reason: downloader.ReasonPlatformBlock,
	},
	{
		name:   "platform block, PO token",
		stderr: "WARNING: [youtube] abc: Some formats are missing a PO Token\nERROR: Failed to extract any player response",
		reason: downloader.ReasonPlatformBlock,
	},
	{
		name:   "unsupported url",
		stderr: "ERROR: Unsupported URL: https://example.com/whatever",
		reason: downloader.ReasonUnsupportedURL,
	},
	{
		name:   "unavailable",
		stderr: "ERROR: [youtube] abc: Video unavailable. This video has been removed by the uploader",
		reason: downloader.ReasonUnavailable,
	},
	{
		name:   "too large",
		stderr: "ERROR: File is larger than max-filesize (123456789 bytes > 10485760 bytes). Aborting.",
		reason: downloader.ReasonTooLarge,
	},
	{
		name:   "disk full",
		stderr: "ERROR: unable to write data: [Errno 28] No space left on device",
		reason: downloader.ReasonDiskFull,
	},
	{
		name:   "out of memory",
		stderr: "Traceback (most recent call last):\nMemoryError",
		reason: downloader.ReasonOutOfMemory,
	},
	{
		name:   "nothing recognisable",
		stderr: "ERROR: something nobody has seen before",
		reason: downloader.ReasonEngine,
	},
}

func TestEveryErrorTypeIsReachableFromAFixture(t *testing.T) {
	t.Parallel()

	for _, f := range stderrFixtures {
		t.Run(f.name, func(t *testing.T) {
			t.Parallel()

			fetchErr := downloader.Classify(&engine.ExitError{Code: 1, Stderr: f.stderr})

			if fetchErr == nil {
				t.Fatal("classified as success")
			}

			if fetchErr.Reason != f.reason {
				t.Fatalf("reason %q, want %q (stderr %q)", fetchErr.Reason, f.reason, f.stderr)
			}

			if fetchErr.Public == "" {
				t.Error("no sentence to show the user")
			}

			// The owner-only detail may hold the engine's message; the public
			// sentence must not, because it names the URL and the arguments.
			if strings.Contains(fetchErr.Public, "ERROR:") || strings.Contains(fetchErr.Public, "http") {
				t.Errorf("the public sentence leaks engine output: %q", fetchErr.Public)
			}

			if fetchErr.Detail == "" {
				t.Error("no owner-only detail")
			}
		})
	}
}

func TestEveryReasonTheRoadmapNamesHasAFixture(t *testing.T) {
	t.Parallel()

	// The ROADMAP lists the reasons by name: out of memory, disk full,
	// platform block, unsupported URL, age-restricted, private, too large.
	// This is the check that none of them quietly stops being reachable.
	required := []downloader.Reason{
		downloader.ReasonOutOfMemory,
		downloader.ReasonDiskFull,
		downloader.ReasonPlatformBlock,
		downloader.ReasonUnsupportedURL,
		downloader.ReasonAgeRestricted,
		downloader.ReasonPrivate,
		downloader.ReasonTooLarge,
	}

	covered := make(map[downloader.Reason]bool, len(stderrFixtures))
	for _, f := range stderrFixtures {
		covered[f.reason] = true
	}

	for _, reason := range required {
		if !covered[reason] {
			t.Errorf("%s is not reachable from any fixture", reason)
		}
	}
}

func TestATimeoutIsNotReportedAsAPlatformProblem(t *testing.T) {
	t.Parallel()

	fetchErr := downloader.Classify(engine.ErrTimeout)
	if fetchErr.Reason != downloader.ReasonTimeout {
		t.Fatalf("reason %q, want timeout", fetchErr.Reason)
	}

	fetchErr = downloader.Classify(context.Canceled)
	if fetchErr.Reason != downloader.ReasonCancelled {
		t.Fatalf("reason %q, want cancelled", fetchErr.Reason)
	}
}

func TestClassifyKeepsAFailureThisPackageAlreadyMade(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{}
	d := newDownloader(t, runner, downloader.Options{MaxBytes: 1024})

	_, err := d.Download(t.Context(), downloader.Request{
		URL:      "https://youtu.be/abc",
		Dir:      t.TempDir(),
		Estimate: 1 << 30,
	})

	fetchErr := downloader.Classify(err)
	if fetchErr.Reason != downloader.ReasonTooLarge {
		t.Fatalf("reason %q, want too_large", fetchErr.Reason)
	}

	// Wrapping must not lose it either.
	fetchErr = downloader.Classify(errors.Join(errors.New("context"), err))
	if fetchErr.Reason != downloader.ReasonTooLarge {
		t.Fatalf("wrapped reason %q, want too_large", fetchErr.Reason)
	}
}

func TestClassifyOfNothingIsNothing(t *testing.T) {
	t.Parallel()

	if downloader.Classify(nil) != nil {
		t.Error("a successful download was classified as a fetchErr")
	}
}

func TestAFailureUnwrapsToTheEngineError(t *testing.T) {
	t.Parallel()

	exit := &engine.ExitError{Code: 1, Stderr: "ERROR: Private video"}

	fetchErr := downloader.Classify(exit)
	if !errors.Is(fetchErr, exit) {
		t.Error("the engine error is not reachable through the fetchErr")
	}

	if !strings.Contains(fetchErr.Error(), "private") {
		t.Errorf("Error() does not name the reason: %q", fetchErr.Error())
	}
}
