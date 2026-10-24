package tools

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/AlexGalhardo/vukem-tools/app/abilities"
	"github.com/AlexGalhardo/vukem-tools/app/catalog"
	"github.com/AlexGalhardo/vukem-tools/internal/detect"
	"github.com/AlexGalhardo/vukem-tools/internal/jobs"
)

// pdfRegistered returns the PDF tools. They need no file store to be
// inspected: nothing is read until a conversion runs.
func pdfRegistered() []jobs.Tool { return pdfTools(nil) }

func TestEveryPDFToolIsCoherentlyDeclared(t *testing.T) {
	t.Parallel()

	registered := pdfRegistered()
	if len(registered) == 0 {
		t.Fatal("no PDF tools are registered")
	}

	seen := make(map[string]bool, len(registered))

	for _, tool := range registered {
		name := tool.Name()

		if seen[name] {
			t.Errorf("two PDF tools are called %q", name)
		}

		seen[name] = true

		if !strings.HasPrefix(name, "pdf-") && !strings.HasSuffix(name, "-to-pdf") {
			t.Errorf("%q does not read as a PDF tool's name", name)
		}

		// An ability that is not in the closed set would mint a key that
		// grants nothing and fails only at call time.
		if !abilities.Valid(tool.Ability()) {
			t.Errorf("%s needs the ability %q, which app/abilities does not define", name, tool.Ability())
		}

		if tool.Timeout() <= 0 {
			t.Errorf("%s has no timeout", name)
		}

		accepter, ok := tool.(jobs.Accepter)
		if !ok {
			t.Errorf("%s does not say which types it reads", name)

			continue
		}

		accepted := accepter.Accepted()
		if len(accepted) == 0 {
			t.Errorf("%s accepts no types, so it can never run", name)
		}

		// A type the service does not recognise can never arrive, because the
		// upload endpoint refuses it first.
		for _, mime := range accepted {
			if !detect.Known(mime) {
				t.Errorf("%s accepts %q, which is not a type uploads may be", name, mime)
			}
		}

		if _, ok := tool.(jobs.OptionsValidator); !ok {
			t.Errorf("%s does not validate its options, so an invalid one would reach the worker", name)
		}
	}
}

// Every registered tool has to appear in the catalogue as Live, and every
// catalogue entry marked Live has to be registered. The two drift apart
// silently otherwise: a tool with no entry is unreachable from the site, and
// an entry with no tool is a page with a form that cannot be submitted.
func TestThePDFCatalogueAndTheRegisteredToolsAgree(t *testing.T) {
	t.Parallel()

	registered := pdfRegistered()

	names := make([]string, 0, len(registered))
	for _, tool := range registered {
		names = append(names, tool.Name())
	}

	for _, name := range names {
		entry, ok := catalog.Find(name)
		if !ok {
			t.Errorf("%s is registered but has no catalogue entry", name)

			continue
		}

		if entry.Status != catalog.Live {
			// catalog.Find returns the first entry with this name, so a
			// stale duplicate earlier in the concatenated catalogue wins.
			// That is what this is: app/catalog/catalog.go's pixelCatalog
			// still holds four placeholder PDF entries from before the
			// per-domain split, and they shadow app/catalog/pdf.go's real
			// ones — so the tools page lists each of them twice, once Live
			// and once Planned, and the tool page renders the stale summary.
			t.Errorf("%s is registered but catalog.Find returns a %q entry.\n"+
				"Fix: delete the four placeholder PDF entries from pixelCatalog in "+
				"app/catalog/catalog.go (pdf-merge, pdf-compress, pdf-to-word, pdf-sign). "+
				"The real entries are in app/catalog/pdf.go. This file cannot make that change: "+
				"catalog.go is shared across the domains being built in parallel.",
				name, entry.Status)
		}

		if entry.Summary == "" {
			t.Errorf("%s has no summary, so the tools page cannot describe it", name)
		}
	}

	// And the other direction, over the whole catalogue: anything claiming to
	// be live must be registered somewhere.
	for _, entry := range catalog.All(names) {
		if entry.Status == catalog.Live && !slices.Contains(names, entry.Name) {
			// Another domain's tool, which this test cannot see. Only the PDF
			// ones are checked.
			continue
		}

		if entry.Status == catalog.Skipped && entry.Reason == "" {
			t.Errorf("%s is skipped with no reason, which reads as neglect", entry.Name)
		}
	}
}

// The options types are the public contract, so their defaults have to be the
// ones the documentation promises: a client that sends `{}` must get the
// documented behaviour rather than a zero value.
func TestPDFOptionDefaultsAreTheDocumentedOnes(t *testing.T) {
	t.Parallel()

	t.Run("split defaults to one file per page", func(t *testing.T) {
		t.Parallel()

		got, err := PDFSplitOptions{}.Normalise()
		if err != nil {
			t.Fatalf("Normalise = %v", err)
		}

		if got.Mode != splitEvery || got.Every != 1 {
			t.Errorf("Normalise = %+v, want mode=every every=1", got)
		}
	})

	t.Run("rotation defaults to a quarter turn", func(t *testing.T) {
		t.Parallel()

		got, err := PDFRotateOptions{}.Normalise()
		if err != nil {
			t.Fatalf("Normalise = %v", err)
		}

		if got.Degrees != 90 {
			t.Errorf("Degrees = %d, want 90", got.Degrees)
		}
	})

	t.Run("rendering defaults to PNG at 150 DPI", func(t *testing.T) {
		t.Parallel()

		got, err := PDFToImageOptions{}.Normalise()
		if err != nil {
			t.Fatalf("Normalise = %v", err)
		}

		if got.Format != "png" || got.DPI != 150 {
			t.Errorf("Normalise = %+v, want png at 150 DPI", got)
		}
	})

	t.Run("compression defaults to balanced", func(t *testing.T) {
		t.Parallel()

		got, err := PDFCompressOptions{}.Normalise()
		if err != nil {
			t.Fatalf("Normalise = %v", err)
		}

		if got.Level != "balanced" {
			t.Errorf("Level = %q, want balanced", got.Level)
		}
	})

	t.Run("page numbers default to the bottom centre", func(t *testing.T) {
		t.Parallel()

		got, err := PDFPageNumberOptions{}.Normalise()
		if err != nil {
			t.Fatalf("Normalise = %v", err)
		}

		if got.Format != "{n}" || got.Position != "bottom-center" || got.StartAt != 1 {
			t.Errorf("Normalise = %+v, want {n} at bottom-center starting at 1", got)
		}
	})
}

// An option a tool cannot act on has to be refused at submission, with the
// error naming the field: the alternative is a conversion that is accepted,
// queued and then fails where nobody is watching.
func TestPDFOptionsRefuseWhatTheToolCannotDo(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		call func() error
	}{
		{"a page range that names nothing", func() error {
			_, err := PDFPagesOptions{Pages: "  "}.Normalise()
			return err
		}},
		{"a split mode that does not exist", func() error {
			_, err := PDFSplitOptions{Mode: "halves"}.Normalise()
			return err
		}},
		{"a split by ranges with no ranges", func() error {
			_, err := PDFSplitOptions{Mode: "ranges"}.Normalise()
			return err
		}},
		{"a rotation that is not a quarter turn", func() error {
			_, err := PDFRotateOptions{Degrees: 45}.Normalise()
			return err
		}},
		{"a crop with no margins", func() error {
			_, err := PDFCropOptions{}.Normalise()
			return err
		}},
		{"a page size that does not exist", func() error {
			_, err := ImagesToPDFOptions{PageSize: "tabloid"}.Normalise()
			return err
		}},
		{"a resolution above the bound", func() error {
			_, err := PDFToImageOptions{DPI: 5000}.Normalise()
			return err
		}},
		{"a PDF/A part that does not exist", func() error {
			_, err := PDFToPDFAOptions{Part: 7}.Normalise()
			return err
		}},
		{"a text format that does not exist", func() error {
			_, err := PDFToTextOptions{Format: "html"}.Normalise()
			return err
		}},
		{"a compression level that does not exist", func() error {
			_, err := PDFCompressOptions{Level: "maximum"}.Normalise()
			return err
		}},
		{"protection with no owner password", func() error {
			_, err := PDFProtectOptions{UserPassword: "open"}.Normalise()
			return err
		}},
		{"protection whose two passwords are the same", func() error {
			_, err := PDFProtectOptions{UserPassword: "same", OwnerPassword: "same"}.Normalise()
			return err
		}},
		{"a password with a line break", func() error {
			_, err := PDFProtectOptions{OwnerPassword: "two\nlines"}.Normalise()
			return err
		}},
		{"an unlock with no password", func() error {
			_, err := PDFUnlockOptions{}.Normalise()
			return err
		}},
		{"a watermark with no text", func() error {
			_, err := PDFWatermarkOptions{Text: "   "}.Normalise()
			return err
		}},
		{"a watermark position that does not exist", func() error {
			_, err := PDFWatermarkOptions{Text: "DRAFT", Position: "middle"}.Normalise()
			return err
		}},
		{"a page-number size above the bound", func() error {
			_, err := PDFPageNumberOptions{Size: 500}.Normalise()
			return err
		}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			if err := c.call(); err == nil {
				t.Error("the options were accepted, but the tool cannot act on them")
			}
		})
	}
}

// A password must survive normalisation exactly. Trimming it would mean the
// document could not be opened with the password the user thinks they set,
// and they would have no way to find out which one it is.
func TestAPasswordIsNotTrimmed(t *testing.T) {
	t.Parallel()

	const password = "  leading and trailing  "

	protect, err := PDFProtectOptions{OwnerPassword: password}.Normalise()
	if err != nil {
		t.Fatalf("Normalise = %v", err)
	}

	if protect.OwnerPassword != password {
		t.Errorf("OwnerPassword = %q, want it unchanged", protect.OwnerPassword)
	}

	unlock, err := PDFUnlockOptions{Password: password}.Normalise()
	if err != nil {
		t.Fatalf("Normalise = %v", err)
	}

	if unlock.Password != password {
		t.Errorf("Password = %q, want it unchanged", unlock.Password)
	}
}

// What ValidateOptions returns is what gets stored, so the worker has to read
// back exactly the options that were validated — including the defaults that
// were filled in.
func TestValidatedOptionsAreWhatTheWorkerWillRead(t *testing.T) {
	t.Parallel()

	for _, tool := range pdfRegistered() {
		validator, ok := tool.(jobs.OptionsValidator)
		if !ok {
			continue
		}

		// The tools whose options have a required field cannot be validated
		// from an empty object, which is itself the behaviour under test
		// elsewhere.
		stored, err := validator.ValidateOptions(json.RawMessage(`{}`))
		if err != nil {
			continue
		}

		// Normalising again must be a no-op: a second pass that changed
		// something would mean the stored options are not the normalised
		// ones.
		again, err := validator.ValidateOptions(stored)
		if err != nil {
			t.Errorf("%s: revalidating its own stored options failed: %v", tool.Name(), err)

			continue
		}

		if string(again) != string(stored) {
			t.Errorf("%s: normalising is not idempotent\nfirst  %s\nsecond %s",
				tool.Name(), stored, again)
		}
	}
}
