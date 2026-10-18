package editor_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/AlexGalhardo/vukem-tools/internal/editor"
	"github.com/AlexGalhardo/vukem-tools/internal/engine"
)

func TestFFmpegFailuresAreClassifiedFromFixtures(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		stderr string
		reason editor.Reason
	}{
		{
			name:   "disk full",
			stderr: "av_interleaved_write_frame(): No space left on device",
			reason: editor.ReasonDiskFull,
		},
		{
			name:   "out of memory",
			stderr: "Cannot allocate memory",
			reason: editor.ReasonOutOfMemory,
		},
		{
			name:   "a broken container",
			stderr: "input.mp4: Invalid data found when processing input",
			reason: editor.ReasonUnsupportedInput,
		},
		{
			name:   "a truncated mp4",
			stderr: "[mov,mp4,m4a,3gp,3g2,mj2 @ 0x1] moov atom not found",
			reason: editor.ReasonUnsupportedInput,
		},
		{
			name:   "a crop ffmpeg itself refused",
			stderr: "[Parsed_crop_0 @ 0x1] Invalid too big or non positive size for width '3000' or height '2000'",
			reason: editor.ReasonOutOfRange,
		},
		{
			name:   "nothing recognisable",
			stderr: "Conversion failed for a reason nobody has written down",
			reason: editor.ReasonEngine,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			got := editor.Classify(&engine.ExitError{Code: 1, Stderr: c.stderr})

			if got == nil {
				t.Fatal("classified as success")
			}

			if got.Reason != c.reason {
				t.Fatalf("reason %q, want %q (stderr %q)", got.Reason, c.reason, c.stderr)
			}

			if got.Public == "" {
				t.Error("no sentence to show the user")
			}

			// The public sentence must not carry the engine's own text: it
			// names the input path and the filter graph.
			if strings.Contains(got.Public, "0x1") || strings.Contains(got.Public, ".mp4") {
				t.Errorf("the public sentence leaks engine output: %q", got.Public)
			}
		})
	}
}

func TestATimeoutIsNotReportedAsABrokenFile(t *testing.T) {
	t.Parallel()

	if got := editor.Classify(engine.ErrTimeout); got.Reason != editor.ReasonTimeout {
		t.Errorf("reason %q, want timeout", got.Reason)
	}

	if got := editor.Classify(context.Canceled); got.Reason != editor.ReasonCancelled {
		t.Errorf("reason %q, want cancelled", got.Reason)
	}

	if editor.Classify(nil) != nil {
		t.Error("a successful edit was classified as a failure")
	}
}

func TestAnEditErrorUnwrapsToTheEngineError(t *testing.T) {
	t.Parallel()

	exit := &engine.ExitError{Code: 1, Stderr: "moov atom not found"}

	got := editor.Classify(exit)
	if !errors.Is(got, exit) {
		t.Error("the engine error is not reachable through the EditError")
	}

	if !strings.Contains(got.Error(), "unsupported_input") {
		t.Errorf("Error() does not name the reason: %q", got.Error())
	}
}
