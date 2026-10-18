package tools

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/AlexGalhardo/vukem/z"

	"github.com/AlexGalhardo/vukem-tools/app/abilities"
	"github.com/AlexGalhardo/vukem-tools/internal/jobs"
	"github.com/AlexGalhardo/vukem-tools/internal/pdf"
)

// ---------------------------------------------------------------------------
// S6.6 Edit
// ---------------------------------------------------------------------------

// PDFWatermarkOptions is the input of a watermark.
type PDFWatermarkOptions struct {
	// Text is the watermark. Unaccented Latin letters, digits and
	// punctuation only, because the standard PDF fonts are the only ones
	// this tool embeds.
	Text string `json:"text" validate:"required,trim,max=200" doc:"The text to stamp. Unaccented Latin letters, digits and punctuation."`
	// Pages is the range to stamp. Empty stamps every page.
	Pages string `json:"pages" validate:"trim,max=512" doc:"Pages to stamp, as 1-3,5. Empty stamps every page."`
	// Position is one of the nine named positions.
	Position string `json:"position" validate:"trim,max=16" doc:"top-left to bottom-right, or center. Default center."`
	// Size is the font size in points. 0 picks one from the page's width.
	Size float64 `json:"size" validate:"min=0,max=144" doc:"Font size in points, 4 to 144. 0 picks a size from the page width."`
	// Rotate is the angle in degrees anticlockwise. 45 is the usual
	// diagonal.
	Rotate float64 `json:"rotate" validate:"min=-360,max=360" doc:"Angle in degrees anticlockwise. 45 is the usual diagonal."`
	// Opacity is 0 to 1.
	Opacity float64 `json:"opacity" validate:"min=0,max=1" doc:"0 invisible to 1 opaque. Default 0.18."`
	// Gray is the ink, 0 black to 1 white.
	Gray float64 `json:"gray" validate:"min=0,max=1" doc:"0 black to 1 white. Default 0."`
	// Bold uses the bold face, for a watermark over text.
	Bold bool `json:"bold" doc:"Use the bold face."`
}

// Normalise implements the option contract.
func (o PDFWatermarkOptions) Normalise() (PDFWatermarkOptions, error) {
	if o.Text = strings.TrimSpace(o.Text); o.Text == "" {
		return PDFWatermarkOptions{}, z.FieldError("options.text", "Enter the text to stamp.")
	}

	if len(o.Text) > pdf.MaxStampTextBytes {
		return PDFWatermarkOptions{}, z.FieldError("options.text",
			"The text may be at most "+strconv.Itoa(pdf.MaxStampTextBytes)+" characters.")
	}

	if o.Position = strings.ToLower(strings.TrimSpace(o.Position)); o.Position == "" {
		o.Position = string(pdf.Center)
	}

	if !pdf.Position(o.Position).Valid() {
		return PDFWatermarkOptions{}, z.FieldError("options.position",
			"The position is one of: "+strings.Join(pdf.Positions(), ", ")+".")
	}

	if o.Size != 0 && (o.Size < pdf.MinStampSize || o.Size > pdf.MaxStampSize) {
		return PDFWatermarkOptions{}, z.FieldError("options.size",
			"The size must be between "+strconv.Itoa(pdf.MinStampSize)+" and "+
				strconv.Itoa(pdf.MaxStampSize)+" points.")
	}

	if o.Opacity < 0 || o.Opacity > 1 {
		return PDFWatermarkOptions{}, z.FieldError("options.opacity", "The opacity must be between 0 and 1.")
	}

	if o.Gray < 0 || o.Gray > 1 {
		return PDFWatermarkOptions{}, z.FieldError("options.gray", "The ink must be between 0 and 1.")
	}

	if o.Rotate < -360 || o.Rotate > 360 {
		return PDFWatermarkOptions{}, z.FieldError("options.rotate", "The angle must be between -360 and 360.")
	}

	o.Pages = strings.TrimSpace(o.Pages)

	return o, nil
}

// PDFPageNumberOptions is the input of a page-number run.
type PDFPageNumberOptions struct {
	// Format is the label, with {n} for the number and {total} for the
	// count.
	Format string `json:"format" validate:"trim,max=200" doc:"The label, with {n} for the number and {total} for the count. Default {n}."`
	// Pages is the range to number. Empty numbers every page.
	Pages string `json:"pages" validate:"trim,max=512" doc:"Pages to number, as 2-z. Empty numbers every page."`
	// StartAt is the number on the first numbered page.
	StartAt int `json:"start_at" validate:"min=0,max=100000" doc:"The number on the first numbered page. Default 1."`
	// Position is one of the nine named positions.
	Position string `json:"position" validate:"trim,max=16" doc:"top-left to bottom-right. Default bottom-center."`
	// Size is the font size in points.
	Size float64 `json:"size" validate:"min=0,max=144" doc:"Font size in points, 4 to 144. Default 10."`
}

// Normalise implements the option contract.
func (o PDFPageNumberOptions) Normalise() (PDFPageNumberOptions, error) {
	if o.Format = strings.TrimSpace(o.Format); o.Format == "" {
		o.Format = "{n}"
	}

	if len(o.Format) > pdf.MaxStampTextBytes {
		return PDFPageNumberOptions{}, z.FieldError("options.format",
			"The label may be at most "+strconv.Itoa(pdf.MaxStampTextBytes)+" characters.")
	}

	if o.Position = strings.ToLower(strings.TrimSpace(o.Position)); o.Position == "" {
		o.Position = string(pdf.BottomCenter)
	}

	if !pdf.Position(o.Position).Valid() {
		return PDFPageNumberOptions{}, z.FieldError("options.position",
			"The position is one of: "+strings.Join(pdf.Positions(), ", ")+".")
	}

	if o.Size == 0 {
		o.Size = pdf.DefaultStampSize
	}

	if o.Size < pdf.MinStampSize || o.Size > pdf.MaxStampSize {
		return PDFPageNumberOptions{}, z.FieldError("options.size",
			"The size must be between "+strconv.Itoa(pdf.MinStampSize)+" and "+
				strconv.Itoa(pdf.MaxStampSize)+" points.")
	}

	if o.StartAt == 0 {
		o.StartAt = 1
	}

	if o.StartAt < 1 {
		return PDFPageNumberOptions{}, z.FieldError("options.start_at", "The first number must be 1 or more.")
	}

	o.Pages = strings.TrimSpace(o.Pages)

	return o, nil
}

// pdfStampTools add a watermark or page numbers (S6.6).
func pdfStampTools(deps *pdfDeps) []jobs.Tool {
	return []jobs.Tool{
		&pdfTool[PDFWatermarkOptions]{
			deps: deps, name: NamePDFWatermark, ability: abilities.PDFEdit,
			timeout: pdfStampTimeout, accepted: pdfInputTypes,
			engines: []string{pdf.EngineQPDF}, maxInputs: 1,
			work: watermarkPDF,
		},
		&pdfTool[PDFPageNumberOptions]{
			deps: deps, name: NamePDFPageNumbers, ability: abilities.PDFEdit,
			timeout: pdfStampTimeout, accepted: pdfInputTypes,
			engines: []string{pdf.EngineQPDF}, maxInputs: 1,
			work: numberPDFPages,
		},
	}
}

// watermarkPDF stamps text across pages.
func watermarkPDF(ctx context.Context, w pdfWork[PDFWatermarkOptions]) (pdfResult, error) {
	in := w.Inputs[0]

	doc, err := w.Engines.Inspect(ctx, w.Dir, in.Path)
	if err != nil {
		return pdfResult{}, err
	}

	pages, err := optionalRange(w.Options.Pages, doc.Count)
	if err != nil {
		return pdfResult{}, err
	}

	out := filepath.Join(w.Dir, "watermarked.pdf")

	stamped, err := w.Engines.Watermark(ctx, pdf.WatermarkRequest{
		Dir:      w.Dir,
		Input:    in.Path,
		Output:   out,
		Text:     w.Options.Text,
		Pages:    pages,
		Position: pdf.Position(w.Options.Position),
		Size:     w.Options.Size,
		Rotate:   w.Options.Rotate,
		Opacity:  w.Options.Opacity,
		Gray:     w.Options.Gray,
		Bold:     w.Options.Bold,
	})
	if err != nil {
		return pdfResult{}, err
	}

	return pdfResult{
		Outputs: []pdfOutput{{Path: out, Name: in.Stem() + "-watermarked.pdf", MIME: mimePDF}},
		Message: "Stamped " + strconv.Itoa(stamped) + " of " + strconv.Itoa(doc.Count) + " pages. " +
			"A watermark is a visible mark, not protection: it can be removed by anybody with a " +
			"PDF editor.",
	}, nil
}

// numberPDFPages stamps page numbers.
func numberPDFPages(ctx context.Context, w pdfWork[PDFPageNumberOptions]) (pdfResult, error) {
	in := w.Inputs[0]

	doc, err := w.Engines.Inspect(ctx, w.Dir, in.Path)
	if err != nil {
		return pdfResult{}, err
	}

	pages, err := optionalRange(w.Options.Pages, doc.Count)
	if err != nil {
		return pdfResult{}, err
	}

	out := filepath.Join(w.Dir, "numbered.pdf")

	stamped, err := w.Engines.NumberPages(ctx, pdf.PageNumberRequest{
		Dir:      w.Dir,
		Input:    in.Path,
		Output:   out,
		Format:   w.Options.Format,
		Pages:    pages,
		StartAt:  w.Options.StartAt,
		Position: pdf.Position(w.Options.Position),
		Size:     w.Options.Size,
	})
	if err != nil {
		return pdfResult{}, err
	}

	return pdfResult{
		Outputs: []pdfOutput{{Path: out, Name: in.Stem() + "-numbered.pdf", MIME: mimePDF}},
		Message: "Numbered " + strconv.Itoa(stamped) + " of " + strconv.Itoa(doc.Count) + " pages.",
	}, nil
}
