package pdf_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AlexGalhardo/vukem-tools/internal/pdf"
)

// flatODT is the smallest document LibreOffice opens: one paragraph, in the
// flat OpenDocument format, which is plain XML and so needs no ZIP to build.
const flatODT = `<?xml version="1.0" encoding="UTF-8"?>
<office:document xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0"
 xmlns:text="urn:oasis:names:tc:opendocument:xmlns:text:1.0"
 office:version="1.3" office:mimetype="application/vnd.oasis.opendocument.text">
 <office:body><office:text><text:p>Quarterly report for the vukem team</text:p></office:text></office:body>
</office:document>`

func pdfText(t *testing.T, engines *pdf.Engines, dir, path string) string {
	t.Helper()

	out := filepath.Join(dir, filepath.Base(path)+".txt")
	if err := engines.ExtractText(
		context.Background(),
		pdf.TextRequest{Dir: dir, Input: path, Output: out},
	); err != nil {
		t.Fatalf("ExtractText: %v", err)
	}

	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read the extracted text: %v", err)
	}

	return string(body)
}

func TestADocumentBecomesAPDFWithItsText(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineLibreOffice, pdf.EngineQPDF, pdf.EnginePDFToText)
	dir := t.TempDir()
	in := filepath.Join(dir, "report.fodt")

	if err := os.WriteFile(in, []byte(flatODT), 0o600); err != nil {
		t.Fatalf("write the document: %v", err)
	}

	out := filepath.Join(dir, "report.pdf")
	if err := engines.OfficeToPDF(
		context.Background(),
		pdf.OfficeRequest{Dir: dir, Input: in, Output: out},
	); err != nil {
		t.Fatalf("OfficeToPDF: %v", err)
	}

	if text := pdfText(t, engines, dir, out); !strings.Contains(text, "Quarterly report") {
		t.Errorf("the PDF's text is %q, want the document's paragraph", text)
	}
}

func TestAFileLibreOfficeCannotOpenIsAnEngineFailureNotAnEmptyPDF(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineLibreOffice)
	dir := t.TempDir()
	in := filepath.Join(dir, "broken.docx")

	if err := os.WriteFile(in, []byte("PK"+string([]byte{3, 4})+" truncated, not a real archive"), 0o600); err != nil {
		t.Fatalf("write the file: %v", err)
	}

	err := engines.OfficeToPDF(
		context.Background(),
		pdf.OfficeRequest{Dir: dir, Input: in, Output: filepath.Join(dir, "x.pdf")},
	)
	if _, ok := errors.AsType[*pdf.EngineError](err); !ok {
		t.Fatalf("err = %v, want an EngineError", err)
	}
}

func TestAScanBecomesSearchable(t *testing.T) {
	t.Parallel()

	engines := testEngines(
		t,
		pdf.EngineLibreOffice,
		pdf.EngineOCRmyPDF,
		pdf.EngineGhostscript,
		pdf.EngineQPDF,
		pdf.EnginePDFToText,
	)
	dir := t.TempDir()
	src := filepath.Join(dir, "report.fodt")

	if err := os.WriteFile(src, []byte(flatODT), 0o600); err != nil {
		t.Fatalf("write the document: %v", err)
	}

	typed := filepath.Join(dir, "typed.pdf")
	if err := engines.OfficeToPDF(
		context.Background(),
		pdf.OfficeRequest{Dir: dir, Input: src, Output: typed},
	); err != nil {
		t.Fatalf("OfficeToPDF: %v", err)
	}

	// Rasterise it into an image-only PDF: this is what a scan is.
	scan := filepath.Join(dir, "scan.pdf")
	// Plain exec is fine in a test: this builds the input, it is not a
	// conversion.
	gs, lookErr := exec.LookPath("gs")
	if lookErr != nil {
		t.Skip("gs is not on PATH")
	}

	if out, err := exec.CommandContext(context.Background(), gs, "-q", "-dNOPAUSE", "-dBATCH",
		"-sDEVICE=pdfimage24", "-r200", "-sOutputFile="+scan, typed).CombinedOutput(); err != nil {
		t.Fatalf("make the scan: %v: %s", err, out)
	}

	if text := strings.TrimSpace(pdfText(t, engines, dir, scan)); text != "" {
		t.Fatalf("the fixture scan already has text %q, so the test proves nothing", text)
	}

	out := filepath.Join(dir, "searchable.pdf")
	if err := engines.OCR(context.Background(), pdf.OCRRequest{Dir: dir, Input: scan, Output: out}); err != nil {
		t.Fatalf("OCR: %v", err)
	}

	if text := pdfText(t, engines, dir, out); !strings.Contains(text, "Quarterly") {
		t.Errorf("the recognised text is %q, want the page's words", text)
	}
}

func TestOCRRefusesALanguageItDoesNotHave(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineOCRmyPDF)
	dir := t.TempDir()

	err := engines.OCR(context.Background(), pdf.OCRRequest{
		Dir: dir, Input: filepath.Join(dir, "a.pdf"), Output: filepath.Join(dir, "b.pdf"), Languages: []string{"xx"},
	})
	if _, ok := errors.AsType[*pdf.OptionError](err); !ok {
		t.Fatalf("err = %v, want an OptionError", err)
	}
}
