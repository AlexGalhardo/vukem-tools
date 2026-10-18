package tools

import (
	"context"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/AlexGalhardo/vukem/z"

	"github.com/AlexGalhardo/vukem-tools/app/abilities"
	"github.com/AlexGalhardo/vukem-tools/internal/jobs"
	"github.com/AlexGalhardo/vukem-tools/internal/pdf"
)

// ---------------------------------------------------------------------------
// S6.2 Convert to PDF, S6.3 Convert from PDF
// ---------------------------------------------------------------------------

// ImagesToPDFOptions is the input of an images-to-PDF conversion.
type ImagesToPDFOptions struct {
	// PageSize is a4, a3, a5, letter or legal. Ignored by the exact fit.
	PageSize string `json:"page_size" validate:"trim,max=16" doc:"a4, a3, a5, letter or legal. Default a4."`
	// Orientation is auto, portrait or landscape.
	Orientation string `json:"orientation" validate:"trim,max=16" doc:"auto turns each page to match its image. Also portrait or landscape. Default auto."`
	// Fit is contain, fill or exact.
	Fit string `json:"fit" validate:"trim,max=16" doc:"contain fits inside the margins, fill covers the page, exact makes the page the image's own size. Default contain."`
	// Margin is the space around the image in points.
	Margin float64 `json:"margin" validate:"min=0,max=144" doc:"Points of space around the image, 0 to 144. Default 0."`
	// Quality is the JPEG quality used for an input that is not already
	// JPEG.
	Quality int `json:"quality" validate:"min=0,max=100" doc:"Quality for images that have to be converted, 1 to 100. Default 90. A JPEG input is never re-encoded."`
}

// Normalise implements the option contract.
func (o ImagesToPDFOptions) Normalise() (ImagesToPDFOptions, error) {
	if o.PageSize = strings.ToLower(strings.TrimSpace(o.PageSize)); o.PageSize == "" {
		o.PageSize = "a4"
	}

	if _, ok := pdf.PageSize(o.PageSize); !ok {
		return ImagesToPDFOptions{}, z.FieldError("options.page_size",
			"The page size is one of: "+strings.Join(pdf.PageSizeNames(), ", ")+".")
	}

	if o.Orientation = strings.ToLower(strings.TrimSpace(o.Orientation)); o.Orientation == "" {
		o.Orientation = string(pdf.OrientAuto)
	}

	if !pdf.Orientation(o.Orientation).Valid() {
		return ImagesToPDFOptions{}, z.FieldError("options.orientation",
			"The orientation is one of: "+strings.Join(pdf.Orientations(), ", ")+".")
	}

	if o.Fit = strings.ToLower(strings.TrimSpace(o.Fit)); o.Fit == "" {
		o.Fit = string(pdf.FitContain)
	}

	if !pdf.Fit(o.Fit).Valid() {
		return ImagesToPDFOptions{}, z.FieldError("options.fit",
			"The fit is one of: "+strings.Join(pdf.Fits(), ", ")+".")
	}

	if o.Margin < 0 || o.Margin > pdf.MaxImageMargin {
		return ImagesToPDFOptions{}, z.FieldError("options.margin",
			"The margin must be between 0 and "+strconv.Itoa(pdf.MaxImageMargin)+" points.")
	}

	if o.Quality == 0 {
		o.Quality = 90
	}

	if o.Quality < 1 || o.Quality > 100 {
		return ImagesToPDFOptions{}, z.FieldError("options.quality", "The quality must be between 1 and 100.")
	}

	return o, nil
}

// PDFToImageOptions is the input of a PDF-to-image conversion.
type PDFToImageOptions struct {
	// Format is png or jpeg.
	Format string `json:"format" validate:"trim,max=8" doc:"png or jpeg. Default png."`
	// DPI is the resolution.
	DPI int `json:"dpi" validate:"min=0,max=600" doc:"Resolution, 36 to 600. Default 150."`
	// Quality is the JPEG quality, used only by the jpeg format.
	Quality int `json:"quality" validate:"min=0,max=100" doc:"JPEG quality, 1 to 100. Default 85."`
	// Pages is the range to render. Empty renders every page.
	Pages string `json:"pages" validate:"trim,max=512" doc:"Pages to render, as 1-3,5. Empty renders every page."`
}

// Normalise implements the option contract.
func (o PDFToImageOptions) Normalise() (PDFToImageOptions, error) {
	if o.Format = strings.ToLower(strings.TrimSpace(o.Format)); o.Format == "" {
		o.Format = string(pdf.RasterPNG)
	}

	if !pdf.RasterFormat(o.Format).Valid() {
		return PDFToImageOptions{}, z.FieldError("options.format",
			"The format is one of: "+strings.Join(pdf.RasterFormats(), ", ")+".")
	}

	if o.DPI == 0 {
		o.DPI = pdf.DefaultDPI
	}

	if o.DPI < pdf.MinDPI || o.DPI > pdf.MaxDPI {
		return PDFToImageOptions{}, z.FieldError("options.dpi",
			"The resolution must be between "+strconv.Itoa(pdf.MinDPI)+" and "+
				strconv.Itoa(pdf.MaxDPI)+" DPI.")
	}

	if o.Quality != 0 && (o.Quality < 1 || o.Quality > 100) {
		return PDFToImageOptions{}, z.FieldError("options.quality", "The quality must be between 1 and 100.")
	}

	o.Pages = strings.TrimSpace(o.Pages)

	return o, nil
}

// PDFToPDFAOptions is the input of a PDF/A conversion.
type PDFToPDFAOptions struct {
	// Part is the conformance level: 1, 2 or 3, all level B.
	Part int `json:"part" validate:"min=0,max=3" doc:"PDF/A conformance: 1 for 1b, 2 for 2b, 3 for 3b. Default 2."`
}

// Normalise implements the option contract.
func (o PDFToPDFAOptions) Normalise() (PDFToPDFAOptions, error) {
	if o.Part == 0 {
		o.Part = int(pdf.PDFA2b)
	}

	if !pdf.ArchivePart(o.Part).Valid() {
		return PDFToPDFAOptions{}, z.FieldError("options.part", "PDF/A conformance is 1, 2 or 3.")
	}

	return o, nil
}

// PDFToTextOptions is the input of a text or Markdown extraction.
type PDFToTextOptions struct {
	// Format is text or markdown.
	Format string `json:"format" validate:"trim,max=16" doc:"text or markdown. Default text."`
	// Layout keeps the page's column spacing, for the text format. Markdown
	// always reads the layout and then undoes the wrapping.
	Layout bool `json:"layout" doc:"Keep the page's column spacing, for the text format."`
	// PageRules writes a horizontal rule between pages, for Markdown.
	PageRules bool `json:"page_rules" doc:"Write a rule between pages, for the markdown format."`
	// Pages is a continuous range. Empty reads the whole document.
	Pages string `json:"pages" validate:"trim,max=512" doc:"One continuous range such as 1-20. Empty reads the whole document."`
}

// The text output formats.
const (
	textFormatPlain    = "text"
	textFormatMarkdown = "markdown"
)

// Normalise implements the option contract.
func (o PDFToTextOptions) Normalise() (PDFToTextOptions, error) {
	if o.Format = strings.ToLower(strings.TrimSpace(o.Format)); o.Format == "" {
		o.Format = textFormatPlain
	}

	if !slices.Contains([]string{textFormatPlain, textFormatMarkdown}, o.Format) {
		return PDFToTextOptions{}, z.FieldError("options.format", "The format is either text or markdown.")
	}

	o.Pages = strings.TrimSpace(o.Pages)

	return o, nil
}

// pdfConversionTools convert to and from PDF (S6.2, S6.3).
func pdfConversionTools(deps *pdfDeps) []jobs.Tool {
	return []jobs.Tool{
		&pdfTool[ImagesToPDFOptions]{
			deps: deps, name: NameImagesToPDF, ability: abilities.PDFConvert,
			timeout: pdfRasterTimeout, accepted: imageInputTypes,
			engines: []string{pdf.EngineVips}, maxInputs: pdf.MaxImagesPerPDF,
			work: imagesToPDF,
		},
		&pdfTool[PDFToImageOptions]{
			deps: deps, name: NamePDFToImage, ability: abilities.PDFConvert,
			timeout: pdfRasterTimeout, accepted: pdfInputTypes,
			engines: []string{pdf.EngineQPDF, pdf.EnginePDFToPPM}, maxInputs: 1,
			work: pdfToImages,
		},
		&pdfTool[PDFToPDFAOptions]{
			deps: deps, name: NamePDFToPDFA, ability: abilities.PDFConvert,
			timeout: pdfGhostTimeout, accepted: pdfInputTypes,
			engines: []string{pdf.EngineQPDF, pdf.EngineGhostscript}, maxInputs: 1,
			work: pdfToPDFA,
		},
		&pdfTool[PDFToTextOptions]{
			deps: deps, name: NamePDFToText, ability: abilities.PDFConvert,
			timeout: pdfStampTimeout, accepted: pdfInputTypes,
			engines: []string{pdf.EngineQPDF, pdf.EnginePDFToText}, maxInputs: 1,
			work: pdfToText,
		},
	}
}

// imagesToPDF binds the attached images into one document.
func imagesToPDF(ctx context.Context, w pdfWork[ImagesToPDFOptions]) (pdfResult, error) {
	paths := make([]string, 0, len(w.Inputs))
	isJPEG := make([]bool, 0, len(w.Inputs))

	for _, in := range w.Inputs {
		paths = append(paths, in.Path)
		// From the type detected at upload, never from the extension: a JPEG
		// is embedded untouched, so claiming a PNG was one would put the
		// wrong bytes in a DCTDecode stream.
		isJPEG = append(isJPEG, in.MIME == "image/jpeg")
	}

	out := filepath.Join(w.Dir, "images.pdf")

	result, err := w.Engines.FromImages(ctx, pdf.ImagesRequest{
		Dir:         w.Dir,
		Inputs:      paths,
		JPEG:        isJPEG,
		Output:      out,
		PageSize:    w.Options.PageSize,
		Orientation: pdf.Orientation(w.Options.Orientation),
		Fit:         pdf.Fit(w.Options.Fit),
		Margin:      w.Options.Margin,
		Quality:     w.Options.Quality,
		Progress:    func(done, total int) { _ = w.Report(done*90/total, "placing") },
	})
	if err != nil {
		return pdfResult{}, err
	}

	message := strconv.Itoa(result.Pages) + " images, one per page."
	if result.Reencoded > 0 {
		message += " " + strconv.Itoa(result.Reencoded) +
			" had to be converted to JPEG; the ones that were already JPEG were embedded untouched."
	} else {
		message += " None was re-encoded."
	}

	return pdfResult{
		Outputs: []pdfOutput{{Path: out, Name: w.Inputs[0].Stem() + ".pdf", MIME: mimePDF}},
		Message: message,
	}, nil
}

// pdfToImages renders pages to images, one file per page.
func pdfToImages(ctx context.Context, w pdfWork[PDFToImageOptions]) (pdfResult, error) {
	in := w.Inputs[0]

	doc, err := w.Engines.Inspect(ctx, w.Dir, in.Path)
	if err != nil {
		return pdfResult{}, err
	}

	pages, err := optionalRange(w.Options.Pages, doc.Count)
	if err != nil {
		return pdfResult{}, err
	}

	format := pdf.RasterFormat(w.Options.Format)

	rendered, err := w.Engines.Rasterise(ctx, pdf.RasterRequest{
		Dir:         w.Dir,
		Input:       in.Path,
		Pages:       pages,
		SourcePages: doc.Count,
		DPI:         w.Options.DPI,
		Format:      format,
		JPEGQuality: w.Options.Quality,
		Prefix:      "page",
		Progress:    func(done, total int) { _ = w.Report(done*100/total, "rendering") },
	})
	if err != nil {
		return pdfResult{}, err
	}

	mime := "image/png"
	if format == pdf.RasterJPEG {
		mime = "image/jpeg"
	}

	outputs := make([]pdfOutput, 0, len(rendered))
	stem := in.Stem()

	for _, page := range rendered {
		outputs = append(outputs, pdfOutput{
			Path: page.Path,
			Name: stem + "-page-" + strconv.Itoa(page.Page) + format.Extension(),
			MIME: mime,
		})
	}

	return pdfResult{
		Outputs: outputs,
		Message: strconv.Itoa(len(outputs)) + " pages at " + strconv.Itoa(w.Options.DPI) + " DPI.",
	}, nil
}

// pdfToPDFA converts to an archival PDF and reports its conformance.
func pdfToPDFA(ctx context.Context, w pdfWork[PDFToPDFAOptions]) (pdfResult, error) {
	in := w.Inputs[0]

	doc, err := w.Engines.Inspect(ctx, w.Dir, in.Path)
	if err != nil {
		return pdfResult{}, err
	}

	out := filepath.Join(w.Dir, "archival.pdf")

	result, err := w.Engines.ToPDFA(ctx, pdf.ArchiveRequest{
		Dir:      w.Dir,
		Input:    in.Path,
		Output:   out,
		Part:     pdf.ArchivePart(w.Options.Part),
		Pages:    doc.Count,
		Progress: func(page, total int) { _ = w.Report(page*100/max(total, 1), "converting") },
	})
	if err != nil {
		return pdfResult{}, err
	}

	level := "PDF/A-" + strconv.Itoa(w.Options.Part) + "b"
	message := "Converted to " + level + ", and the converter reports it as conformant."

	if !result.Conformant {
		// The content was kept and the conformance was not, which is the
		// policy this tool runs under. Saying so is the whole value of the
		// report: a conformant file with a missing logo would be worse.
		message = "Converted to " + level + ", but the converter reports it as not fully conformant: " +
			strings.Join(result.Complaints, "; ") +
			". The page content was kept rather than dropped to reach conformance."
	}

	return pdfResult{
		Outputs: []pdfOutput{{Path: out, Name: in.Stem() + "-" + level + ".pdf", MIME: mimePDF}},
		Message: message,
	}, nil
}

// pdfToText extracts text or Markdown.
func pdfToText(ctx context.Context, w pdfWork[PDFToTextOptions]) (pdfResult, error) {
	in := w.Inputs[0]

	doc, err := w.Engines.Inspect(ctx, w.Dir, in.Path)
	if err != nil {
		return pdfResult{}, err
	}

	pages, err := optionalRange(w.Options.Pages, doc.Count)
	if err != nil {
		return pdfResult{}, err
	}

	if w.Options.Format == textFormatMarkdown {
		out := filepath.Join(w.Dir, "document.md")

		result, err := w.Engines.ToMarkdown(ctx, pdf.MarkdownRequest{
			Dir:         w.Dir,
			Input:       in.Path,
			Output:      out,
			Pages:       pages,
			SourcePages: doc.Count,
			PageRules:   w.Options.PageRules,
		})
		if err != nil {
			return pdfResult{}, err
		}

		headings := result.Headings[0] + result.Headings[1] + result.Headings[2]

		return pdfResult{
			Outputs: []pdfOutput{{Path: out, Name: in.Stem() + ".md", MIME: mimeMarkdown}},
			Message: strconv.Itoa(headings) + " headings, " + strconv.Itoa(result.ListItems) +
				" list items and " + strconv.Itoa(result.Paragraphs) + " paragraphs recognised. " +
				"This is a text conversion with heading and list heuristics, not a layout " +
				"reconstruction: a document that marks its headings only by font size comes out " +
				"as paragraphs.",
		}, nil
	}

	out := filepath.Join(w.Dir, "document.txt")

	err = w.Engines.ExtractText(ctx, pdf.TextRequest{
		Dir:         w.Dir,
		Input:       in.Path,
		Output:      out,
		Layout:      w.Options.Layout,
		Pages:       pages,
		SourcePages: doc.Count,
	})
	if err != nil {
		return pdfResult{}, err
	}

	return pdfResult{
		Outputs: []pdfOutput{{Path: out, Name: in.Stem() + ".txt", MIME: mimePlain}},
		Message: "Text extracted from " + strconv.Itoa(doc.Count) + " pages. A scanned document " +
			"has no text to extract; run OCR on it first.",
	}, nil
}
