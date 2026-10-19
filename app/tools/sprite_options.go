package tools

import (
	"encoding/json"
	"fmt"
	"image/color"
	"slices"
	"strconv"
	"strings"

	"github.com/AlexGalhardo/vukem/z"

	"github.com/AlexGalhardo/vukem-tools/internal/spritesheet"
)

// The sprite tools' public options.
//
// Separate types from the library's on purpose, as every other domain does:
// these are the names a client sends, the bounds it is held to and the words
// the documentation uses, and they have to be able to stay still while the
// library's own options change.

// SpriteSplitOptions is the validated input of a sprite-split conversion.
type SpriteSplitOptions struct {
	// Mode selects how frames are found.
	Mode string `json:"mode" validate:"trim,max=8" doc:"grid or auto. Default grid."`
	// The grid, either as a cell size or as counts.
	CellW   int `json:"cell_w"  validate:"min=0,max=8192" doc:"Cell width in pixels. Give this and cell_h, or cols and rows."`
	CellH   int `json:"cell_h"  validate:"min=0,max=8192" doc:"Cell height in pixels."`
	Cols    int `json:"cols"    validate:"min=0,max=2048" doc:"Column count, as an alternative to the cell size."`
	Rows    int `json:"rows"    validate:"min=0,max=2048" doc:"Row count."`
	Margin  int `json:"margin"  validate:"min=0,max=256"  doc:"Gutter between the sheet's edge and the first cell."`
	Spacing int `json:"spacing" validate:"min=0,max=256"  doc:"Gutter between two neighbouring cells."`
	// Trim shrinks each frame to its opaque bounding box.
	Trim bool `json:"trim" doc:"Shrink each frame to its opaque box, recording the offset so an importer can re-inflate it. Refused with the css metadata format, which has nowhere to put the offset."`
	// Background is the colour auto mode treats as empty.
	Background string `json:"background" validate:"trim,max=9" doc:"Hex colour auto mode treats as empty, e.g. #ff00ff. Empty means transparency."`
	// BackgroundTolerance is how far a pixel may be from Background.
	BackgroundTolerance int `json:"background_tolerance" validate:"min=0,max=255" doc:"How far a pixel may be from the background colour and still count as background."`
	// MinFrame drops a detected component smaller than this.
	MinFrame int `json:"min_frame" validate:"min=0,max=1000000" doc:"Drop a detected frame smaller than this many pixels, as noise. Auto mode only."`
	// Prefix names the frames.
	Prefix string `json:"prefix" validate:"trim,max=64" doc:"Frame name prefix; the nth frame is prefix plus a zero-padded index. Default frame."`
	// Metadata lists the formats to write.
	Metadata []string `json:"metadata" validate:"max=4,dive,trim,max=16" doc:"Metadata formats to write, at most four. GET /api/v1/tools/sprite-split/formats lists them."`
	// Aseprite adds Aseprite's extensions to the JSON formats.
	Aseprite bool `json:"aseprite" doc:"Add Aseprite's per-frame duration and frameTags to the JSON formats."`
	// FrameMS is the per-frame duration those extensions carry.
	FrameMS int `json:"frame_ms" validate:"min=0,max=60000" doc:"Per-frame duration in milliseconds for the Aseprite extensions. 0 means 100."`
}

// SpriteBuildOptions is the validated input of a sprite-build conversion.
type SpriteBuildOptions struct {
	// Layout selects the arrangement.
	Layout string `json:"layout" validate:"trim,max=8" doc:"grid or compact. Default grid, which Godot 4 and Unity import with no metadata file."`
	// The gutters.
	Margin  int `json:"margin"  validate:"min=0,max=256" doc:"Transparent gutter at the sheet's edges."`
	Padding int `json:"padding" validate:"min=0,max=256" doc:"Transparent gutter between two frames, which stops a neighbour's colour bleeding in."`
	Extrude int `json:"extrude" validate:"min=0,max=8"   doc:"Repeat each frame's edge pixels into the padding, which is the fix for the dark halo transparent padding leaves."`
	// MaxSize caps the sheet.
	MaxSize int `json:"max_size" validate:"min=0,max=8192" doc:"Cap the sheet's width and height. 0 means 4096, which every GPU handles as one texture."`
	// POT rounds the sheet up to a power of two.
	POT bool `json:"pot" doc:"Round the sheet up to a power of two on both axes."`
	// Metadata, Aseprite and FrameMS are the same as a split's.
	Metadata []string `json:"metadata" validate:"max=4,dive,trim,max=16" doc:"Metadata formats to write, at most four."`
	Aseprite bool     `json:"aseprite"                                   doc:"Add Aseprite's per-frame duration and frameTags to the JSON formats."`
	FrameMS  int      `json:"frame_ms" validate:"min=0,max=60000"        doc:"Per-frame duration in milliseconds for the Aseprite extensions. 0 means 100."`
}

// SpriteModes returns how sprite-split may be told to find frames, for the
// documentation and the interface.
func SpriteModes() []string {
	return []string{string(spritesheet.ModeGrid), string(spritesheet.ModeAuto)}
}

// SpriteLayouts returns the sheet layouts.
func SpriteLayouts() []string {
	return []string{string(spritesheet.LayoutGrid), string(spritesheet.LayoutCompact)}
}

// SpriteMetaFormats returns the metadata format names.
func SpriteMetaFormats() []string {
	formats := spritesheet.MetaFormats()

	out := make([]string, 0, len(formats))
	for _, f := range formats {
		out = append(out, string(f.Name))
	}

	return out
}

// Normalise fills in every omitted field and refuses a value that is not one
// of the documented ones.
func (in SpriteSplitOptions) Normalise() (SpriteSplitOptions, error) {
	if in.Mode = strings.ToLower(strings.TrimSpace(in.Mode)); in.Mode == "" {
		in.Mode = string(spritesheet.ModeGrid)
	}

	if !slices.Contains(SpriteModes(), in.Mode) {
		return SpriteSplitOptions{}, z.FieldError("options.mode",
			"Unknown mode. Use one of: "+strings.Join(SpriteModes(), ", ")+".")
	}

	// The grid is described one way or the other, never both: they are two
	// ways of saying one thing, and accepting both lets a caller state a
	// contradiction nobody can resolve.
	if in.Mode == string(spritesheet.ModeGrid) {
		bySize := in.CellW > 0 || in.CellH > 0
		byCount := in.Cols > 0 || in.Rows > 0

		switch {
		case bySize && byCount:
			return SpriteSplitOptions{}, z.FieldError("options.cell_w",
				"Give either a cell size or a column and row count, not both.")
		case bySize && (in.CellW <= 0 || in.CellH <= 0):
			return SpriteSplitOptions{}, z.FieldError("options.cell_h",
				"A cell size needs both a width and a height.")
		case byCount && (in.Cols <= 0 || in.Rows <= 0):
			return SpriteSplitOptions{}, z.FieldError("options.rows",
				"A grid by counts needs both a column count and a row count.")
		case !bySize && !byCount:
			return SpriteSplitOptions{}, z.FieldError("options.cell_w",
				"Grid mode needs a cell size, or a column and row count.")
		}
	}

	if _, err := parseSpriteBackground(in.Background); err != nil {
		return SpriteSplitOptions{}, err
	}

	metadata, err := normaliseMetadata(in.Metadata)
	if err != nil {
		return SpriteSplitOptions{}, err
	}

	in.Metadata = metadata

	// Trimming and CSS cannot both be asked for: CSS has nowhere to record
	// the offset, so the frames would be drawn in the wrong place. Caught
	// here rather than by the worker, so the person is told while they are
	// still looking at the form.
	if in.Trim && slices.Contains(in.Metadata, string(spritesheet.MetaCSS)) {
		return SpriteSplitOptions{}, z.FieldError("options.metadata",
			"A CSS sprite sheet has nowhere to record a frame's trim offset. "+
				"Turn trimming off, or choose a different metadata format.")
	}

	in.Prefix = strings.TrimSpace(in.Prefix)

	return in, nil
}

// Normalise fills in every omitted field and refuses a value that is not one
// of the documented ones.
func (in SpriteBuildOptions) Normalise() (SpriteBuildOptions, error) {
	if in.Layout = strings.ToLower(strings.TrimSpace(in.Layout)); in.Layout == "" {
		in.Layout = string(spritesheet.LayoutGrid)
	}

	if !slices.Contains(SpriteLayouts(), in.Layout) {
		return SpriteBuildOptions{}, z.FieldError("options.layout",
			"Unknown layout. Use one of: "+strings.Join(SpriteLayouts(), ", ")+".")
	}

	if in.Extrude > 0 && in.Padding == 0 {
		// Extruding into a gutter that does not exist writes nothing, which
		// looks like the option being ignored. Saying so beats a silent
		// no-op.
		return SpriteBuildOptions{}, z.FieldError("options.extrude",
			"Extruding repeats a frame's edge pixels into the padding, so it needs a padding to "+
				"repeat them into.")
	}

	metadata, err := normaliseMetadata(in.Metadata)
	if err != nil {
		return SpriteBuildOptions{}, err
	}

	in.Metadata = metadata

	return in, nil
}

// normaliseMetadata validates the requested formats, removing duplicates and
// keeping the order asked for.
func normaliseMetadata(requested []string) ([]string, error) {
	out := make([]string, 0, len(requested))

	for _, name := range requested {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" {
			continue
		}

		if _, ok := spritesheet.MetaFormatOf(spritesheet.MetaFormat(name)); !ok {
			return nil, z.FieldError("options.metadata",
				"Unknown metadata format "+strconv.Quote(name)+". Use one of: "+
					strings.Join(SpriteMetaFormats(), ", ")+".")
		}

		// A format asked for twice is one file, not two: duplicating it would
		// spend a result row and a signed URL on the same bytes.
		if !slices.Contains(out, name) {
			out = append(out, name)
		}
	}

	if len(out) > spritesheet.MaxMetaFormats {
		return nil, z.FieldError("options.metadata",
			fmt.Sprintf("At most %d metadata formats at once.", spritesheet.MaxMetaFormats))
	}

	return out, nil
}

// parseSpriteBackground reads the background colour.
//
// Hex, in the three spellings a person actually types: #rgb, #rrggbb and
// #rrggbbaa. An empty string means transparency, which is the default and the
// right answer for almost every sheet.
func parseSpriteBackground(value string) (*color.NRGBA, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil //nolint:nilnil // no colour and no error is the documented "use transparency"
	}

	hex := strings.TrimPrefix(value, "#")

	// Shorthand expands by doubling each digit, which is CSS's own rule.
	if len(hex) == 3 {
		hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
	}

	if len(hex) != 6 && len(hex) != 8 {
		return nil, z.FieldError("options.background",
			"A background colour is hex: #rgb, #rrggbb or #rrggbbaa.")
	}

	out := color.NRGBA{A: 0xff}

	channels := []*uint8{&out.R, &out.G, &out.B, &out.A}

	for i := range len(hex) / 2 {
		n, err := strconv.ParseUint(hex[i*2:i*2+2], 16, 8)
		if err != nil {
			return nil, z.FieldError("options.background",
				"A background colour is hex: #rgb, #rrggbb or #rrggbbaa.")
		}

		*channels[i] = uint8(n)
	}

	return &out, nil
}

// library turns the public split options into the library's.
func (in SpriteSplitOptions) library() (spritesheet.SliceOptions, error) {
	background, err := parseSpriteBackground(in.Background)
	if err != nil {
		return spritesheet.SliceOptions{}, err
	}

	return spritesheet.SliceOptions{
		Mode: spritesheet.Mode(in.Mode),
		Grid: spritesheet.Grid{
			CellW:   in.CellW,
			CellH:   in.CellH,
			Cols:    in.Cols,
			Rows:    in.Rows,
			Margin:  in.Margin,
			Spacing: in.Spacing,
		},
		Trim:                in.Trim,
		Background:          background,
		BackgroundTolerance: in.BackgroundTolerance,
		MinFrame:            in.MinFrame,
		Prefix:              in.Prefix,
	}, nil
}

// library turns the public build options into the library's.
func (in SpriteBuildOptions) library() spritesheet.BuildOptions {
	return spritesheet.BuildOptions{
		Layout:  spritesheet.Layout(in.Layout),
		Margin:  in.Margin,
		Padding: in.Padding,
		Extrude: in.Extrude,
		MaxSize: in.MaxSize,
		POT:     in.POT,
	}
}

// meta turns the shared metadata options into the library's.
func spriteMetaOptions(aseprite bool, frameMS int) spritesheet.MetaOptions {
	return spritesheet.MetaOptions{Aseprite: aseprite, FrameMS: frameMS}
}

// ParseSpriteSplitOptions validates and decodes the options stored on a
// conversion.
func ParseSpriteSplitOptions(raw json.RawMessage) (SpriteSplitOptions, error) {
	var in SpriteSplitOptions

	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &in); err != nil {
			return SpriteSplitOptions{}, z.FieldError("options", "The options are not a JSON object.")
		}
	}

	return in.Normalise()
}

// ParseSpriteBuildOptions validates and decodes the options stored on a
// conversion.
func ParseSpriteBuildOptions(raw json.RawMessage) (SpriteBuildOptions, error) {
	var in SpriteBuildOptions

	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &in); err != nil {
			return SpriteBuildOptions{}, z.FieldError("options", "The options are not a JSON object.")
		}
	}

	return in.Normalise()
}
