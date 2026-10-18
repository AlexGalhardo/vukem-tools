package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/AlexGalhardo/vukem-tools/internal/jobs"
	"github.com/AlexGalhardo/vukem-tools/internal/pdf"
)

// Every failure the PDF library can report has to arrive at the taxonomy as
// the right code, because the code decides two things that matter: what the
// user is told, and whether the queue tries again. A password-protected
// document classified as Internal would be retried three times and then
// reported as "something went wrong on our side".
//
// ROADMAP S6.1's last assert — "an encrypted input is refused with
// unsupported_input rather than producing an empty file" — is the first row.
func TestEveryPDFFailureIsClassifiedOntoTheTaxonomy(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want jobs.ErrorType
		// retry is what the taxonomy says about the code, which is the part
		// that decides whether a worker is spent again on the same file.
		retry bool
		// publicMentions is a word the sentence shown to the user must
		// contain, where the sentence has to be specific to be useful.
		publicMentions string
	}{
		{
			name:           "an encrypted document",
			err:            pdf.ErrEncrypted,
			want:           jobs.UnsupportedInput,
			publicMentions: "password-protected",
		},
		{
			name:           "a wrong password",
			err:            pdf.ErrWrongPassword,
			want:           jobs.UnsupportedInput,
			publicMentions: "does not open",
		},
		{
			name:           "a document that has no password",
			err:            pdf.ErrNotEncrypted,
			want:           jobs.UnsupportedInput,
			publicMentions: "no password",
		},
		{
			name:           "a file that is not a PDF",
			err:            &pdf.NotPDFError{Reason: "no header"},
			want:           jobs.UnsupportedInput,
			publicMentions: "not a PDF",
		},
		{
			name:           "a damaged document",
			err:            &pdf.DamagedError{Detail: "cannot read the trailer"},
			want:           jobs.UnsupportedInput,
			publicMentions: "damaged",
		},
		{
			name:           "a document past the page cap",
			err:            &pdf.TooManyPagesError{Pages: 20000, Limit: pdf.MaxPages},
			want:           jobs.InputTooLarge,
			publicMentions: "20000",
		},
		{
			name:           "a page range that names a page the document lacks",
			err:            &pdf.PageRangeError{Input: "11", Reason: "This document has 10 pages."},
			want:           jobs.UnsupportedInput,
			publicMentions: "10 pages",
		},
		{
			name:           "an option the tool cannot act on",
			err:            &pdf.OptionError{Field: "dpi", Reason: "The resolution must be between 36 and 600 DPI."},
			want:           jobs.UnsupportedInput,
			publicMentions: "resolution",
		},
		{
			name:  "an engine that ran out of time",
			err:   &pdf.TimeoutError{Engine: "gs"},
			want:  jobs.Timeout,
			retry: true,
		},
		{
			name:           "an engine that is not installed",
			err:            &pdf.EngineMissingError{Engine: "gs"},
			want:           jobs.EngineFailure,
			publicMentions: "not available",
		},
		{
			name:           "an engine that rejected the document",
			err:            &pdf.EngineError{Engine: "qpdf", Code: 2, Stderr: "qpdf: /job/input-0.pdf: bad object"},
			want:           jobs.EngineFailure,
			publicMentions: "could not process",
		},
		{
			name:  "something nobody classified",
			err:   errors.New("the disk went away"),
			want:  jobs.Internal,
			retry: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			got := classifyPDF(c.err)

			if kind := jobs.Classify(got); kind != c.want {
				t.Errorf("classified as %q, want %q", kind, c.want)
			}

			if retry := jobs.Retryable(got); retry != c.retry {
				t.Errorf("retryable = %v, want %v", retry, c.retry)
			}

			public := jobs.MessageOf(got)
			if public == "" {
				t.Fatal("there is no sentence to show the user")
			}

			if c.publicMentions != "" && !strings.Contains(public, c.publicMentions) {
				t.Errorf("the message is %q, which does not mention %q", public, c.publicMentions)
			}

			// The owner-only detail must never reach the user. An engine's
			// own text routinely holds the working directory and the whole
			// command line, which is why it lives on Detail and nowhere
			// else.
			if strings.Contains(public, "/job/") || strings.Contains(public, "input-0.pdf") {
				t.Errorf("the message shown to the user leaks a path: %q", public)
			}
		})
	}
}

// A cancellation is the owner stopping the conversion, not a failure of the
// input, and it must survive classification as such: reporting it as
// unsupported_input would tell somebody their file was wrong when they had
// simply pressed cancel.
func TestACancellationIsNotBlamedOnTheInput(t *testing.T) {
	t.Parallel()

	if kind := jobs.Classify(classifyPDF(context.Canceled)); kind != jobs.CancelledByUser {
		t.Errorf("a cancelled context classified as %q, want %q", kind, jobs.CancelledByUser)
	}
}
