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
	"github.com/AlexGalhardo/vukem-tools/internal/files"
	"github.com/AlexGalhardo/vukem-tools/internal/jobs"
	"github.com/AlexGalhardo/vukem-tools/internal/pdf"
)

// The stable tool ids. They are stored on every conversion and published by
// GET /api/v1/tools, so each one is part of the public API and renaming one is
// a breaking change.
const (
	NamePDFMerge        = "pdf-merge"
	NamePDFSplit        = "pdf-split"
	NamePDFExtractPages = "pdf-extract-pages"
	NamePDFRemovePages  = "pdf-remove-pages"
	NamePDFOrganise     = "pdf-organise"
	NamePDFRotate       = "pdf-rotate"
	NamePDFCrop         = "pdf-crop"
	NameImagesToPDF     = "images-to-pdf"
	NamePDFToImage      = "pdf-to-image"
	NamePDFToPDFA       = "pdf-to-pdfa"
	NamePDFToText       = "pdf-to-text"
	NamePDFCompress     = "pdf-compress"
	NamePDFRepair       = "pdf-repair"
	NamePDFProtect      = "pdf-protect"
	NamePDFUnlock       = "pdf-unlock"
	NamePDFCompare      = "pdf-compare"
	NamePDFWatermark    = "pdf-watermark"
	NamePDFPageNumbers  = "pdf-page-numbers"
)

// The media types the PDF domain reads.
const (
	mimePDF      = "application/pdf"
	mimePlain    = "text/plain"
	mimeMarkdown = "text/markdown"
)

// pdfInputTypes is what every PDF-reading tool accepts.
//
// One type, from the bytes. A PDF is the case the project's rule about magic
// bytes was written for: the extension says nothing, and a document whose
// header is malformed must be refused rather than handed to Ghostscript.
var pdfInputTypes = []string{mimePDF}

// imageInputTypes is what images-to-pdf accepts.
//
// JPEG is first because it is the one embedded without being re-encoded.
// Everything else goes through libvips once. SVG is deliberately absent: it
// is XML, and docs/deployment.md records that no live tool accepts XML
// input, which is what keeps libxml2's denial-of-service advisory
// unreachable.
var imageInputTypes = []string{
	"image/jpeg",
	"image/png",
	"image/webp",
	"image/tiff",
	"image/avif",
	"image/gif",
	"image/bmp",
}

// The per-tool bounds. They differ by an order of magnitude on purpose: a
// rotation edits a dictionary and a PDF/A conversion re-renders every page.
const (
	pdfStructureTimeout = 5 * time.Minute
	pdfStampTimeout     = 10 * time.Minute
	pdfRasterTimeout    = 15 * time.Minute
	pdfGhostTimeout     = 25 * time.Minute
)

// pdfTools is the PDF domain (S6).
//
// Every entry is the same generic tool with a different name, ability, bound
// and work function; see pdfkit.go for why there is one implementation rather
// than nineteen.
func pdfTools(f *files.Store) []jobs.Tool {
	deps := newPDFDeps(f)

	return slices.Concat(
		pdfStructureTools(deps),
		pdfConversionTools(deps),
		pdfOptimiseTools(deps),
		pdfSecurityTools(deps),
		pdfStampTools(deps),
	)
}

// ---------------------------------------------------------------------------
// S6.1 Structure
// ---------------------------------------------------------------------------

// PDFMergeOptions is the input of a merge. There is nothing to choose: the
// order is the order the files were attached.
type PDFMergeOptions struct{}

// Normalise implements the option contract.
func (o PDFMergeOptions) Normalise() (PDFMergeOptions, error) { return o, nil }

// PDFPagesOptions names a page range, for the tools whose only option is
// which pages.
type PDFPagesOptions struct {
	// Pages is a range such as "1-3,5,8-z". Required: a tool that defaulted
	// to "every page" would make "extract pages" a copy and "remove pages"
	// an empty document.
	Pages string `json:"pages" validate:"required,trim,max=512" doc:"Pages, as 1-3,5,8-z. z is the last page."`
}

// Normalise implements the option contract.
func (o PDFPagesOptions) Normalise() (PDFPagesOptions, error) {
	o.Pages = strings.TrimSpace(o.Pages)
	if o.Pages == "" {
		return PDFPagesOptions{}, z.FieldError("options.pages", "Name the pages, for example 1-3,5.")
	}

	return o, nil
}

// PDFSplitOptions is the input of a split.
type PDFSplitOptions struct {
	// Mode is "every" or "ranges".
	Mode string `json:"mode" validate:"trim,max=16" doc:"every splits into groups of n pages; ranges uses the ranges given. Default every."`
	// Every is the group size for the every mode. 1 gives one file per page.
	Every int `json:"every" validate:"min=0,max=10000" doc:"Pages per file, for the every mode. Default 1."`
	// Ranges are the parts for the ranges mode, each a range such as "1-3".
	Ranges []string `json:"ranges" validate:"max=500,dive,trim,max=512" doc:"One range per output file, for the ranges mode."`
}

// The split modes.
const (
	splitEvery  = "every"
	splitRanges = "ranges"
)

// Normalise implements the option contract.
func (o PDFSplitOptions) Normalise() (PDFSplitOptions, error) {
	if o.Mode = strings.TrimSpace(o.Mode); o.Mode == "" {
		o.Mode = splitEvery
	}

	switch o.Mode {
	case splitEvery:
		if o.Every == 0 {
			o.Every = 1
		}

		if o.Every < 1 {
			return PDFSplitOptions{}, z.FieldError("options.every", "Split into groups of one page or more.")
		}

		o.Ranges = nil
	case splitRanges:
		cleaned := make([]string, 0, len(o.Ranges))

		for _, part := range o.Ranges {
			if trimmed := strings.TrimSpace(part); trimmed != "" {
				cleaned = append(cleaned, trimmed)
			}
		}

		if len(cleaned) == 0 {
			return PDFSplitOptions{}, z.FieldError("options.ranges", "Give at least one range, for example 1-3.")
		}

		o.Ranges = cleaned
		o.Every = 0
	default:
		return PDFSplitOptions{}, z.FieldError("options.mode", "The mode is either every or ranges.")
	}

	return o, nil
}

// PDFRotateOptions is the input of a rotation.
type PDFRotateOptions struct {
	// Degrees is 90, 180 or 270, clockwise.
	Degrees int `json:"degrees" validate:"min=0,max=360" doc:"90, 180 or 270, clockwise. Default 90."`
	// Pages is the range to rotate. Empty rotates every page.
	Pages string `json:"pages" validate:"trim,max=512" doc:"Pages to rotate, as 1-3,5. Empty rotates every page."`
	// Absolute sets the rotation instead of adding to the page's own.
	Absolute bool `json:"absolute" doc:"Set the rotation rather than add to the page's current one."`
}

// Normalise implements the option contract.
func (o PDFRotateOptions) Normalise() (PDFRotateOptions, error) {
	if o.Degrees == 0 {
		o.Degrees = 90
	}

	if !slices.Contains([]int{90, 180, 270}, o.Degrees) {
		return PDFRotateOptions{}, z.FieldError("options.degrees", "Rotate by 90, 180 or 270 degrees.")
	}

	o.Pages = strings.TrimSpace(o.Pages)

	return o, nil
}

// PDFCropOptions is the input of a crop.
type PDFCropOptions struct {
	// The margins to trim, in points. 72 points to the inch.
	Left   float64 `json:"left"   validate:"min=0,max=1000" doc:"Points to trim from the left edge."`
	Bottom float64 `json:"bottom" validate:"min=0,max=1000" doc:"Points to trim from the bottom edge."`
	Right  float64 `json:"right"  validate:"min=0,max=1000" doc:"Points to trim from the right edge."`
	Top    float64 `json:"top"    validate:"min=0,max=1000" doc:"Points to trim from the top edge."`
	// Pages is the range to crop. Empty crops every page.
	Pages string `json:"pages" validate:"trim,max=512" doc:"Pages to crop, as 1-3,5. Empty crops every page."`
}

// Normalise implements the option contract.
func (o PDFCropOptions) Normalise() (PDFCropOptions, error) {
	if o.Left == 0 && o.Bottom == 0 && o.Right == 0 && o.Top == 0 {
		return PDFCropOptions{}, z.FieldError("options.left", "Say how many points to trim from at least one edge.")
	}

	o.Pages = strings.TrimSpace(o.Pages)

	return o, nil
}

// pdfStructureTools are the operations that rearrange pages (S6.1).
func pdfStructureTools(deps *pdfDeps) []jobs.Tool {
	return []jobs.Tool{
		&pdfTool[PDFMergeOptions]{
			deps: deps, name: NamePDFMerge, ability: abilities.PDFStructure,
			timeout: pdfStructureTimeout, accepted: pdfInputTypes,
			engines: []string{pdf.EngineQPDF}, minInputs: 2, maxInputs: pdf.MaxMergeInputs,
			work: mergePDFs,
		},
		&pdfTool[PDFSplitOptions]{
			deps: deps, name: NamePDFSplit, ability: abilities.PDFStructure,
			timeout: pdfStructureTimeout, accepted: pdfInputTypes,
			engines: []string{pdf.EngineQPDF}, maxInputs: 1,
			work: splitPDF,
		},
		&pdfTool[PDFPagesOptions]{
			deps: deps, name: NamePDFExtractPages, ability: abilities.PDFStructure,
			timeout: pdfStructureTimeout, accepted: pdfInputTypes,
			engines: []string{pdf.EngineQPDF}, maxInputs: 1,
			work: selectPDFPages(false),
		},
		&pdfTool[PDFPagesOptions]{
			deps: deps, name: NamePDFRemovePages, ability: abilities.PDFStructure,
			timeout: pdfStructureTimeout, accepted: pdfInputTypes,
			engines: []string{pdf.EngineQPDF}, maxInputs: 1,
			work: selectPDFPages(true),
		},
		&pdfTool[PDFPagesOptions]{
			deps: deps, name: NamePDFOrganise, ability: abilities.PDFStructure,
			timeout: pdfStructureTimeout, accepted: pdfInputTypes,
			engines: []string{pdf.EngineQPDF}, maxInputs: 1,
			work: selectPDFPages(false),
		},
		&pdfTool[PDFRotateOptions]{
			deps: deps, name: NamePDFRotate, ability: abilities.PDFStructure,
			timeout: pdfStructureTimeout, accepted: pdfInputTypes,
			engines: []string{pdf.EngineQPDF}, maxInputs: 1,
			work: rotatePDF,
		},
		&pdfTool[PDFCropOptions]{
			deps: deps, name: NamePDFCrop, ability: abilities.PDFStructure,
			timeout: pdfStructureTimeout, accepted: pdfInputTypes,
			engines: []string{pdf.EngineQPDF}, maxInputs: 1,
			work: cropPDF,
		},
	}
}

// mergePDFs joins the attached documents in the order they were attached.
func mergePDFs(ctx context.Context, w pdfWork[PDFMergeOptions]) (pdfResult, error) {
	paths := make([]string, 0, len(w.Inputs))
	for _, in := range w.Inputs {
		paths = append(paths, in.Path)
	}

	out := filepath.Join(w.Dir, "merged.pdf")

	result, err := w.Engines.Merge(ctx, pdf.MergeRequest{
		Dir:      w.Dir,
		Inputs:   paths,
		Output:   out,
		Progress: func(done, total int) { _ = w.Report(done*50/total, "checking") },
	})
	if err != nil {
		return pdfResult{}, err
	}

	message := "Joined " + strconv.Itoa(len(paths)) + " documents into " +
		strconv.Itoa(result.Pages) + " pages."

	// Said rather than implied: only the first document's bookmarks survive,
	// and a user who merged a book into a cover page needs to know which half
	// of the table of contents they have.
	if result.OutlineKept {
		message += " The first document's bookmarks were kept; the others' cannot be."
	}

	return pdfResult{
		Outputs: []pdfOutput{{Path: out, Name: w.Inputs[0].Stem() + "-merged.pdf", MIME: mimePDF}},
		Message: message,
	}, nil
}

// splitPDF writes one document per group.
func splitPDF(ctx context.Context, w pdfWork[PDFSplitOptions]) (pdfResult, error) {
	in := w.Inputs[0]

	doc, err := w.Engines.Inspect(ctx, w.Dir, in.Path)
	if err != nil {
		return pdfResult{}, err
	}

	groups, err := splitGroups(w.Options, doc.Count)
	if err != nil {
		return pdfResult{}, err
	}

	parts, err := w.Engines.Split(ctx, pdf.SplitRequest{
		Dir:         w.Dir,
		Input:       in.Path,
		SourcePages: doc.Count,
		Groups:      groups,
		Prefix:      "part",
		Progress:    func(done, total int) { _ = w.Report(done*100/total, "splitting") },
	})
	if err != nil {
		return pdfResult{}, err
	}

	outputs := make([]pdfOutput, 0, len(parts))
	stem := in.Stem()

	for _, part := range parts {
		outputs = append(outputs, pdfOutput{
			Path: part.Path,
			// Named after the pages it holds, so a folder of twenty downloads
			// is still readable.
			Name: stem + "-" + pdf.QPDFRange(part.Pages) + ".pdf",
			MIME: mimePDF,
		})
	}

	return pdfResult{
		Outputs: outputs,
		Message: "Split into " + strconv.Itoa(len(outputs)) + " files. Every one has to be " +
			"downloaded before the result expires.",
	}, nil
}

// splitGroups turns the options into the page groups to produce.
func splitGroups(opts PDFSplitOptions, pages int) ([][]int, error) {
	if opts.Mode == splitEvery {
		return pdf.GroupEvery(pages, opts.Every), nil
	}

	groups := make([][]int, 0, len(opts.Ranges))

	for _, part := range opts.Ranges {
		group, err := pdf.ParsePageRange(part, pages)
		if err != nil {
			return nil, err
		}

		groups = append(groups, group)
	}

	return groups, nil
}

// selectPDFPages returns the work function for extract, remove and organise.
//
// All three are a page selection; remove is the complement of the range it
// was given. Sharing the function is what keeps "remove pages 2-3" and
// "extract pages 1,4" from drifting apart.
func selectPDFPages(complement bool) func(context.Context, pdfWork[PDFPagesOptions]) (pdfResult, error) {
	return func(ctx context.Context, w pdfWork[PDFPagesOptions]) (pdfResult, error) {
		in := w.Inputs[0]

		doc, err := w.Engines.Inspect(ctx, w.Dir, in.Path)
		if err != nil {
			return pdfResult{}, err
		}

		pages, err := pdf.ParsePageRange(w.Options.Pages, doc.Count)
		if err != nil {
			return pdfResult{}, err
		}

		suffix := "-pages"

		if complement {
			pages = pdf.Complement(pages, doc.Count)
			suffix = "-trimmed"

			if len(pages) == 0 {
				return pdfResult{}, &pdf.OptionError{
					Field:  "pages",
					Reason: "That would remove every page, leaving a document that no reader opens.",
				}
			}
		}

		out := filepath.Join(w.Dir, "selected.pdf")

		err = w.Engines.Select(ctx, pdf.SelectRequest{
			Dir: w.Dir, Input: in.Path, Output: out, Pages: pages, SourcePages: doc.Count,
		})
		if err != nil {
			return pdfResult{}, err
		}

		message := strconv.Itoa(len(pages)) + " of " + strconv.Itoa(doc.Count) + " pages kept."
		if !pdf.KeepsOutline(pages, doc.Count) {
			// Honest rather than silent: an outline whose entries point at
			// pages that are gone is worse than no outline, so it is dropped
			// and the user is told.
			message += " The bookmarks were dropped, because some would point at pages that are no longer here."
		}

		return pdfResult{
			Outputs: []pdfOutput{{Path: out, Name: in.Stem() + suffix + ".pdf", MIME: mimePDF}},
			Message: message,
		}, nil
	}
}

// rotatePDF turns pages.
func rotatePDF(ctx context.Context, w pdfWork[PDFRotateOptions]) (pdfResult, error) {
	in := w.Inputs[0]

	doc, err := w.Engines.Inspect(ctx, w.Dir, in.Path)
	if err != nil {
		return pdfResult{}, err
	}

	pages, err := optionalRange(w.Options.Pages, doc.Count)
	if err != nil {
		return pdfResult{}, err
	}

	out := filepath.Join(w.Dir, "rotated.pdf")

	err = w.Engines.Rotate(ctx, pdf.RotateRequest{
		Dir: w.Dir, Input: in.Path, Output: out,
		Degrees: w.Options.Degrees, Pages: pages, SourcePages: doc.Count, Absolute: w.Options.Absolute,
	})
	if err != nil {
		return pdfResult{}, err
	}

	return pdfResult{
		Outputs: []pdfOutput{{Path: out, Name: in.Stem() + "-rotated.pdf", MIME: mimePDF}},
		Message: "Rotated by " + strconv.Itoa(w.Options.Degrees) + " degrees. " +
			"The bookmarks and links are unchanged.",
	}, nil
}

// cropPDF sets a crop box.
func cropPDF(ctx context.Context, w pdfWork[PDFCropOptions]) (pdfResult, error) {
	in := w.Inputs[0]

	doc, err := w.Engines.Inspect(ctx, w.Dir, in.Path)
	if err != nil {
		return pdfResult{}, err
	}

	pages, err := optionalRange(w.Options.Pages, doc.Count)
	if err != nil {
		return pdfResult{}, err
	}

	out := filepath.Join(w.Dir, "cropped.pdf")

	result, err := w.Engines.Crop(ctx, pdf.CropRequest{
		Dir: w.Dir, Input: in.Path, Output: out, Pages: pages,
		Margins: pdf.Margins{
			Left:   w.Options.Left,
			Bottom: w.Options.Bottom,
			Right:  w.Options.Right,
			Top:    w.Options.Top,
		},
	})
	if err != nil {
		return pdfResult{}, err
	}

	return pdfResult{
		Outputs: []pdfOutput{{Path: out, Name: in.Stem() + "-cropped.pdf", MIME: mimePDF}},
		Message: "Cropped " + strconv.Itoa(result.Cropped) + " pages. The trimmed margins are hidden, " +
			"not deleted, so the crop can be undone.",
	}, nil
}

// optionalRange parses a page range that may be empty, where empty means
// every page.
func optionalRange(raw string, pages int) ([]int, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}

	return pdf.ParsePageRange(raw, pages)
}
