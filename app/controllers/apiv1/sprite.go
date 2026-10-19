package apiv1

import (
	"github.com/AlexGalhardo/vukem/web"

	"github.com/AlexGalhardo/vukem-tools/app/tools"
	"github.com/AlexGalhardo/vukem-tools/internal/spritesheet"
)

// The sprite sheet submit routes, and the one endpoint that publishes the
// metadata formats so the interface does not hold a list of its own.

// SubmitSpriteSplit is the body of POST /api/v1/tools/sprite-split.
type SubmitSpriteSplit struct {
	FileID         string                   `json:"file_id"         validate:"required,trim" doc:"The id of an upload of yours: the sheet to cut."`
	IdempotencyKey string                   `json:"idempotency_key" validate:"trim,max=128"  doc:"Repeat a submission safely: the same key returns the same conversion."`
	Options        tools.SpriteSplitOptions `json:"options"                                  doc:"How to find the frames, and which metadata to write."`
}

// SpriteSplit cuts a sheet into frames: POST /api/v1/tools/sprite-split.
func (h *Conversions) SpriteSplit(c *web.Ctx, in SubmitSpriteSplit) (ConversionView, error) {
	return submitOne(h, c, tools.NameSpriteSplit, in.FileID, in.IdempotencyKey, in.Options)
}

// SubmitSpriteBuild is the body of POST /api/v1/tools/sprite-build.
type SubmitSpriteBuild struct {
	FileID         string                   `json:"file_id"         validate:"required,trim" doc:"The id of an upload of yours: a ZIP of frames."`
	IdempotencyKey string                   `json:"idempotency_key" validate:"trim,max=128"  doc:"Repeat a submission safely."`
	Options        tools.SpriteBuildOptions `json:"options"                                  doc:"How to pack the frames, and which metadata to write."`
}

// SpriteBuild packs frames into a sheet: POST /api/v1/tools/sprite-build.
func (h *Conversions) SpriteBuild(c *web.Ctx, in SubmitSpriteBuild) (ConversionView, error) {
	return submitOne(h, c, tools.NameSpriteBuild, in.FileID, in.IdempotencyKey, in.Options)
}

// SpriteFormat is one metadata format, as the API publishes it.
type SpriteFormat struct {
	Name      string `json:"name"      doc:"The value to send in options.metadata."`
	Extension string `json:"extension" doc:"The file's extension, with the dot."`
	MIME      string `json:"mime"      doc:"The media type the result is served as."`
	Imports   string `json:"imports"   doc:"One sentence saying what reads this format."`
	Trim      bool   `json:"trim"      doc:"Whether the format can describe a trimmed frame. CSS cannot."`
}

// SpriteCapabilities is the answer to the sprite capability endpoint.
type SpriteCapabilities struct {
	Formats []SpriteFormat `json:"formats" doc:"Every metadata format, in the order the interface offers them."`
	Modes   []string       `json:"modes"   doc:"How sprite-split finds frames."`
	Layouts []string       `json:"layouts" doc:"How sprite-build arranges them."`
	// MaxFormats is published rather than left to the client to remember.
	MaxFormats int `json:"max_formats" doc:"How many metadata formats one conversion may ask for."`
	MaxFrames  int `json:"max_frames"  doc:"The most frames one conversion produces."`
	MaxSize    int `json:"max_size"    doc:"The largest sheet dimension, in pixels."`
}

// SpriteCapabilities answers GET /api/v1/tools/sprite-split/formats.
//
// Published rather than hardcoded in the interface for the same reason the
// media capabilities are: a list that lives in two places is a list that will
// disagree with itself, and this one carries a sentence per format that the
// person choosing between five of them actually needs.
func (*Conversions) SpriteCapabilities(_ *web.Ctx, _ struct{}) (SpriteCapabilities, error) {
	formats := spritesheet.MetaFormats()

	out := SpriteCapabilities{
		Formats:    make([]SpriteFormat, 0, len(formats)),
		Modes:      tools.SpriteModes(),
		Layouts:    tools.SpriteLayouts(),
		MaxFormats: spritesheet.MaxMetaFormats,
		MaxFrames:  spritesheet.MaxFrames,
		MaxSize:    spritesheet.MaxDimension,
	}

	for _, f := range formats {
		out.Formats = append(out.Formats, SpriteFormat{
			Name:      string(f.Name),
			Extension: f.Extension,
			MIME:      f.MIME,
			Imports:   f.Imports,
			Trim:      f.Trim,
		})
	}

	return out, nil
}
