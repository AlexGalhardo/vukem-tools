package spritesheet_test

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/AlexGalhardo/vukem-tools/internal/spritesheet"
)

// update rewrites the golden files instead of comparing against them.
//
// A flag rather than an environment variable, so `go test -update` is
// discoverable from `go test -args -h` and cannot be set by accident in a
// shell that happens to export the wrong name.
var update = flag.Bool("update", false, "rewrite the atlas golden files")

// threeFrames is the atlas every golden is written from: one untrimmed frame,
// one trimmed frame and one more untrimmed, so each writer has to handle both
// shapes and the trimmed one is not the first or the last.
func threeFrames() spritesheet.Atlas {
	return spritesheet.Atlas{
		Image: "sheet.png",
		Size:  image.Pt(96, 32),
		Frames: []spritesheet.Frame{
			{
				Name:   "walk_000",
				Rect:   image.Rect(0, 0, 32, 32),
				Offset: image.Pt(0, 0),
				Source: image.Pt(32, 32),
			},
			{
				Name:    "walk_001",
				Rect:    image.Rect(32, 2, 60, 29),
				Offset:  image.Pt(3, 2),
				Source:  image.Pt(32, 32),
				Trimmed: true,
			},
			{
				Name:   "idle_000",
				Rect:   image.Rect(64, 0, 96, 32),
				Offset: image.Pt(0, 0),
				Source: image.Pt(32, 32),
			},
		},
	}
}

// write renders one format and returns the bytes.
func write(t *testing.T, a spritesheet.Atlas, f spritesheet.MetaFormat, o spritesheet.MetaOptions) []byte {
	t.Helper()

	var buf bytes.Buffer
	if err := spritesheet.WriteAtlas(&buf, a, f, o); err != nil {
		t.Fatalf("write %s: %v", f, err)
	}

	return buf.Bytes()
}

// golden compares got against the committed file, or rewrites it under
// -update.
func golden(t *testing.T, name string, got []byte) {
	t.Helper()

	path := filepath.Join("testdata", name)

	if *update {
		if err := os.WriteFile(path, got, 0o600); err != nil {
			t.Fatalf("rewrite %s: %v", path, err)
		}

		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s (run `go test ./internal/spritesheet/ -update` to create it): %v", path, err)
	}

	if !bytes.Equal(got, want) {
		t.Errorf("%s does not match the golden file.\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

// Every writer has a golden over the same three frames, so a change to any
// one of them is visible in review rather than only in a consumer's importer.
func TestEveryWriterMatchesItsGolden(t *testing.T) {
	t.Parallel()

	a := threeFrames()

	for _, c := range []struct {
		format spritesheet.MetaFormat
		opts   spritesheet.MetaOptions
		name   string
	}{
		{spritesheet.MetaJSONHash, spritesheet.MetaOptions{}, "atlas-hash.json"},
		{spritesheet.MetaJSONArray, spritesheet.MetaOptions{}, "atlas-array.json"},
		{spritesheet.MetaJSONHash, spritesheet.MetaOptions{Aseprite: true, FrameMS: 80}, "atlas-aseprite.json"},
		{spritesheet.MetaLibGDX, spritesheet.MetaOptions{}, "atlas.atlas"},
		{spritesheet.MetaSparrow, spritesheet.MetaOptions{}, "atlas.xml"},
	} {
		golden(t, c.name, write(t, a, c.format, c.opts))
	}

	// CSS gets its own golden from an untrimmed atlas, because it refuses a
	// trimmed one — which is itself a committed decision rather than a gap.
	golden(t, "atlas.css", write(t, untrimmedFrames(), spritesheet.MetaCSS, spritesheet.MetaOptions{}))
}

// untrimmedFrames is the same three frames with nothing trimmed, for the two
// places a trimmed atlas cannot be used.
func untrimmedFrames() spritesheet.Atlas {
	return spritesheet.Atlas{
		Image: "sheet.png",
		Size:  image.Pt(96, 32),
		Frames: []spritesheet.Frame{
			{Name: "walk_000", Rect: image.Rect(0, 0, 32, 32), Source: image.Pt(32, 32)},
			{Name: "walk_001", Rect: image.Rect(32, 0, 64, 32), Source: image.Pt(32, 32)},
			{Name: "idle_000", Rect: image.Rect(64, 0, 96, 32), Source: image.Pt(32, 32)},
		},
	}
}

// A writer that produces the same bytes twice is what a cache and a golden
// both depend on. The hash form is the one at risk, because it goes through a
// map.
func TestTheWritersAreDeterministic(t *testing.T) {
	t.Parallel()

	// CSS takes the untrimmed atlas, which it is the only format to need:
	// it refuses a trimmed one by design.
	for _, c := range []struct {
		format spritesheet.MetaFormat
		atlas  spritesheet.Atlas
	}{
		{spritesheet.MetaJSONHash, threeFrames()},
		{spritesheet.MetaJSONArray, threeFrames()},
		{spritesheet.MetaLibGDX, threeFrames()},
		{spritesheet.MetaSparrow, threeFrames()},
		{spritesheet.MetaCSS, untrimmedFrames()},
	} {
		first := write(t, c.atlas, c.format, spritesheet.MetaOptions{})

		// The hash form is the one at risk, because it goes through a map,
		// and a map's iteration order changes between runs of the same
		// binary rather than between builds — so eight attempts is a real
		// test rather than a formality.
		for range 8 {
			if got := write(t, c.atlas, c.format, spritesheet.MetaOptions{}); !bytes.Equal(got, first) {
				t.Fatalf("%s is not deterministic", c.format)
			}
		}
	}
}

// The JSON goldens are parsed back by a reader written against the documented
// field set, so a typo in a key fails here rather than in somebody's game.
func TestTheJSONGoldensParseBackToTheSameGeometry(t *testing.T) {
	t.Parallel()

	a := threeFrames()

	t.Run("hash", func(t *testing.T) {
		t.Parallel()

		var doc struct {
			Frames map[string]readFrame `json:"frames"`
			Meta   readMeta             `json:"meta"`
		}

		decodeJSON(t, write(t, a, spritesheet.MetaJSONHash, spritesheet.MetaOptions{}), &doc)
		checkMeta(t, doc.Meta, a)

		if len(doc.Frames) != len(a.Frames) {
			t.Fatalf("the hash holds %d frames, want %d", len(doc.Frames), len(a.Frames))
		}

		for _, want := range a.Frames {
			got, ok := doc.Frames[want.Name]
			if !ok {
				t.Fatalf("the hash has no entry for %q", want.Name)
			}

			checkFrame(t, got, want)
		}
	})

	t.Run("array", func(t *testing.T) {
		t.Parallel()

		var doc struct {
			Frames []readFrame `json:"frames"`
			Meta   readMeta    `json:"meta"`
		}

		decodeJSON(t, write(t, a, spritesheet.MetaJSONArray, spritesheet.MetaOptions{}), &doc)
		checkMeta(t, doc.Meta, a)

		if len(doc.Frames) != len(a.Frames) {
			t.Fatalf("the array holds %d frames, want %d", len(doc.Frames), len(a.Frames))
		}

		for i, want := range a.Frames {
			if doc.Frames[i].Filename != want.Name {
				t.Errorf("frame %d is named %q, want %q: the array form carries the name on each entry",
					i, doc.Frames[i].Filename, want.Name)
			}

			checkFrame(t, doc.Frames[i], want)
		}
	})
}

// The ROADMAP's own assert, stated as one test because it is the pair of
// fields that makes trimming safe and the pair that is easy to swap.
func TestATrimmedFrameCarriesBothSizes(t *testing.T) {
	t.Parallel()

	a := threeFrames()

	var doc struct {
		Frames map[string]readFrame `json:"frames"`
	}

	decodeJSON(t, write(t, a, spritesheet.MetaJSONHash, spritesheet.MetaOptions{}), &doc)

	trimmed := doc.Frames["walk_001"]
	if !trimmed.Trimmed {
		t.Fatal("walk_001 is not marked trimmed")
	}

	if trimmed.SpriteSourceSize.W != trimmed.Frame.W || trimmed.SpriteSourceSize.H != trimmed.Frame.H {
		t.Errorf("spriteSourceSize is %dx%d and frame is %dx%d: they must match, because "+
			"spriteSourceSize is where the *trimmed* content sits in the original box",
			trimmed.SpriteSourceSize.W, trimmed.SpriteSourceSize.H, trimmed.Frame.W, trimmed.Frame.H)
	}

	if trimmed.SpriteSourceSize.X != 3 || trimmed.SpriteSourceSize.Y != 2 {
		t.Errorf("the trim offset is (%d,%d), want (3,2)",
			trimmed.SpriteSourceSize.X, trimmed.SpriteSourceSize.Y)
	}

	if trimmed.SourceSize.W != 32 || trimmed.SourceSize.H != 32 {
		t.Errorf("sourceSize is %dx%d, want the untrimmed 32x32",
			trimmed.SourceSize.W, trimmed.SourceSize.H)
	}

	untrimmed := doc.Frames["walk_000"]
	if untrimmed.Trimmed {
		t.Error("walk_000 is marked trimmed")
	}

	if untrimmed.SpriteSourceSize.X != 0 || untrimmed.SpriteSourceSize.Y != 0 {
		t.Errorf("an untrimmed frame has offset (%d,%d), want zero",
			untrimmed.SpriteSourceSize.X, untrimmed.SpriteSourceSize.Y)
	}
}

// rotated is false on every frame of every format. The packer never rotates,
// and this is what stops that changing without the metadata and the importers
// being dealt with at the same time.
func TestNothingIsEverRotated(t *testing.T) {
	t.Parallel()

	a := threeFrames()

	var hash struct {
		Frames map[string]readFrame `json:"frames"`
	}

	decodeJSON(t, write(t, a, spritesheet.MetaJSONHash, spritesheet.MetaOptions{}), &hash)

	for name, f := range hash.Frames {
		if f.Rotated {
			t.Errorf("%s is marked rotated in the hash form", name)
		}
	}

	var array struct {
		Frames []readFrame `json:"frames"`
	}

	decodeJSON(t, write(t, a, spritesheet.MetaJSONArray, spritesheet.MetaOptions{}), &array)

	for _, f := range array.Frames {
		if f.Rotated {
			t.Errorf("%s is marked rotated in the array form", f.Filename)
		}
	}

	for line := range strings.SplitSeq(string(write(t, a, spritesheet.MetaLibGDX, spritesheet.MetaOptions{})), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "rotate:") &&
			strings.TrimSpace(line) != "rotate: false" {
			t.Errorf("the libGDX atlas says %q", strings.TrimSpace(line))
		}
	}
}

// The libGDX golden is parsed by a reader implementing the grammar read out
// of TextureAtlas.java, and every region's bounds and offsets round-trip.
func TestTheLibGDXGoldenRoundTrips(t *testing.T) {
	t.Parallel()

	a := threeFrames()
	page, regions := parseLibGDX(t, string(write(t, a, spritesheet.MetaLibGDX, spritesheet.MetaOptions{})))

	if page["__image__"] != a.Image {
		t.Errorf("the page names %q, want %q", page["__image__"], a.Image)
	}

	if want := fmt.Sprintf("%d, %d", a.Size.X, a.Size.Y); page["size"] != want {
		t.Errorf("size: %q, want %q", page["size"], want)
	}

	for _, key := range []string{"format", "filter", "repeat", "pma"} {
		if page[key] == "" {
			t.Errorf("the page header has no %s", key)
		}
	}

	if len(regions) != len(a.Frames) {
		t.Fatalf("parsed %d regions, want %d", len(regions), len(a.Frames))
	}

	for i, want := range a.Frames {
		got := regions[i]

		if got.name != want.Name {
			t.Errorf("region %d is %q, want %q", i, got.name, want.Name)
		}

		wantBounds := [4]int{want.Rect.Min.X, want.Rect.Min.Y, want.Rect.Dx(), want.Rect.Dy()}
		if got.bounds != wantBounds {
			t.Errorf("%s bounds: %v, want %v", got.name, got.bounds, wantBounds)
		}

		// offsets is the trim offset then the *original* size, which is the
		// pair libGDX re-inflates a trimmed region with.
		wantOffsets := [4]int{want.Offset.X, want.Offset.Y, want.Source.X, want.Source.Y}
		if got.offsets != wantOffsets {
			t.Errorf("%s offsets: %v, want %v", got.name, got.offsets, wantOffsets)
		}
	}
}

// The Sparrow golden parses into the attribute set Phaser's AtlasXML.js
// reads, and the frame* attributes appear only on a trimmed frame.
func TestTheSparrowGoldenCarriesFrameAttributesOnlyWhenTrimmed(t *testing.T) {
	t.Parallel()

	a := threeFrames()

	var doc struct {
		ImagePath  string `xml:"imagePath,attr"`
		SubTexture []struct {
			Name        string `xml:"name,attr"`
			X           int    `xml:"x,attr"`
			Y           int    `xml:"y,attr"`
			Width       int    `xml:"width,attr"`
			Height      int    `xml:"height,attr"`
			FrameX      *int   `xml:"frameX,attr"`
			FrameY      *int   `xml:"frameY,attr"`
			FrameWidth  *int   `xml:"frameWidth,attr"`
			FrameHeight *int   `xml:"frameHeight,attr"`
		} `xml:"SubTexture"`
	}

	raw := write(t, a, spritesheet.MetaSparrow, spritesheet.MetaOptions{})
	if err := xml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse the Sparrow atlas: %v\n%s", err, raw)
	}

	if doc.ImagePath != a.Image {
		t.Errorf("imagePath = %q, want %q", doc.ImagePath, a.Image)
	}

	if len(doc.SubTexture) != len(a.Frames) {
		t.Fatalf("parsed %d SubTextures, want %d", len(doc.SubTexture), len(a.Frames))
	}

	for i, want := range a.Frames {
		got := doc.SubTexture[i]

		if got.Name != want.Name || got.X != want.Rect.Min.X || got.Y != want.Rect.Min.Y ||
			got.Width != want.Rect.Dx() || got.Height != want.Rect.Dy() {
			t.Errorf("SubTexture %d is %+v, want %s at %v", i, got, want.Name, want.Rect)
		}

		if !want.Trimmed {
			if got.FrameX != nil || got.FrameY != nil || got.FrameWidth != nil || got.FrameHeight != nil {
				t.Errorf("%s is untrimmed but carries frame* attributes, which a reader takes "+
					"as the signal that it is trimmed", got.Name)
			}

			continue
		}

		if got.FrameX == nil || got.FrameY == nil || got.FrameWidth == nil || got.FrameHeight == nil {
			t.Fatalf("%s is trimmed but carries no frame* attributes", got.Name)
		}

		// Negative, which is Sparrow's convention for the origin's position
		// relative to the trimmed content.
		if *got.FrameX != -want.Offset.X || *got.FrameY != -want.Offset.Y {
			t.Errorf("%s frameX,frameY = %d,%d, want %d,%d",
				got.Name, *got.FrameX, *got.FrameY, -want.Offset.X, -want.Offset.Y)
		}

		if *got.FrameWidth != want.Source.X || *got.FrameHeight != want.Source.Y {
			t.Errorf("%s frameWidth,frameHeight = %d,%d, want the untrimmed %d,%d",
				got.Name, *got.FrameWidth, *got.FrameHeight, want.Source.X, want.Source.Y)
		}
	}
}

// Every CSS rule's background-position is the exact negative of its frame's
// position, which is the whole of CSS spriting and the one thing an
// off-by-one would silently break.
func TestEveryCSSRulePositionsItsFrameExactly(t *testing.T) {
	t.Parallel()

	a := untrimmedFrames()

	rules := parseCSS(t, string(write(t, a, spritesheet.MetaCSS, spritesheet.MetaOptions{})))

	for _, want := range a.Frames {
		rule, ok := rules["sprite-"+want.Name]
		if !ok {
			t.Fatalf("no rule for %q; parsed %v", want.Name, keysOf(rules))
		}

		wantPos := fmt.Sprintf("%dpx %dpx", -want.Rect.Min.X, -want.Rect.Min.Y)
		if rule["background-position"] != wantPos {
			t.Errorf("%s background-position: %q, want %q",
				want.Name, rule["background-position"], wantPos)
		}

		if rule["width"] != strconv.Itoa(want.Rect.Dx())+"px" {
			t.Errorf("%s width: %q, want %dpx", want.Name, rule["width"], want.Rect.Dx())
		}

		if rule["height"] != strconv.Itoa(want.Rect.Dy())+"px" {
			t.Errorf("%s height: %q, want %dpx", want.Name, rule["height"], want.Rect.Dy())
		}
	}
}

// CSS has nowhere to record a trim offset, so asking for both is refused
// rather than producing a sheet whose frames are drawn in the wrong place.
func TestCSSRefusesATrimmedAtlas(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	err := spritesheet.WriteAtlas(&buf, threeFrames(), spritesheet.MetaCSS, spritesheet.MetaOptions{})

	trim, ok := errors.AsType[*spritesheet.TrimError](err)
	if !ok {
		t.Fatalf("err = %v, want a *TrimError", err)
	}

	if !strings.Contains(trim.Message(), "trim") {
		t.Errorf("the message does not name the problem: %q", trim.Message())
	}

	if buf.Len() != 0 {
		t.Errorf("%d bytes were written before the refusal", buf.Len())
	}
}

// The Aseprite extensions are a duration on every frame and a tag per run of
// frames sharing a name, 0-based and inclusive.
func TestTheAsepriteExtensionsTagEachRunOfFrames(t *testing.T) {
	t.Parallel()

	a := threeFrames()

	var doc struct {
		Frames map[string]readFrame `json:"frames"`
		Meta   struct {
			FrameTags []struct {
				Name      string `json:"name"`
				From      int    `json:"from"`
				To        int    `json:"to"`
				Direction string `json:"direction"`
			} `json:"frameTags"`
		} `json:"meta"`
	}

	decodeJSON(t, write(t, a, spritesheet.MetaJSONHash,
		spritesheet.MetaOptions{Aseprite: true, FrameMS: 80}), &doc)

	for name, f := range doc.Frames {
		if f.Duration != 80 {
			t.Errorf("%s has duration %d, want 80", name, f.Duration)
		}
	}

	if len(doc.Meta.FrameTags) != 2 {
		t.Fatalf("got %d tags, want 2 (walk 0-1 and idle 2-2): %+v",
			len(doc.Meta.FrameTags), doc.Meta.FrameTags)
	}

	if got := doc.Meta.FrameTags[0]; got.Name != "walk" || got.From != 0 || got.To != 1 {
		t.Errorf("the first tag is %+v, want walk from 0 to 1", got)
	}

	if got := doc.Meta.FrameTags[1]; got.Name != "idle" || got.From != 2 || got.To != 2 {
		t.Errorf("the second tag is %+v, want idle from 2 to 2", got)
	}

	for _, tag := range doc.Meta.FrameTags {
		if tag.Direction != "forward" {
			t.Errorf("%s plays %q; nothing here knows an animation ping-pongs", tag.Name, tag.Direction)
		}
	}
}

// Two runs of the same stem separated by another animation must not become
// one tag spanning the frames between them, which would play the wrong
// animation.
func TestARepeatedStemIsTwoTagsNotOne(t *testing.T) {
	t.Parallel()

	a := spritesheet.Atlas{
		Image: "sheet.png",
		Size:  image.Pt(96, 32),
		Frames: []spritesheet.Frame{
			{Name: "walk_000", Rect: image.Rect(0, 0, 32, 32), Source: image.Pt(32, 32)},
			{Name: "idle_000", Rect: image.Rect(32, 0, 64, 32), Source: image.Pt(32, 32)},
			{Name: "walk_001", Rect: image.Rect(64, 0, 96, 32), Source: image.Pt(32, 32)},
		},
	}

	var doc struct {
		Meta struct {
			FrameTags []struct {
				Name string `json:"name"`
				From int    `json:"from"`
				To   int    `json:"to"`
			} `json:"frameTags"`
		} `json:"meta"`
	}

	decodeJSON(t, write(t, a, spritesheet.MetaJSONHash, spritesheet.MetaOptions{Aseprite: true}), &doc)

	if len(doc.Meta.FrameTags) != 3 {
		t.Fatalf("got %d tags, want 3: %+v", len(doc.Meta.FrameTags), doc.Meta.FrameTags)
	}

	for _, tag := range doc.Meta.FrameTags {
		if tag.From != tag.To {
			t.Errorf("tag %s spans %d to %d, but no two of these frames are adjacent",
				tag.Name, tag.From, tag.To)
		}
	}
}

// An unknown format is refused by name rather than writing nothing and
// reporting success.
func TestAnUnknownMetadataFormatIsRefused(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	err := spritesheet.WriteAtlas(&buf, threeFrames(), spritesheet.MetaFormat("plist"),
		spritesheet.MetaOptions{})

	if _, ok := errors.AsType[*spritesheet.OptionError](err); !ok {
		t.Fatalf("err = %v, want an *OptionError", err)
	}
}

// Every format in the published table has a writer, and every writer is in
// the table. The API publishes this list so the interface does not hardcode
// it, which only works if it is complete.
func TestTheFormatTableAndTheWritersAgree(t *testing.T) {
	t.Parallel()

	formats := spritesheet.MetaFormats()
	if len(formats) != 5 {
		t.Fatalf("the table lists %d formats, want 5", len(formats))
	}

	flat := spritesheet.Atlas{
		Image:  "sheet.png",
		Size:   image.Pt(32, 32),
		Frames: []spritesheet.Frame{{Name: "a_000", Rect: image.Rect(0, 0, 32, 32), Source: image.Pt(32, 32)}},
	}

	for _, f := range formats {
		var buf bytes.Buffer
		if err := spritesheet.WriteAtlas(&buf, flat, f.Name, spritesheet.MetaOptions{}); err != nil {
			t.Errorf("%s is in the table but cannot be written: %v", f.Name, err)
		}

		if buf.Len() == 0 {
			t.Errorf("%s wrote nothing", f.Name)
		}

		if f.Extension == "" || f.MIME == "" || f.Imports == "" {
			t.Errorf("%s is published without an extension, a media type or a sentence: %+v", f.Name, f)
		}
	}
}

// readFrame and readMeta are a reader written against the documented field
// set, used instead of the writer's own types: reusing those would make the
// test agree with the writer by construction and prove nothing about the key
// names.
type readFrame struct {
	Filename         string   `json:"filename"`
	Frame            readRect `json:"frame"`
	Rotated          bool     `json:"rotated"`
	Trimmed          bool     `json:"trimmed"`
	SpriteSourceSize readRect `json:"spriteSourceSize"`
	SourceSize       readSize `json:"sourceSize"`
	Pivot            struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
	} `json:"pivot"`
	Duration int `json:"duration"`
}

type readRect struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

type readSize struct {
	W int `json:"w"`
	H int `json:"h"`
}

type readMeta struct {
	App     string   `json:"app"`
	Version string   `json:"version"`
	Image   string   `json:"image"`
	Format  string   `json:"format"`
	Size    readSize `json:"size"`
	Scale   string   `json:"scale"`
}

func decodeJSON(t *testing.T, raw []byte, into any) {
	t.Helper()

	if err := json.Unmarshal(raw, into); err != nil {
		t.Fatalf("parse the JSON atlas: %v\n%s", err, raw)
	}
}

func checkMeta(t *testing.T, got readMeta, a spritesheet.Atlas) {
	t.Helper()

	if got.Image != a.Image {
		t.Errorf("meta.image = %q, want %q", got.Image, a.Image)
	}

	if got.Size.W != a.Size.X || got.Size.H != a.Size.Y {
		t.Errorf("meta.size = %dx%d, want %dx%d", got.Size.W, got.Size.H, a.Size.X, a.Size.Y)
	}

	if got.Scale != "1" {
		t.Errorf("meta.scale = %q, want \"1\": the readers expect a string", got.Scale)
	}

	if got.Format != "RGBA8888" {
		t.Errorf("meta.format = %q, want RGBA8888", got.Format)
	}

	if got.App == "" || got.Version == "" {
		t.Errorf("meta names no app or version: %+v", got)
	}
}

func checkFrame(t *testing.T, got readFrame, want spritesheet.Frame) {
	t.Helper()

	if got.Frame.X != want.Rect.Min.X || got.Frame.Y != want.Rect.Min.Y ||
		got.Frame.W != want.Rect.Dx() || got.Frame.H != want.Rect.Dy() {
		t.Errorf("%s frame = %+v, want %v", want.Name, got.Frame, want.Rect)
	}

	if got.Trimmed != want.Trimmed {
		t.Errorf("%s trimmed = %v, want %v", want.Name, got.Trimmed, want.Trimmed)
	}

	if got.SourceSize.W != want.Source.X || got.SourceSize.H != want.Source.Y {
		t.Errorf("%s sourceSize = %+v, want %v", want.Name, got.SourceSize, want.Source)
	}
}

// libGDXRegion is one parsed region.
type libGDXRegion struct {
	name    string
	bounds  [4]int
	offsets [4]int
}

// parseLibGDX implements the grammar read out of libGDX's TextureAtlas.java:
// a blank line, the page's image file, indented `key: value` page fields, then
// one region per unindented name line with its own indented fields.
func parseLibGDX(t *testing.T, raw string) (map[string]string, []libGDXRegion) {
	t.Helper()

	page := map[string]string{}

	var (
		regions []libGDXRegion
		inPage  bool
	)

	for line := range strings.SplitSeq(raw, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}

		indented := strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")
		trimmed := strings.TrimSpace(line)

		if !indented {
			if page["__image__"] == "" {
				page["__image__"] = trimmed
				inPage = true

				continue
			}

			key, value, isField := strings.Cut(trimmed, ": ")
			if inPage && isField {
				page[key] = value

				continue
			}

			inPage = false
			regions = append(regions, libGDXRegion{name: trimmed})

			continue
		}

		key, value, ok := strings.Cut(trimmed, ": ")
		if !ok {
			t.Fatalf("indented line is not a field: %q", line)
		}

		if inPage {
			page[key] = value

			continue
		}

		if len(regions) == 0 {
			t.Fatalf("a region field appeared before any region: %q", line)
		}

		switch key {
		case "bounds":
			regions[len(regions)-1].bounds = four(t, value)
		case "offsets":
			regions[len(regions)-1].offsets = four(t, value)
		}
	}

	return page, regions
}

// four parses "a, b, c, d".
func four(t *testing.T, value string) [4]int {
	t.Helper()

	parts := strings.Split(value, ",")
	if len(parts) != 4 {
		t.Fatalf("want four numbers, got %q", value)
	}

	var out [4]int

	for i, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			t.Fatalf("%q is not a number in %q: %v", p, value, err)
		}

		out[i] = n
	}

	return out
}

// parseCSS reads the declarations of each rule, which is all this needs: the
// writer produces one flat block per frame.
func parseCSS(t *testing.T, raw string) map[string]map[string]string {
	t.Helper()

	out := map[string]map[string]string{}

	for block := range strings.SplitSeq(raw, "}") {
		selector, body, ok := strings.Cut(block, "{")
		if !ok {
			continue
		}

		name := strings.TrimSpace(selector)
		if i := strings.LastIndex(name, "."); i >= 0 {
			name = name[i+1:]
		}

		declarations := map[string]string{}

		for line := range strings.SplitSeq(body, ";") {
			key, value, isDecl := strings.Cut(line, ":")
			if isDecl {
				declarations[strings.TrimSpace(key)] = strings.TrimSpace(value)
			}
		}

		out[name] = declarations
	}

	return out
}

func keysOf(m map[string]map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}

	return out
}
