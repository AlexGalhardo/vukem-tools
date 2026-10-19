package spritesheet

import (
	"bufio"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"image"
	"io"
	"slices"
	"strconv"
	"strings"
)

// Atlas metadata: one value, five independent writers.
//
// The geometry lives in [Atlas] and nowhere else, so a format cannot change
// where a frame is — only how that is spelled. Every writer streams into a
// [bufio.Writer] rather than building a string, for the same reason the
// splitter writes frames one at a time: a thousand-frame atlas is a document
// nobody should have to hold twice.
//
// The field names were read from each consumer's own parser or documentation
// rather than from memory, and the package comment cites each source. Where a
// source could not be reached — CodeAndWeb's JSON reference — the golden tests
// parse the output back through a reader written against the documented field
// set, so a typo in a key fails the test instead of shipping.

// MetaFormat is one metadata format.
type MetaFormat string

// The formats.
const (
	// MetaJSONHash is TexturePacker JSON with frames keyed by name. It is
	// what Phaser 3's load.atlas and PixiJS read.
	MetaJSONHash MetaFormat = "json-hash"
	// MetaJSONArray is the same frames as a list, each carrying its own
	// filename.
	MetaJSONArray MetaFormat = "json-array"
	// MetaLibGDX is libGDX's .atlas text format, in the modern
	// bounds:/offsets: spelling.
	MetaLibGDX MetaFormat = "libgdx"
	// MetaSparrow is Sparrow/Starling XML, the widest-supported format there
	// is.
	MetaSparrow MetaFormat = "sparrow"
	// MetaCSS is a CSS sprite sheet: one class per frame.
	MetaCSS MetaFormat = "css"
)

// MaxMetaFormats is how many formats one conversion may ask for.
//
// Four, because a sheet is imported by one engine and the second and third
// files are for a colleague using another. A caller asking for all five is
// asking for a file they will not open, and each one is a result row with its
// own signed URL and its own five-minute clock.
const MaxMetaFormats = 4

// MetaFormatInfo describes one format for the API and the interface, so
// neither holds a list of its own.
type MetaFormatInfo struct {
	// Name is the value a client sends.
	Name MetaFormat `json:"name"`
	// Extension is the file's extension, with the dot.
	Extension string `json:"extension"`
	// MIME is what the result is stored and served as.
	MIME string `json:"mime"`
	// Imports is the one sentence saying what reads this, which is the only
	// thing somebody choosing between five formats actually needs.
	Imports string `json:"imports"`
	// Trim reports whether the format can express a trimmed frame. CSS
	// cannot, and asking for both is refused rather than silently producing
	// a sheet whose frames are off by their trim offset.
	Trim bool `json:"trim"`
}

// metaFormats is the format table, in the order the interface offers them.
var metaFormats = []MetaFormatInfo{
	{
		Name: MetaJSONHash, Extension: ".json", MIME: "application/json",
		Imports: "Phaser 3 (load.atlas) and PixiJS. The usual choice for a web game.",
		Trim:    true,
	},
	{
		Name: MetaJSONArray, Extension: ".json", MIME: "application/json",
		Imports: "The same readers as the hash form, for a pipeline that wants an ordered list.",
		Trim:    true,
	},
	{
		Name: MetaLibGDX, Extension: ".atlas", MIME: "application/vnd.libgdx.atlas",
		Imports: "libGDX's TextureAtlas, and anything else that reads its .atlas text format.",
		Trim:    true,
	},
	{
		Name: MetaSparrow, Extension: ".xml", MIME: "application/xml",
		Imports: "Starling, Sparrow and Phaser 3 (load.atlasXML). The widest support of the five.",
		Trim:    true,
	},
	{
		Name: MetaCSS, Extension: ".css", MIME: "text/css",
		Imports: "A web page, as a CSS sprite sheet: one class per frame. Cannot express trimming.",
		Trim:    false,
	},
}

// MetaFormats returns every format, in display order.
func MetaFormats() []MetaFormatInfo { return slices.Clone(metaFormats) }

// MetaFormatOf returns the format's description.
func MetaFormatOf(name MetaFormat) (MetaFormatInfo, bool) {
	for _, f := range metaFormats {
		if f.Name == name {
			return f, true
		}
	}

	return MetaFormatInfo{}, false
}

// Atlas is a sheet and its frames: everything a metadata writer needs.
type Atlas struct {
	// Image is the sheet's filename, which goes in the metadata so an
	// importer can find the picture beside the description.
	Image string
	// Size is the sheet's size in pixels.
	Size image.Point
	// Frames are the frames, in the order they should be listed. The order is
	// the animation's order, so it is never sorted here.
	Frames []Frame
}

// MetaOptions configures a writer.
type MetaOptions struct {
	// Aseprite adds Aseprite's extensions to the two JSON writers: a
	// per-frame duration and meta.frameTags derived from the frame names. It
	// is a flag on those writers rather than a sixth format, because that is
	// what it is — Aseprite's own CLI writes json-hash and json-array.
	Aseprite bool
	// FrameMS is the per-frame duration the Aseprite extensions carry.
	FrameMS int
	// CSSPrefix is the class-name prefix for the CSS writer. Empty means
	// "sprite".
	CSSPrefix string
}

// defaultCSSPrefix names CSS classes when the caller does not.
const defaultCSSPrefix = "sprite"

// defaultFrameMS is the per-frame duration when Aseprite extensions are on
// and no duration was given: 100ms is ten frames a second, which is Aseprite's
// own default for a new frame.
const defaultFrameMS = 100

// TrimError reports a format that cannot express a trimmed frame being asked
// to describe one.
type TrimError struct {
	Format MetaFormat
}

// Error implements error.
func (e *TrimError) Error() string {
	return "spritesheet: " + string(e.Format) + " cannot express a trimmed frame"
}

// Message is the sentence to show the person.
func (e *TrimError) Message() string {
	return "A CSS sprite sheet has nowhere to record a frame's trim offset, so trimmed frames " +
		"would be drawn in the wrong place. Turn trimming off, or choose a different metadata " +
		"format."
}

// WriteAtlas writes the atlas in one format.
//
// It refuses a combination it cannot describe faithfully rather than writing
// a file that looks right and places frames wrongly: that is the whole reason
// [TrimError] exists.
func WriteAtlas(w io.Writer, a Atlas, format MetaFormat, o MetaOptions) error {
	info, ok := MetaFormatOf(format)
	if !ok {
		return &OptionError{
			Field: "metadata",
			Value: strconv.Quote(string(format)),
			Want:  "one of " + metaFormatNames(),
		}
	}

	if !info.Trim && anyTrimmed(a.Frames) {
		return &TrimError{Format: format}
	}

	if o.CSSPrefix == "" {
		o.CSSPrefix = defaultCSSPrefix
	}

	if o.FrameMS <= 0 {
		o.FrameMS = defaultFrameMS
	}

	buf := bufio.NewWriter(w)

	var err error

	switch format {
	case MetaJSONHash:
		err = writeJSON(buf, a, o, true)
	case MetaJSONArray:
		err = writeJSON(buf, a, o, false)
	case MetaLibGDX:
		err = writeLibGDX(buf, a)
	case MetaSparrow:
		err = writeSparrow(buf, a)
	case MetaCSS:
		err = writeCSS(buf, a, o)
	default:
		// Unreachable: MetaFormatOf refused anything else above. Kept so
		// adding a format to the table without a writer is a switch a reader
		// can see rather than an empty file.
		err = &OptionError{Field: "metadata", Value: strconv.Quote(string(format)), Want: metaFormatNames()}
	}

	if err != nil {
		return err
	}

	if err := buf.Flush(); err != nil {
		return fmt.Errorf("spritesheet: finish the %s metadata: %w", format, err)
	}

	return nil
}

// metaFormatNames lists the formats for a message.
func metaFormatNames() string {
	names := make([]string, 0, len(metaFormats))
	for _, f := range metaFormats {
		names = append(names, string(f.Name))
	}

	return strings.Join(names, ", ")
}

// anyTrimmed reports whether any frame carries a trim offset.
func anyTrimmed(frames []Frame) bool {
	return slices.ContainsFunc(frames, func(f Frame) bool { return f.Trimmed })
}

// The TexturePacker JSON shape.
//
// Written as types rather than as a map so the key names are declared once and
// the compiler keeps them, and so the field order is stable — which is what
// lets a golden file pin the output. The field set is PixiJS's sprite-sheet
// guide, corroborated against Phaser 3's load.atlas.
type (
	jsonRect struct {
		X int `json:"x"`
		Y int `json:"y"`
		W int `json:"w"`
		H int `json:"h"`
	}

	jsonSize struct {
		W int `json:"w"`
		H int `json:"h"`
	}

	jsonPivot struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
	}

	jsonFrame struct {
		// Filename is written only by the array form, where there is no key
		// to carry the name.
		Filename string   `json:"filename,omitempty"`
		Frame    jsonRect `json:"frame"`
		// Rotated is written and is always false. The packer never rotates:
		// it buys a few per cent of area and costs importer compatibility,
		// and a hand-rolled Godot AtlasTexture does not handle it.
		Rotated          bool      `json:"rotated"`
		Trimmed          bool      `json:"trimmed"`
		SpriteSourceSize jsonRect  `json:"spriteSourceSize"`
		SourceSize       jsonSize  `json:"sourceSize"`
		Pivot            jsonPivot `json:"pivot"`
		// Duration is the Aseprite extension, omitted when it is off.
		Duration int `json:"duration,omitempty"`
	}

	jsonTag struct {
		Name string `json:"name"`
		From int    `json:"from"`
		To   int    `json:"to"`
		// Direction is Aseprite's playback direction. Only "forward" is
		// produced: nothing here knows that an animation ping-pongs.
		Direction string `json:"direction"`
	}

	jsonMeta struct {
		App     string `json:"app"`
		Version string `json:"version"`
		Image   string `json:"image"`
		// Format is the pixel format of the sheet, which is always RGBA8888:
		// the packer writes NRGBA PNGs.
		Format string   `json:"format"`
		Size   jsonSize `json:"size"`
		// Scale is a string rather than a number, which is TexturePacker's
		// own spelling and what the readers expect.
		Scale     string    `json:"scale"`
		FrameTags []jsonTag `json:"frameTags,omitempty"`
	}
)

// metaApp and metaVersion identify what wrote the file.
//
// Named honestly rather than as "TexturePacker": the format is
// TexturePacker's, the writer is not, and a file claiming to be from a tool
// that did not write it is the kind of small lie that wastes somebody's
// afternoon when a version-specific quirk is missing.
const (
	metaApp     = "vukem-tools"
	metaVersion = "1.0"
)

// writeJSON writes the TexturePacker JSON, keyed by name or as an array.
func writeJSON(w io.Writer, a Atlas, o MetaOptions, hash bool) error {
	meta := jsonMeta{
		App:     metaApp,
		Version: metaVersion,
		Image:   a.Image,
		Format:  "RGBA8888",
		Size:    jsonSize{W: a.Size.X, H: a.Size.Y},
		Scale:   "1",
	}

	if o.Aseprite {
		meta.FrameTags = frameTags(a.Frames)
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")

	if hash {
		frames := make(map[string]jsonFrame, len(a.Frames))
		for _, f := range a.Frames {
			frames[f.Name] = jsonFrameOf(f, o, false)
		}

		// encoding/json sorts map keys, so the hash form is deterministic
		// without the writer doing anything: two runs over the same frames
		// produce byte-identical output.
		return encodeJSON(enc, struct {
			Frames map[string]jsonFrame `json:"frames"`
			Meta   jsonMeta             `json:"meta"`
		}{Frames: frames, Meta: meta})
	}

	frames := make([]jsonFrame, 0, len(a.Frames))
	for _, f := range a.Frames {
		frames = append(frames, jsonFrameOf(f, o, true))
	}

	return encodeJSON(enc, struct {
		Frames []jsonFrame `json:"frames"`
		Meta   jsonMeta    `json:"meta"`
	}{Frames: frames, Meta: meta})
}

// encodeJSON writes one document and names the format in any failure.
func encodeJSON(enc *json.Encoder, doc any) error {
	if err := enc.Encode(doc); err != nil {
		return fmt.Errorf("spritesheet: write the JSON atlas: %w", err)
	}

	return nil
}

// jsonFrameOf converts one frame.
//
// The two size fields are the whole of what makes trimming safe, and they are
// easy to get backwards: spriteSourceSize is where the trimmed content sits
// inside the original box and how big it is, so its w and h equal the frame
// rectangle's; sourceSize is the original box. An importer re-inflates with
// both, and swapping them makes every trimmed animation jitter.
func jsonFrameOf(f Frame, o MetaOptions, named bool) jsonFrame {
	out := jsonFrame{
		Frame:   jsonRect{X: f.Rect.Min.X, Y: f.Rect.Min.Y, W: f.Rect.Dx(), H: f.Rect.Dy()},
		Rotated: false,
		Trimmed: f.Trimmed,
		SpriteSourceSize: jsonRect{
			X: f.Offset.X, Y: f.Offset.Y, W: f.Rect.Dx(), H: f.Rect.Dy(),
		},
		SourceSize: jsonSize{W: f.Source.X, H: f.Source.Y},
		Pivot:      jsonPivot{X: 0.5, Y: 0.5},
	}

	if named {
		out.Filename = f.Name
	}

	if o.Aseprite {
		out.Duration = o.FrameMS
	}

	return out
}

// frameTags groups frames into Aseprite tags by their name.
//
// A frame is named prefix + index, so the tag is the name with its trailing
// digits removed: "walk_000" through "walk_007" become one "walk" tag from 0
// to 7. from and to are 0-based and inclusive, which is Aseprite's own
// convention.
//
// Only runs of adjacent frames are grouped. Two separate runs sharing a stem
// would otherwise become one tag spanning the frames between them, which is a
// tag that plays the wrong animation.
func frameTags(frames []Frame) []jsonTag {
	var tags []jsonTag

	for i, f := range frames {
		stem := nameStem(f.Name)

		if n := len(tags); n > 0 && tags[n-1].Name == stem && tags[n-1].To == i-1 {
			tags[n-1].To = i

			continue
		}

		tags = append(tags, jsonTag{Name: stem, From: i, To: i, Direction: "forward"})
	}

	return tags
}

// nameStem is a frame's name with its trailing digits and separator removed.
func nameStem(name string) string {
	end := len(name)
	for end > 0 && name[end-1] >= '0' && name[end-1] <= '9' {
		end--
	}

	// A name that is nothing but digits keeps its name: there is no stem to
	// find, and an empty tag name helps nobody.
	if end == 0 {
		return name
	}

	return strings.TrimRight(name[:end], "_-. ")
}

// writeLibGDX writes libGDX's .atlas text format.
//
// The modern spelling, read out of libGDX's own TextureAtlas.java: a page
// header of size/format/filter/repeat/pma, then one region per frame with
// bounds, offsets and index. The deprecated xy:/size:/orig:/offset: spelling
// is not written.
//
// offsets carries four numbers — the trim offset and then the *original*
// size — which is the pair that lets libGDX re-inflate a trimmed region.
func writeLibGDX(w io.Writer, a Atlas) error {
	// A leading blank line is part of the format: the parser reads lines
	// until a non-empty one and takes that as the page's image file.
	fields := [][2]string{
		{"size", fmt.Sprintf("%d, %d", a.Size.X, a.Size.Y)},
		{"format", "RGBA8888"},
		// Nearest on both axes, because this is pixel art: linear filtering
		// is what makes a sprite sheet blurry, and a sheet packed from pixel
		// art almost never wants it.
		{"filter", "Nearest, Nearest"},
		{"repeat", "none"},
		// Premultiplied alpha is off: the packer composites straight alpha.
		{"pma", "false"},
	}

	if _, err := fmt.Fprintf(w, "\n%s\n", a.Image); err != nil {
		return fmt.Errorf("spritesheet: write the libGDX page header: %w", err)
	}

	for _, f := range fields {
		if _, err := fmt.Fprintf(w, "%s: %s\n", f[0], f[1]); err != nil {
			return fmt.Errorf("spritesheet: write the libGDX page header: %w", err)
		}
	}

	for _, f := range a.Frames {
		if _, err := fmt.Fprintf(w,
			"%s\n  rotate: false\n  bounds: %d, %d, %d, %d\n  offsets: %d, %d, %d, %d\n  index: -1\n",
			f.Name,
			f.Rect.Min.X, f.Rect.Min.Y, f.Rect.Dx(), f.Rect.Dy(),
			f.Offset.X, f.Offset.Y, f.Source.X, f.Source.Y,
		); err != nil {
			return fmt.Errorf("spritesheet: write the libGDX region %q: %w", f.Name, err)
		}
	}

	return nil
}

// The Sparrow/Starling XML shape, read out of Phaser 3.88.2's AtlasXML.js.
type (
	sparrowAtlas struct {
		XMLName    xml.Name       `xml:"TextureAtlas"`
		ImagePath  string         `xml:"imagePath,attr"`
		SubTexture []sparrowFrame `xml:"SubTexture"`
	}

	sparrowFrame struct {
		Name   string `xml:"name,attr"`
		X      int    `xml:"x,attr"`
		Y      int    `xml:"y,attr"`
		Width  int    `xml:"width,attr"`
		Height int    `xml:"height,attr"`
		// The frame* attributes describe a trimmed frame and are written
		// only for one: a reader takes their presence as the signal that the
		// region is trimmed, so writing zeroes on an untrimmed frame would
		// claim a trim that is not there.
		FrameX      *int `xml:"frameX,attr,omitempty"`
		FrameY      *int `xml:"frameY,attr,omitempty"`
		FrameWidth  *int `xml:"frameWidth,attr,omitempty"`
		FrameHeight *int `xml:"frameHeight,attr,omitempty"`
	}
)

// writeSparrow writes Sparrow/Starling XML.
func writeSparrow(w io.Writer, a Atlas) error {
	doc := sparrowAtlas{ImagePath: a.Image, SubTexture: make([]sparrowFrame, 0, len(a.Frames))}

	for _, f := range a.Frames {
		entry := sparrowFrame{
			Name:   f.Name,
			X:      f.Rect.Min.X,
			Y:      f.Rect.Min.Y,
			Width:  f.Rect.Dx(),
			Height: f.Rect.Dy(),
		}

		if f.Trimmed {
			// Negative, which is Sparrow's convention: frameX is where the
			// original's origin sits relative to the trimmed content, so
			// trimming three pixels off the left is frameX = -3. Phaser
			// takes the absolute value, so the sign costs nothing there and
			// is what every other reader expects.
			x, y := -f.Offset.X, -f.Offset.Y
			sw, sh := f.Source.X, f.Source.Y
			entry.FrameX, entry.FrameY, entry.FrameWidth, entry.FrameHeight = &x, &y, &sw, &sh
		}

		doc.SubTexture = append(doc.SubTexture, entry)
	}

	if _, err := io.WriteString(w, xml.Header); err != nil {
		return fmt.Errorf("spritesheet: write the XML header: %w", err)
	}

	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")

	if err := enc.Encode(doc); err != nil {
		return fmt.Errorf("spritesheet: write the Sparrow atlas: %w", err)
	}

	if err := enc.Close(); err != nil {
		return fmt.Errorf("spritesheet: finish the Sparrow atlas: %w", err)
	}

	if _, err := io.WriteString(w, "\n"); err != nil {
		return fmt.Errorf("spritesheet: finish the Sparrow atlas: %w", err)
	}

	return nil
}

// writeCSS writes one class per frame.
//
// background-position is the exact negative of the frame's position in the
// sheet, which is the whole of CSS spriting: the element is a window of the
// frame's size and the sheet is slid underneath it.
func writeCSS(w io.Writer, a Atlas, o MetaOptions) error {
	if _, err := fmt.Fprintf(w,
		"/* %d frames of %s, %dx%d. Generated by %s. */\n"+
			".%s {\n  background-image: url(%q);\n  background-repeat: no-repeat;\n"+
			"  display: inline-block;\n}\n",
		len(a.Frames), a.Image, a.Size.X, a.Size.Y, metaApp, o.CSSPrefix, a.Image,
	); err != nil {
		return fmt.Errorf("spritesheet: write the CSS header: %w", err)
	}

	for _, f := range a.Frames {
		if _, err := fmt.Fprintf(w,
			"\n.%s-%s {\n  width: %dpx;\n  height: %dpx;\n  background-position: %dpx %dpx;\n}\n",
			o.CSSPrefix, cssIdent(f.Name), f.Rect.Dx(), f.Rect.Dy(), -f.Rect.Min.X, -f.Rect.Min.Y,
		); err != nil {
			return fmt.Errorf("spritesheet: write the CSS rule for %q: %w", f.Name, err)
		}
	}

	return nil
}

// cssIdent makes a frame name usable as a class name.
//
// Anything that is not a letter, a digit, a hyphen or an underscore becomes a
// hyphen, and a leading digit is prefixed — a CSS identifier cannot start with
// one, and a rule that never matches is worse than a renamed class.
func cssIdent(name string) string {
	var b strings.Builder

	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}

	out := b.String()
	if out == "" {
		return "frame"
	}

	if out[0] >= '0' && out[0] <= '9' {
		return "f" + out
	}

	return out
}
