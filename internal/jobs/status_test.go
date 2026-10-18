package jobs_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/AlexGalhardo/vukem-tools/internal/jobs"
)

// Every code the migration's enum allows must be a Status the code knows,
// and vice versa: a row the database accepts but the code cannot read is a
// conversion nobody can render.
func TestStatusValidCoversTheDatabaseEnum(t *testing.T) {
	t.Parallel()

	// The enum in database/migrations/..._create_conversions_tables.go.
	enum := []jobs.Status{"pending", "running", "finished", "error", "cancelled"}

	for _, s := range enum {
		if !s.Valid() {
			t.Errorf("status %q is allowed by the database but not by Status.Valid", s)
		}
	}

	if jobs.Status("paused").Valid() {
		t.Error("Status.Valid accepted a status the database would reject")
	}
}

func TestStatusFinal(t *testing.T) {
	t.Parallel()

	final := map[jobs.Status]bool{
		jobs.Pending:   false,
		jobs.Running:   false,
		jobs.Finished:  true,
		jobs.Errored:   true,
		jobs.Cancelled: true,
	}

	for s, want := range final {
		if got := s.Final(); got != want {
			t.Errorf("%s.Final() = %v, want %v", s, got, want)
		}
	}
}

// The taxonomy is a contract: nine codes, each with a client-safe message
// and a real HTTP status. A new code must be added here deliberately, which
// is the point of asserting the count.
func TestCatalogIsComplete(t *testing.T) {
	t.Parallel()

	want := []jobs.ErrorType{
		jobs.UnsupportedInput,
		jobs.InputTooLarge,
		jobs.QuotaExceeded,
		jobs.CancelledByUser,
		jobs.EngineFailure,
		jobs.Timeout,
		jobs.OutOfMemory,
		jobs.DiskFull,
		jobs.Internal,
	}

	got := jobs.Catalog()
	if len(got) != len(want) {
		t.Fatalf("catalog has %d entries, want %d", len(got), len(want))
	}

	for i, info := range got {
		if info.Type != want[i] {
			t.Errorf("catalog[%d] = %q, want %q", i, info.Type, want[i])
		}

		if info.Message == "" {
			t.Errorf("%s has no client-safe message", info.Type)
		}

		if !strings.HasSuffix(info.Message, ".") {
			t.Errorf("%s message is not a sentence: %q", info.Type, info.Message)
		}

		if info.Status < 400 || info.Status > 599 {
			t.Errorf("%s has HTTP status %d, want a 4xx or 5xx", info.Type, info.Status)
		}
	}
}

// Catalog hands out a copy. A caller that sorts or truncates the returned
// slice must not change the taxonomy for everybody else.
func TestCatalogCannotBeMutatedByCallers(t *testing.T) {
	t.Parallel()

	first := jobs.Catalog()
	first[0] = jobs.Info{Type: "tampered"}

	if jobs.Catalog()[0].Type == "tampered" {
		t.Fatal("Catalog returns the package's own slice")
	}
}

func TestDescribeFallsBackToInternal(t *testing.T) {
	t.Parallel()

	got := jobs.Describe("a_code_from_the_future")
	if got.Type != jobs.Internal {
		t.Fatalf("Describe of an unknown code = %q, want %q", got.Type, jobs.Internal)
	}

	if got.Status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", got.Status)
	}
}

func TestClassify(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		err  error
		want jobs.ErrorType
	}{
		"nil":                {nil, ""},
		"deadline":           {context.DeadlineExceeded, jobs.Timeout},
		"cancelled":          {context.Canceled, jobs.CancelledByUser},
		"wrapped deadline":   {fmt.Errorf("run ffmpeg: %w", context.DeadlineExceeded), jobs.Timeout},
		"plain":              {errors.New("boom"), jobs.Internal},
		"failure":            {jobs.Failf(jobs.DiskFull, "no space in %s", "/tmp/x"), jobs.DiskFull},
		"wrapped failure":    {fmt.Errorf("stage 2: %w", jobs.Failf(jobs.InputTooLarge, "9000px")), jobs.InputTooLarge},
		"unknown error type": {jobs.Failf("made_up", "x"), jobs.Internal},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := jobs.Classify(tc.err); got != tc.want {
				t.Fatalf("Classify(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

// A failure nobody classified is retried. Treating it as the user's fault
// would both lie to the user and throw away work that might have succeeded.
func TestRetryable(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		err  error
		want bool
	}{
		"unclassified":      {errors.New("boom"), true},
		"timeout":           {context.DeadlineExceeded, true},
		"disk full":         {jobs.Failf(jobs.DiskFull, "x"), true},
		"out of memory":     {jobs.Failf(jobs.OutOfMemory, "x"), true},
		"bad input":         {jobs.Failf(jobs.UnsupportedInput, "x"), false},
		"too large":         {jobs.Failf(jobs.InputTooLarge, "x"), false},
		"quota":             {jobs.Failf(jobs.QuotaExceeded, "x"), false},
		"engine rejected":   {jobs.Failf(jobs.EngineFailure, "x"), false},
		"cancelled by user": {context.Canceled, false},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := jobs.Retryable(tc.err); got != tc.want {
				t.Fatalf("Retryable(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// The client-safe message must never carry the owner-only detail. This is
// the test that stops a path or an engine command line reaching a response.
func TestFailureDetailStaysOutOfTheClientMessage(t *testing.T) {
	t.Parallel()

	const secret = "/srv/work/9f3/input.mkv"

	err := jobs.Fail(jobs.EngineFailure, errors.New("exit status 1"), "ffmpeg -i "+secret)

	if !strings.Contains(jobs.DetailOf(err), secret) {
		t.Fatal("DetailOf dropped the owner-only detail, which the log needs")
	}

	if msg := jobs.Describe(jobs.Classify(err)).Message; strings.Contains(msg, secret) {
		t.Fatalf("the client-safe message leaked the detail: %q", msg)
	}
}

func TestFailureUnwraps(t *testing.T) {
	t.Parallel()

	cause := errors.New("exit status 137")

	err := fmt.Errorf("convert: %w", jobs.Fail(jobs.OutOfMemory, cause, "rss 2.1 GiB"))
	if !errors.Is(err, cause) {
		t.Fatal("the cause is not reachable with errors.Is")
	}
}
