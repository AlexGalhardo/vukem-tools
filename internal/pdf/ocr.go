package pdf

import (
	"context"
	"slices"
	"strings"
	"time"
)

// ocrTimeout bounds one OCR run. Recognition costs seconds per page, so a
// long scan needs minutes; the page cap keeps the worst case finite.
const ocrTimeout = 20 * time.Minute

// MaxOCRPages caps a scan. OCR is the most CPU-hungry operation in the
// service, and a bound the caller cannot raise is what keeps one request from
// holding a worker for an hour.
const MaxOCRPages = 100

// The OCR languages the image carries. A language is a Tesseract model that
// has to be installed, so the list is what the Dockerfile ships, not what
// Tesseract could do.
var ocrLanguages = []string{"eng", "por"}

// OCRLanguages returns the languages a client may name.
func OCRLanguages() []string { return slices.Clone(ocrLanguages) }

// OCRRequest is a scanned document to make searchable.
type OCRRequest struct {
	// Dir is the job's working directory.
	Dir string
	// Input and Output are PDFs inside Dir.
	Input, Output string
	// Languages are Tesseract codes from OCRLanguages. Empty means English.
	Languages []string
	// Deskew straightens pages that were scanned at a slight angle.
	Deskew bool
}

// OCR adds a text layer to a scanned PDF with OCRmyPDF and Tesseract.
//
// Pages that already carry text are left alone (--skip-text): redoing them
// would replace good text with a guess, and OCRmyPDF's default is to refuse
// the whole document. One job at a time (--jobs 1) because the service bounds
// concurrency itself and a second layer of parallelism would multiply the
// memory limit.
func (e *Engines) OCR(ctx context.Context, req OCRRequest) error {
	if !inside(req.Dir, req.Input) || !inside(req.Dir, req.Output) {
		return &OptionError{Field: "path", Reason: "The file is outside the working directory."}
	}

	langs := req.Languages
	if len(langs) == 0 {
		langs = []string{"eng"}
	}

	for _, l := range langs {
		if !slices.Contains(ocrLanguages, l) {
			return &OptionError{Field: "languages", Reason: "The language " + l + " is not installed."}
		}
	}

	args := []string{"--skip-text", "--jobs", "1", "--output-type", "pdf", "-l", strings.Join(langs, "+")}
	if req.Deskew {
		args = append(args, "--deskew")
	}

	args = append(args, req.Input, req.Output)

	if _, err := e.run(
		ctx,
		command{engine: EngineOCRmyPDF, dir: req.Dir, args: args, timeout: ocrTimeout},
	); err != nil {
		return err
	}

	return ValidateHeader(req.Output)
}
