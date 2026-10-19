package apiv1

import (
	"github.com/AlexGalhardo/vukem/web"

	"github.com/AlexGalhardo/vukem-tools/app/tools"
)

// The archive submit routes.
//
// Two operations, one per direction, for the reason app/tools/archive.go
// gives: the directions take different inputs, so they cannot share a request
// body either. Creating goes through submitMany, which the PDF merge already
// needed; extracting goes through submitOne. Both helpers live in pdf.go,
// where the first domain to need them put them, and neither is PDF-specific.

// SubmitArchiveCreate is the body of POST /api/v1/tools/archive-create.
type SubmitArchiveCreate struct {
	// FileIDs are the uploads to pack, in the order they should appear in the
	// archive.
	FileIDs        []string                   `json:"file_ids"        validate:"required,min=1,max=64,dive,required,trim" doc:"The uploads to pack, in order. One to sixty-four."`
	IdempotencyKey string                     `json:"idempotency_key" validate:"trim,max=128"                             doc:"Repeat a submission safely: the same key returns the same conversion."`
	Options        tools.ArchiveCreateOptions `json:"options"                                                             doc:"The format, the compression level and the archive's name. Every field is optional."`
}

// ArchiveCreate packs uploads into an archive: POST
// /api/v1/tools/archive-create.
//
// The one rule that needs the options and the file list together is applied
// here, before the conversion is accepted: gz holds a single stream, so asking
// for it with several files is a 422 on the format rather than a worker spent
// to say the same thing.
func (h *Conversions) ArchiveCreate(c *web.Ctx, in SubmitArchiveCreate) (ConversionView, error) {
	normalised, err := in.Options.Normalise()
	if err != nil {
		return ConversionView{}, err
	}

	if err := tools.CheckArchiveCreateFiles(normalised, len(in.FileIDs)); err != nil {
		return ConversionView{}, err
	}

	return submitMany(h, c, tools.NameArchiveCreate, in.FileIDs, in.IdempotencyKey, normalised)
}

// SubmitArchiveExtract is the body of POST /api/v1/tools/archive-extract.
type SubmitArchiveExtract struct {
	FileID         string                      `json:"file_id"         validate:"required,trim" doc:"The id of an upload of yours: a ZIP, TAR, TAR.GZ or GZ."`
	IdempotencyKey string                      `json:"idempotency_key" validate:"trim,max=128"  doc:"Repeat a submission safely."`
	Options        tools.ArchiveExtractOptions `json:"options"                                  doc:"Extracting has no options: the archive says what it is."`
}

// ArchiveExtract unpacks an archive: POST /api/v1/tools/archive-extract.
func (h *Conversions) ArchiveExtract(c *web.Ctx, in SubmitArchiveExtract) (ConversionView, error) {
	return submitOne(h, c, tools.NameArchiveExtract, in.FileID, in.IdempotencyKey, in.Options)
}
