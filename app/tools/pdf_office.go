package tools

import (
	"context"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/AlexGalhardo/vukem/z"

	"github.com/AlexGalhardo/vukem-tools/app/abilities"
	"github.com/AlexGalhardo/vukem-tools/internal/detect"
	"github.com/AlexGalhardo/vukem-tools/internal/jobs"
	"github.com/AlexGalhardo/vukem-tools/internal/pdf"
)

// The bounds of the engines that cost the most: a layout engine and a
// recogniser. Both sit above the library's own bound, so the tool's clock is
// the outer one.
const (
	pdfOfficeTimeout = 10 * time.Minute
	pdfOCRTimeout    = 30 * time.Minute
)

// PDFOfficeOptions is the input of an Office conversion. There is nothing to
// choose: the document's own page setup decides the pages.
type PDFOfficeOptions struct{}

// Normalise implements the option contract.
func (o PDFOfficeOptions) Normalise() (PDFOfficeOptions, error) { return o, nil }

// PDFOCROptions is the input of a recognition run.
type PDFOCROptions struct {
	// Languages are the Tesseract models to use together.
	Languages []string `json:"languages" validate:"max=4,dive,trim,max=8" doc:"Languages in the scan: eng, por. Several may be combined. Default eng."`
	// Deskew straightens pages scanned at an angle.
	Deskew bool `json:"deskew" doc:"Straighten pages that were scanned at a slight angle."`
}

// Normalise implements the option contract.
func (o PDFOCROptions) Normalise() (PDFOCROptions, error) {
	seen := make([]string, 0, len(o.Languages))

	for _, l := range o.Languages {
		l = strings.ToLower(strings.TrimSpace(l))
		if !slices.Contains(pdf.OCRLanguages(), l) {
			return PDFOCROptions{}, z.FieldError("options.languages",
				"The languages are: "+strings.Join(pdf.OCRLanguages(), ", ")+".")
		}

		if !slices.Contains(seen, l) {
			seen = append(seen, l)
		}
	}

	if len(seen) == 0 {
		seen = []string{"eng"}
	}

	o.Languages = seen

	return o, nil
}

// pdfOfficeTools are the Word, PowerPoint and Excel converters and the
// recogniser (S6.2).
//
// The three converters are one function with three input lists: LibreOffice
// does not care which family it is handed, and a separate tool per family is
// what lets a visitor find "Word to PDF" by that name and lets an API key be
// limited to the one it needs.
func pdfOfficeTools(deps *pdfDeps) []jobs.Tool {
	office := func(name string, accepted []string) jobs.Tool {
		return &pdfTool[PDFOfficeOptions]{
			deps: deps, name: name, ability: abilities.PDFConvert,
			timeout: pdfOfficeTimeout, accepted: accepted,
			engines: []string{pdf.EngineLibreOffice}, maxInputs: 1,
			work: officeToPDF,
		}
	}

	return []jobs.Tool{
		office(NameWordToPDF, detect.WordTypes()),
		office(NamePowerPointToPDF, detect.PresentationTypes()),
		office(NameExcelToPDF, detect.SpreadsheetTypes()),
		&pdfTool[PDFOCROptions]{
			deps: deps, name: NamePDFOCR, ability: abilities.PDFConvert,
			timeout: pdfOCRTimeout, accepted: pdfInputTypes,
			engines: []string{pdf.EngineOCRmyPDF}, maxInputs: 1,
			work: ocrPDF,
		},
	}
}

// officeToPDF converts one document.
func officeToPDF(ctx context.Context, w pdfWork[PDFOfficeOptions]) (pdfResult, error) {
	in := w.Inputs[0]
	out := filepath.Join(w.Dir, "converted.pdf")

	if err := w.Engines.OfficeToPDF(ctx, pdf.OfficeRequest{Dir: w.Dir, Input: in.Path, Output: out}); err != nil {
		return pdfResult{}, err
	}

	return pdfResult{
		Outputs: []pdfOutput{{Path: out, Name: in.Stem() + ".pdf", MIME: mimePDF}},
		Message: "Converted with LibreOffice. A font the server does not have is replaced by a similar " +
			"one, so line breaks can differ from the original application.",
	}, nil
}

// ocrPDF makes a scan searchable.
func ocrPDF(ctx context.Context, w pdfWork[PDFOCROptions]) (pdfResult, error) {
	in := w.Inputs[0]

	doc, err := w.Engines.Inspect(ctx, w.Dir, in.Path)
	if err != nil {
		return pdfResult{}, err
	}

	if doc.Count > pdf.MaxOCRPages {
		return pdfResult{}, &pdf.TooManyPagesError{Pages: doc.Count, Limit: pdf.MaxOCRPages}
	}

	out := filepath.Join(w.Dir, "searchable.pdf")

	err = w.Engines.OCR(ctx, pdf.OCRRequest{
		Dir: w.Dir, Input: in.Path, Output: out,
		Languages: w.Options.Languages, Deskew: w.Options.Deskew,
	})
	if err != nil {
		return pdfResult{}, err
	}

	return pdfResult{
		Outputs: []pdfOutput{{Path: out, Name: in.Stem() + "-searchable.pdf", MIME: mimePDF}},
		Message: "Recognised " + strconv.Itoa(doc.Count) + " pages in " + strings.Join(w.Options.Languages, "+") +
			". Pages that already had text were left as they were. Check names and numbers: recognition " +
			"makes mistakes.",
	}, nil
}
