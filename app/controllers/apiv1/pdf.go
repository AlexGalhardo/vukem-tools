package apiv1

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"

	"github.com/AlexGalhardo/vukem/database"
	"github.com/AlexGalhardo/vukem/errs"
	"github.com/AlexGalhardo/vukem/web"
	"github.com/AlexGalhardo/vukem/z"

	"github.com/AlexGalhardo/vukem-tools/app/models"
	"github.com/AlexGalhardo/vukem-tools/app/tools"
	"github.com/AlexGalhardo/vukem-tools/internal/detect"
	"github.com/AlexGalhardo/vukem-tools/internal/jobs"
)

// The PDF submit routes.
//
// There is one route and one input type per tool rather than one route
// carrying opaque JSON, for the reason SubmitPixelSVG gives: the typed-handler
// validator derives its schema from the Go type, so neither json.RawMessage
// nor map[string]any can be an input field, and the OpenAPI document then
// describes each tool's options exactly instead of saying "an object".
//
// Nineteen tools would be nineteen near-identical handlers if each one
// repeated the body of PixelSVG. They do not: each handler is one line
// calling submitPDF, which is generic over the options type. What is
// duplicated is only the part that has to be — the struct whose fields the
// schema is generated from — and that is the point of the exercise rather
// than boilerplate to be avoided. A reader of the OpenAPI document gets
// nineteen documented option sets; a reader of this file gets nineteen
// declarations and one implementation.

// optionsNormaliser is the contract a tool's options satisfy: they can
// normalise themselves, returning the same concrete type. It is the same
// contract app/tools uses, restated here because a controller must not import
// a tool's internals to know it.
//
// Not PDF-specific despite living in this file: the PDF domain was the first
// to need it, and the archive routes submit through the same helpers.
type optionsNormaliser[T any] interface {
	Normalise() (T, error)
}

// submitOne is the body of every single-input handler.
//
// It normalises the options before anything else, so a bad value is a 422 on
// the right field rather than a conversion that is accepted and then fails
// in a worker where nobody is listening.
func submitOne[O optionsNormaliser[O]](
	h *Conversions,
	c *web.Ctx,
	tool, fileID, key string,
	options O,
) (ConversionView, error) {
	normalised, err := options.Normalise()
	if err != nil {
		return ConversionView{}, err
	}

	encoded, err := json.Marshal(normalised)
	if err != nil {
		return ConversionView{}, err
	}

	return h.submit(c, submission{
		Tool:           tool,
		FileID:         fileID,
		IdempotencyKey: key,
		Options:        encoded,
	})
}

// submitMany is the body of every handler that takes several files: the PDF
// merge and compare, and creating an archive.
//
// It cannot go through submit, which attaches one upload. The inputs are
// attached in the order they were given, inside the conversion's own
// transaction, and the order is the instruction — page one of the merged
// document is page one of the first file.
func submitMany[O optionsNormaliser[O]](
	h *Conversions,
	c *web.Ctx,
	tool string,
	fileIDs []string,
	key string,
	options O,
) (ConversionView, error) {
	userID, err := callerID(c)
	if err != nil {
		return ConversionView{}, err
	}

	normalised, err := options.Normalise()
	if err != nil {
		return ConversionView{}, err
	}

	encoded, err := json.Marshal(normalised)
	if err != nil {
		return ConversionView{}, err
	}

	registered, ok := h.pipe.Tool(tool)
	if !ok {
		return ConversionView{}, errs.NewKey(http.StatusServiceUnavailable, "conversion.tool_unavailable",
			"That tool is not available right now.", nil)
	}

	if err := requireAbility(c, registered.Ability()); err != nil {
		return ConversionView{}, err
	}

	if err := h.withinBudget(c, userID); err != nil {
		return ConversionView{}, err
	}

	// The same upload twice would attach once and then fail, leaving a
	// conversion with fewer inputs than it was given. Refusing it here says
	// what happened.
	if len(slices.Compact(slices.Sorted(slices.Values(fileIDs)))) != len(fileIDs) {
		return ConversionView{}, z.FieldError("file_ids",
			"The same upload is listed twice. Upload it again to use it twice.")
	}

	inputs, bytesIn, err := h.conversionInputs(c, userID, registered, fileIDs)
	if err != nil {
		return ConversionView{}, err
	}

	created, _, err := h.pipe.Submit(c.Context(), jobs.Submission{
		UserID:         userID,
		Tool:           tool,
		Options:        encoded,
		IdempotencyKey: key,
		BytesIn:        bytesIn,
		Attach: func(ctx context.Context, ex database.Executor, conversionID string) error {
			for i := range inputs {
				attached, err := h.files.AttachTx(ctx, ex, userID, inputs[i].ID, conversionID)
				if err != nil {
					return err
				}

				if !attached {
					// Taken by another conversion between the check above and
					// this attach. The whole conversion must not exist, so
					// the transaction rolls back.
					return errInputAlreadyUsed
				}
			}

			return nil
		},
	})
	if err != nil {
		if errors.Is(err, errInputAlreadyUsed) {
			return ConversionView{}, z.FieldError("file_ids",
				"One of those uploads has already been converted. Upload it again to convert it twice.")
		}

		return ConversionView{}, err
	}

	return viewOf(created), nil
}

// conversionInputs looks up the caller's uploads and checks each one's type.
//
// A tool that implements no Accepter takes anything the upload endpoint
// accepted, which is what creating an archive means: the check is skipped
// rather than widened to every type the service knows.
func (h *Conversions) conversionInputs(
	c *web.Ctx,
	userID string,
	tool jobs.Tool,
	fileIDs []string,
) ([]models.File, int64, error) {
	accepted, _ := tool.(jobs.Accepter)

	out := make([]models.File, 0, len(fileIDs))

	var total int64

	for _, id := range fileIDs {
		input, err := h.files.Find(c.Context(), userID, id)
		if err != nil {
			// Someone else's upload, or one that does not exist: the same
			// answer either way, so an id cannot be probed.
			return nil, 0, z.FieldError("file_ids", "One of those uploads does not exist.")
		}

		if accepted != nil && !slices.Contains(accepted.Accepted(), input.MIME) {
			kind, _ := detect.KindOf(input.MIME)
			if kind.Label == "" {
				kind.Label = input.MIME
			}

			return nil, 0, z.FieldError("file_ids",
				"One of those uploads is "+kind.Label+", which this tool cannot read.")
		}

		out = append(out, input)
		total += input.SizeBytes
	}

	return out, total, nil
}

// SubmitPDFMerge is the body of POST /api/v1/tools/pdf-merge.
type SubmitPDFMerge struct {
	// FileIDs are the uploads to join, in the order they should appear.
	FileIDs []string `json:"file_ids" validate:"required,min=2,max=50,dive,required,trim" doc:"The uploads to join, in order. Two to fifty."`
	// IdempotencyKey makes a repeated submission return the first
	// conversion instead of starting a second one.
	IdempotencyKey string `json:"idempotency_key" validate:"trim,max=128" doc:"Repeat a submission safely: the same key returns the same conversion."`
	// Options has no fields: the order of file_ids is the only instruction.
	Options tools.PDFMergeOptions `json:"options" doc:"Merging has no options; the order of file_ids is the order of the pages."`
}

// PDFMerge joins PDFs: POST /api/v1/tools/pdf-merge.
func (h *Conversions) PDFMerge(c *web.Ctx, in SubmitPDFMerge) (ConversionView, error) {
	return submitMany(h, c, tools.NamePDFMerge, in.FileIDs, in.IdempotencyKey, in.Options)
}

// SubmitPDFCompare is the body of POST /api/v1/tools/pdf-compare.
type SubmitPDFCompare struct {
	// FileIDs are the two uploads, before and after, in that order.
	FileIDs        []string                `json:"file_ids"        validate:"required,min=2,max=2,dive,required,trim" doc:"Exactly two uploads: the earlier document first."`
	IdempotencyKey string                  `json:"idempotency_key" validate:"trim,max=128"                            doc:"Repeat a submission safely: the same key returns the same conversion."`
	Options        tools.PDFCompareOptions `json:"options"                                                            doc:"Comparison has no options."`
}

// PDFCompare compares two PDFs: POST /api/v1/tools/pdf-compare.
func (h *Conversions) PDFCompare(c *web.Ctx, in SubmitPDFCompare) (ConversionView, error) {
	return submitMany(h, c, tools.NamePDFCompare, in.FileIDs, in.IdempotencyKey, in.Options)
}

// pdfFileInput is the two fields every single-input PDF submission shares.
//
// It is repeated in each struct below rather than embedded, because the
// schema generator reads a struct's own fields: an embedded struct would
// produce a nested object in the OpenAPI document and a nested object in the
// request body, which is not the shape the other tools' routes have.

// SubmitPDFSplit is the body of POST /api/v1/tools/pdf-split.
type SubmitPDFSplit struct {
	FileID         string                `json:"file_id"         validate:"required,trim" doc:"The id of an upload of yours."`
	IdempotencyKey string                `json:"idempotency_key" validate:"trim,max=128"  doc:"Repeat a submission safely."`
	Options        tools.PDFSplitOptions `json:"options"                                  doc:"How to split. Every field is optional."`
}

// PDFSplit splits a PDF: POST /api/v1/tools/pdf-split.
func (h *Conversions) PDFSplit(c *web.Ctx, in SubmitPDFSplit) (ConversionView, error) {
	return submitOne(h, c, tools.NamePDFSplit, in.FileID, in.IdempotencyKey, in.Options)
}

// SubmitPDFExtractPages is the body of POST /api/v1/tools/pdf-extract-pages.
type SubmitPDFExtractPages struct {
	FileID         string                `json:"file_id"         validate:"required,trim" doc:"The id of an upload of yours."`
	IdempotencyKey string                `json:"idempotency_key" validate:"trim,max=128"  doc:"Repeat a submission safely."`
	Options        tools.PDFPagesOptions `json:"options"         validate:"required"      doc:"Which pages to keep."`
}

// PDFExtractPages extracts pages: POST /api/v1/tools/pdf-extract-pages.
func (h *Conversions) PDFExtractPages(c *web.Ctx, in SubmitPDFExtractPages) (ConversionView, error) {
	return submitOne(h, c, tools.NamePDFExtractPages, in.FileID, in.IdempotencyKey, in.Options)
}

// SubmitPDFRemovePages is the body of POST /api/v1/tools/pdf-remove-pages.
type SubmitPDFRemovePages struct {
	FileID         string                `json:"file_id"         validate:"required,trim" doc:"The id of an upload of yours."`
	IdempotencyKey string                `json:"idempotency_key" validate:"trim,max=128"  doc:"Repeat a submission safely."`
	Options        tools.PDFPagesOptions `json:"options"         validate:"required"      doc:"Which pages to remove."`
}

// PDFRemovePages removes pages: POST /api/v1/tools/pdf-remove-pages.
func (h *Conversions) PDFRemovePages(c *web.Ctx, in SubmitPDFRemovePages) (ConversionView, error) {
	return submitOne(h, c, tools.NamePDFRemovePages, in.FileID, in.IdempotencyKey, in.Options)
}

// SubmitPDFOrganise is the body of POST /api/v1/tools/pdf-organise.
type SubmitPDFOrganise struct {
	FileID         string                `json:"file_id"         validate:"required,trim" doc:"The id of an upload of yours."`
	IdempotencyKey string                `json:"idempotency_key" validate:"trim,max=128"  doc:"Repeat a submission safely."`
	Options        tools.PDFPagesOptions `json:"options"         validate:"required"      doc:"The pages in the order they should appear."`
}

// PDFOrganise reorders pages: POST /api/v1/tools/pdf-organise.
func (h *Conversions) PDFOrganise(c *web.Ctx, in SubmitPDFOrganise) (ConversionView, error) {
	return submitOne(h, c, tools.NamePDFOrganise, in.FileID, in.IdempotencyKey, in.Options)
}

// SubmitPDFRotate is the body of POST /api/v1/tools/pdf-rotate.
type SubmitPDFRotate struct {
	FileID         string                 `json:"file_id"         validate:"required,trim" doc:"The id of an upload of yours."`
	IdempotencyKey string                 `json:"idempotency_key" validate:"trim,max=128"  doc:"Repeat a submission safely."`
	Options        tools.PDFRotateOptions `json:"options"                                  doc:"How far to rotate, and which pages."`
}

// PDFRotate rotates pages: POST /api/v1/tools/pdf-rotate.
func (h *Conversions) PDFRotate(c *web.Ctx, in SubmitPDFRotate) (ConversionView, error) {
	return submitOne(h, c, tools.NamePDFRotate, in.FileID, in.IdempotencyKey, in.Options)
}

// SubmitPDFCrop is the body of POST /api/v1/tools/pdf-crop.
type SubmitPDFCrop struct {
	FileID         string               `json:"file_id"         validate:"required,trim" doc:"The id of an upload of yours."`
	IdempotencyKey string               `json:"idempotency_key" validate:"trim,max=128"  doc:"Repeat a submission safely."`
	Options        tools.PDFCropOptions `json:"options"         validate:"required"      doc:"How many points to trim from each edge."`
}

// PDFCrop crops pages: POST /api/v1/tools/pdf-crop.
func (h *Conversions) PDFCrop(c *web.Ctx, in SubmitPDFCrop) (ConversionView, error) {
	return submitOne(h, c, tools.NamePDFCrop, in.FileID, in.IdempotencyKey, in.Options)
}

// SubmitImagesToPDF is the body of POST /api/v1/tools/images-to-pdf.
type SubmitImagesToPDF struct {
	// FileIDs are the images, in the order they should appear.
	FileIDs        []string                 `json:"file_ids"        validate:"required,min=1,max=50,dive,required,trim" doc:"The images, in page order. One to fifty."`
	IdempotencyKey string                   `json:"idempotency_key" validate:"trim,max=128"                             doc:"Repeat a submission safely."`
	Options        tools.ImagesToPDFOptions `json:"options"                                                             doc:"Page size, orientation, fit and margin."`
}

// ImagesToPDF binds images into a PDF: POST /api/v1/tools/images-to-pdf.
func (h *Conversions) ImagesToPDF(c *web.Ctx, in SubmitImagesToPDF) (ConversionView, error) {
	return submitMany(h, c, tools.NameImagesToPDF, in.FileIDs, in.IdempotencyKey, in.Options)
}

// SubmitPDFToImage is the body of POST /api/v1/tools/pdf-to-image.
type SubmitPDFToImage struct {
	FileID         string                  `json:"file_id"         validate:"required,trim" doc:"The id of an upload of yours."`
	IdempotencyKey string                  `json:"idempotency_key" validate:"trim,max=128"  doc:"Repeat a submission safely."`
	Options        tools.PDFToImageOptions `json:"options"                                  doc:"Format, resolution and which pages."`
}

// PDFToImage renders pages to images: POST /api/v1/tools/pdf-to-image.
func (h *Conversions) PDFToImage(c *web.Ctx, in SubmitPDFToImage) (ConversionView, error) {
	return submitOne(h, c, tools.NamePDFToImage, in.FileID, in.IdempotencyKey, in.Options)
}

// SubmitPDFToPDFA is the body of POST /api/v1/tools/pdf-to-pdfa.
type SubmitPDFToPDFA struct {
	FileID         string                 `json:"file_id"         validate:"required,trim" doc:"The id of an upload of yours."`
	IdempotencyKey string                 `json:"idempotency_key" validate:"trim,max=128"  doc:"Repeat a submission safely."`
	Options        tools.PDFToPDFAOptions `json:"options"                                  doc:"Which conformance level to target."`
}

// PDFToPDFA converts to PDF/A: POST /api/v1/tools/pdf-to-pdfa.
func (h *Conversions) PDFToPDFA(c *web.Ctx, in SubmitPDFToPDFA) (ConversionView, error) {
	return submitOne(h, c, tools.NamePDFToPDFA, in.FileID, in.IdempotencyKey, in.Options)
}

// SubmitPDFToText is the body of POST /api/v1/tools/pdf-to-text.
type SubmitPDFToText struct {
	FileID         string                 `json:"file_id"         validate:"required,trim" doc:"The id of an upload of yours."`
	IdempotencyKey string                 `json:"idempotency_key" validate:"trim,max=128"  doc:"Repeat a submission safely."`
	Options        tools.PDFToTextOptions `json:"options"                                  doc:"Text or Markdown, and which pages."`
}

// PDFToText extracts text: POST /api/v1/tools/pdf-to-text.
func (h *Conversions) PDFToText(c *web.Ctx, in SubmitPDFToText) (ConversionView, error) {
	return submitOne(h, c, tools.NamePDFToText, in.FileID, in.IdempotencyKey, in.Options)
}

// SubmitPDFCompress is the body of POST /api/v1/tools/pdf-compress.
type SubmitPDFCompress struct {
	FileID         string                   `json:"file_id"         validate:"required,trim" doc:"The id of an upload of yours."`
	IdempotencyKey string                   `json:"idempotency_key" validate:"trim,max=128"  doc:"Repeat a submission safely."`
	Options        tools.PDFCompressOptions `json:"options"                                  doc:"Which compression level to use."`
}

// PDFCompress compresses a PDF: POST /api/v1/tools/pdf-compress.
func (h *Conversions) PDFCompress(c *web.Ctx, in SubmitPDFCompress) (ConversionView, error) {
	return submitOne(h, c, tools.NamePDFCompress, in.FileID, in.IdempotencyKey, in.Options)
}

// SubmitPDFRepair is the body of POST /api/v1/tools/pdf-repair.
type SubmitPDFRepair struct {
	FileID         string                 `json:"file_id"         validate:"required,trim" doc:"The id of an upload of yours."`
	IdempotencyKey string                 `json:"idempotency_key" validate:"trim,max=128"  doc:"Repeat a submission safely."`
	Options        tools.PDFRepairOptions `json:"options"                                  doc:"Repair has no options."`
}

// PDFRepair rebuilds a damaged PDF: POST /api/v1/tools/pdf-repair.
func (h *Conversions) PDFRepair(c *web.Ctx, in SubmitPDFRepair) (ConversionView, error) {
	return submitOne(h, c, tools.NamePDFRepair, in.FileID, in.IdempotencyKey, in.Options)
}

// SubmitPDFProtect is the body of POST /api/v1/tools/pdf-protect.
//
// The passwords are in the request body, which means they are in whatever
// logs the client keeps and never in this service's: the request body is not
// logged, the options are stored on the conversion row, and that row is
// deleted with the result.
type SubmitPDFProtect struct {
	FileID         string                  `json:"file_id"         validate:"required,trim" doc:"The id of an upload of yours."`
	IdempotencyKey string                  `json:"idempotency_key" validate:"trim,max=128"  doc:"Repeat a submission safely."`
	Options        tools.PDFProtectOptions `json:"options"         validate:"required"      doc:"The passwords and the permissions."`
}

// PDFProtect encrypts a PDF: POST /api/v1/tools/pdf-protect.
func (h *Conversions) PDFProtect(c *web.Ctx, in SubmitPDFProtect) (ConversionView, error) {
	return submitOne(h, c, tools.NamePDFProtect, in.FileID, in.IdempotencyKey, in.Options)
}

// SubmitPDFUnlock is the body of POST /api/v1/tools/pdf-unlock.
type SubmitPDFUnlock struct {
	FileID         string                 `json:"file_id"         validate:"required,trim" doc:"The id of an upload of yours."`
	IdempotencyKey string                 `json:"idempotency_key" validate:"trim,max=128"  doc:"Repeat a submission safely."`
	Options        tools.PDFUnlockOptions `json:"options"         validate:"required"      doc:"The password that opens the document."`
}

// PDFUnlock removes a password: POST /api/v1/tools/pdf-unlock.
func (h *Conversions) PDFUnlock(c *web.Ctx, in SubmitPDFUnlock) (ConversionView, error) {
	return submitOne(h, c, tools.NamePDFUnlock, in.FileID, in.IdempotencyKey, in.Options)
}

// SubmitPDFWatermark is the body of POST /api/v1/tools/pdf-watermark.
type SubmitPDFWatermark struct {
	FileID         string                    `json:"file_id"         validate:"required,trim" doc:"The id of an upload of yours."`
	IdempotencyKey string                    `json:"idempotency_key" validate:"trim,max=128"  doc:"Repeat a submission safely."`
	Options        tools.PDFWatermarkOptions `json:"options"         validate:"required"      doc:"The text and how to draw it."`
}

// PDFWatermark stamps a watermark: POST /api/v1/tools/pdf-watermark.
func (h *Conversions) PDFWatermark(c *web.Ctx, in SubmitPDFWatermark) (ConversionView, error) {
	return submitOne(h, c, tools.NamePDFWatermark, in.FileID, in.IdempotencyKey, in.Options)
}

// SubmitPDFPageNumbers is the body of POST /api/v1/tools/pdf-page-numbers.
type SubmitPDFPageNumbers struct {
	FileID         string                     `json:"file_id"         validate:"required,trim" doc:"The id of an upload of yours."`
	IdempotencyKey string                     `json:"idempotency_key" validate:"trim,max=128"  doc:"Repeat a submission safely."`
	Options        tools.PDFPageNumberOptions `json:"options"                                  doc:"The label, the position and which pages."`
}

// PDFPageNumbers stamps page numbers: POST /api/v1/tools/pdf-page-numbers.
func (h *Conversions) PDFPageNumbers(c *web.Ctx, in SubmitPDFPageNumbers) (ConversionView, error) {
	return submitOne(h, c, tools.NamePDFPageNumbers, in.FileID, in.IdempotencyKey, in.Options)
}

// SubmitWordToPDF is the body of POST /api/v1/tools/word-to-pdf.
type SubmitWordToPDF struct {
	FileID         string                 `json:"file_id"         validate:"required,trim" doc:"The id of an upload of yours."`
	IdempotencyKey string                 `json:"idempotency_key" validate:"trim,max=128"  doc:"Repeat a submission safely."`
	Options        tools.PDFOfficeOptions `json:"options"                                  doc:"No options."`
}

// WordToPDF converts a Word document: POST /api/v1/tools/word-to-pdf.
func (h *Conversions) WordToPDF(c *web.Ctx, in SubmitWordToPDF) (ConversionView, error) {
	return submitOne(h, c, tools.NameWordToPDF, in.FileID, in.IdempotencyKey, in.Options)
}

// SubmitPowerPointToPDF is the body of POST /api/v1/tools/powerpoint-to-pdf.
type SubmitPowerPointToPDF struct {
	FileID         string                 `json:"file_id"         validate:"required,trim" doc:"The id of an upload of yours."`
	IdempotencyKey string                 `json:"idempotency_key" validate:"trim,max=128"  doc:"Repeat a submission safely."`
	Options        tools.PDFOfficeOptions `json:"options"                                  doc:"No options."`
}

// PowerPointToPDF converts a presentation: POST /api/v1/tools/powerpoint-to-pdf.
func (h *Conversions) PowerPointToPDF(c *web.Ctx, in SubmitPowerPointToPDF) (ConversionView, error) {
	return submitOne(h, c, tools.NamePowerPointToPDF, in.FileID, in.IdempotencyKey, in.Options)
}

// SubmitExcelToPDF is the body of POST /api/v1/tools/excel-to-pdf.
type SubmitExcelToPDF struct {
	FileID         string                 `json:"file_id"         validate:"required,trim" doc:"The id of an upload of yours."`
	IdempotencyKey string                 `json:"idempotency_key" validate:"trim,max=128"  doc:"Repeat a submission safely."`
	Options        tools.PDFOfficeOptions `json:"options"                                  doc:"No options."`
}

// ExcelToPDF converts a spreadsheet: POST /api/v1/tools/excel-to-pdf.
func (h *Conversions) ExcelToPDF(c *web.Ctx, in SubmitExcelToPDF) (ConversionView, error) {
	return submitOne(h, c, tools.NameExcelToPDF, in.FileID, in.IdempotencyKey, in.Options)
}

// SubmitPDFOCR is the body of POST /api/v1/tools/pdf-ocr.
type SubmitPDFOCR struct {
	FileID         string              `json:"file_id"         validate:"required,trim" doc:"The id of an upload of yours."`
	IdempotencyKey string              `json:"idempotency_key" validate:"trim,max=128"  doc:"Repeat a submission safely."`
	Options        tools.PDFOCROptions `json:"options"                                  doc:"The languages in the scan."`
}

// PDFOCR makes a scanned PDF searchable: POST /api/v1/tools/pdf-ocr.
func (h *Conversions) PDFOCR(c *web.Ctx, in SubmitPDFOCR) (ConversionView, error) {
	return submitOne(h, c, tools.NamePDFOCR, in.FileID, in.IdempotencyKey, in.Options)
}

// SubmitPDFForms is the body of POST /api/v1/tools/pdf-forms.
type SubmitPDFForms struct {
	FileID         string                `json:"file_id"         validate:"required,trim" doc:"The id of an upload of yours."`
	IdempotencyKey string                `json:"idempotency_key" validate:"trim,max=128"  doc:"Repeat a submission safely."`
	Options        tools.PDFFormsOptions `json:"options"                                  doc:"What to do with the form, and the values to fill."`
}

// PDFForms lists, fills or flattens a form: POST /api/v1/tools/pdf-forms.
func (h *Conversions) PDFForms(c *web.Ctx, in SubmitPDFForms) (ConversionView, error) {
	return submitOne(h, c, tools.NamePDFForms, in.FileID, in.IdempotencyKey, in.Options)
}

// SubmitPDFSign is the body of POST /api/v1/tools/pdf-sign.
type SubmitPDFSign struct {
	FileIDs        []string             `json:"file_ids"        validate:"required,min=2,max=2,dive,required,trim" doc:"Exactly two uploads: the PDF and your .p12 certificate bundle, in either order."`
	IdempotencyKey string               `json:"idempotency_key" validate:"trim,max=128"                            doc:"Repeat a submission safely: the same key returns the same conversion."`
	Options        tools.PDFSignOptions `json:"options"         validate:"required"                                doc:"The bundle's password and what to show beside the signature."`
}

// PDFSign signs a PDF: POST /api/v1/tools/pdf-sign.
func (h *Conversions) PDFSign(c *web.Ctx, in SubmitPDFSign) (ConversionView, error) {
	return submitMany(h, c, tools.NamePDFSign, in.FileIDs, in.IdempotencyKey, in.Options)
}
