package pdf_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AlexGalhardo/vukem-tools/internal/pdf"
)

func TestAFileWithNoPDFHeaderIsRefusedWithoutAnEngine(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		bytes []byte
	}{
		{"empty", nil},
		{"plain text", []byte("Dear Sir, please find attached my PDF.\n")},
		{"a comment that is not a header", []byte("%PDF-x this is not a version\n")},
		{"a JPEG", []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 'J', 'F', 'I', 'F'}},
		{"a header past the window", append(make([]byte, 2048), []byte("%PDF-1.7")...)},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			// A directory per subtest, not one shared by the parent: a
			// parent's temporary directory is cleaned up around its parallel
			// subtests' files, and on Windows a file that is momentarily
			// locked makes that cleanup fail and the test with it.
			path := filepath.Join(t.TempDir(), strings.ReplaceAll(c.name, " ", "-")+".pdf")
			if err := os.WriteFile(path, c.bytes, 0o600); err != nil {
				t.Fatalf("write %s: %v", path, err)
			}

			err := pdf.ValidateHeader(path)
			if err == nil {
				t.Fatal("ValidateHeader accepted a file that is not a PDF")
			}

			notPDF, ok := errors.AsType[*pdf.NotPDFError](err)
			if !ok {
				t.Fatalf("error is %T, want *pdf.NotPDFError", err)
			}

			if notPDF.Message() == "" {
				t.Error("the refusal needs a sentence to show the user")
			}
		})
	}
}

func TestAHeaderAfterSomeLeadingJunkIsAccepted(t *testing.T) {
	t.Parallel()

	// Readers and both engines scan for the header, so a file they accept has
	// to be accepted here: refusing it would reject documents that work
	// everywhere else.
	path := filepath.Join(t.TempDir(), "offset.pdf")
	body := append([]byte("\n\n   junk from a bad transfer\n"), []byte("%PDF-1.4\n")...)

	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := pdf.ValidateHeader(path); err != nil {
		t.Errorf("ValidateHeader = %v, want nil", err)
	}
}

func TestInspectReadsThePageListOfADocumentWeWrote(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF)
	dir := t.TempDir()
	path := writeFixture(t, dir, "input.pdf", 4)

	doc, err := engines.Inspect(context.Background(), dir, path)
	if err != nil {
		t.Fatalf("Inspect = %v", err)
	}

	if doc.Count != 4 {
		t.Errorf("Count = %d, want 4", doc.Count)
	}

	for page := 1; page <= 4; page++ {
		obj, ok := doc.Object(page)
		if !ok || obj == "" {
			t.Errorf("page %d has no object id", page)
		}
	}

	if _, ok := doc.Object(5); ok {
		t.Error("Object(5) resolved on a four-page document")
	}

	if _, ok := doc.Object(0); ok {
		t.Error("Object(0) resolved: pages are 1-based")
	}
}

// An input outside the job's own directory is refused before any engine is
// pointed at it. The upload sanitises the name, so reaching here would mean
// two gates had been passed, and this is the second one.
func TestInspectRefusesAPathOutsideTheJobDirectory(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF)
	dir := t.TempDir()
	outside := writeFixture(t, t.TempDir(), "elsewhere.pdf", 1)

	if _, err := engines.Inspect(context.Background(), dir, outside); err == nil {
		t.Error("Inspect accepted a path outside the job directory")
	}
}

// ROADMAP S6.1: "An encrypted input is refused with unsupported_input rather
// than producing an empty file." This is the library half of that: the
// refusal happens before any engine runs on the document.
func TestAnEncryptedDocumentIsRefusedByEveryOperation(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF)
	dir := t.TempDir()
	plain := writeFixture(t, dir, "plain.pdf", 2)

	locked := filepath.Join(dir, "locked.pdf")
	if err := engines.Protect(context.Background(), pdf.ProtectRequest{
		Dir:           dir,
		Input:         plain,
		Output:        locked,
		UserPassword:  "open-me",
		OwnerPassword: "owner-me",
	}); err != nil {
		t.Fatalf("Protect = %v", err)
	}

	encrypted, err := engines.IsEncrypted(context.Background(), dir, locked)
	if err != nil {
		t.Fatalf("IsEncrypted = %v", err)
	}

	if !encrypted {
		t.Fatal("the protected document does not report as encrypted")
	}

	if _, err := engines.Inspect(context.Background(), dir, locked); !errors.Is(err, pdf.ErrEncrypted) {
		t.Errorf("Inspect = %v, want ErrEncrypted", err)
	}
}

func TestADocumentWeWroteIsNotEncrypted(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF)
	dir := t.TempDir()
	path := writeFixture(t, dir, "plain.pdf", 1)

	encrypted, err := engines.IsEncrypted(context.Background(), dir, path)
	if err != nil {
		t.Fatalf("IsEncrypted = %v", err)
	}

	if encrypted {
		t.Error("a document this package wrote reports as encrypted")
	}
}
