package tests

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/AlexGalhardo/vukem/vukemtest"

	"github.com/AlexGalhardo/vukem-tools/app/models"
	"github.com/AlexGalhardo/vukem-tools/app/tools"
	"github.com/AlexGalhardo/vukem-tools/bootstrap"
	"github.com/AlexGalhardo/vukem-tools/internal/jobs"
	"github.com/AlexGalhardo/vukem-tools/internal/spritesheet"
)

// A split runs from one sheet to an archive and a metadata file, and the
// archive holds the metadata too — which is the phase's own decision: one
// download has to be self-contained.
func TestSplittingASheetProducesAnArchiveAndItsMetadata(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New, vukemtest.Options{Env: sandbox(t)})
	key := issueKey(t, app, "spritesplit@example.com", "sprite:sheet")

	fileID := upload(t, app, key, "hero.png", sheetPNG(t, 4, 2, 32))

	submitted := app.Client(t).WithHeader("Authorization", "Bearer "+key).
		PostJSON("/api/v1/tools/sprite-split", map[string]any{
			"file_id": fileID,
			"options": map[string]any{
				"mode":     "grid",
				"cell_w":   32,
				"cell_h":   32,
				"prefix":   "walk",
				"metadata": []string{"json-hash"},
			},
		}).AssertCreated()

	id, _ := submitted.JSONPath("id")

	conversionID, _ := id.(string)
	if conversionID == "" {
		t.Fatalf("the submission returned no conversion id:\n%s", submitted.Body())
	}

	drain(t, app)

	done := app.Client(t).WithHeader("Authorization", "Bearer "+key).
		Get("/api/v1/conversions/" + conversionID).AssertOK()

	if status, _ := done.JSONPath("status"); status != string(jobs.Finished) {
		t.Fatalf("status = %v, want finished. The whole response:\n%s", status, done.Body())
	}

	out := outputsOf(t, app, conversionID)
	if len(out) != 2 {
		t.Fatalf("the split produced %d files, want the archive and the metadata", len(out))
	}

	var archive, meta string

	for _, f := range out {
		switch f.MIME {
		case "application/zip":
			archive = fetch(t, app, f)
		case "application/json":
			meta = fetch(t, app, f)
		}
	}

	if archive == "" || meta == "" {
		t.Fatalf("want one zip and one json, got %v", mimesOf(out))
	}

	names := zipNames(t, archive)

	// Eight frames, and the metadata inside the archive as well.
	frames := 0
	hasMeta := false

	for _, name := range names {
		switch {
		case strings.HasSuffix(name, ".png"):
			frames++
		case strings.HasSuffix(name, ".json"):
			hasMeta = true
		}
	}

	if frames != 8 {
		t.Errorf("the archive holds %d frames, want 8 from a 4x2 sheet: %v", frames, names)
	}

	if !hasMeta {
		t.Errorf("the archive holds no metadata, so one download is not self-contained: %v", names)
	}

	if !slices.Contains(names, "walk_0000.png") {
		t.Errorf("the frames are not named from the prefix: %v", names)
	}

	// The metadata really describes those frames.
	var atlas struct {
		Frames map[string]struct {
			Frame struct {
				X int `json:"x"`
				Y int `json:"y"`
				W int `json:"w"`
				H int `json:"h"`
			} `json:"frame"`
		} `json:"frames"`
		Meta struct {
			Image string `json:"image"`
			Size  struct {
				W int `json:"w"`
				H int `json:"h"`
			} `json:"size"`
		} `json:"meta"`
	}

	if err := json.Unmarshal([]byte(meta), &atlas); err != nil {
		t.Fatalf("the metadata is not readable JSON: %v\n%s", err, firstBytes(meta))
	}

	if len(atlas.Frames) != 8 {
		t.Errorf("the metadata describes %d frames, want 8", len(atlas.Frames))
	}

	if atlas.Meta.Size.W != 128 || atlas.Meta.Size.H != 64 {
		t.Errorf("the metadata says the sheet is %dx%d, want 128x64",
			atlas.Meta.Size.W, atlas.Meta.Size.H)
	}

	if atlas.Meta.Image != "hero.png" {
		t.Errorf("the metadata points at %q, want the uploaded name hero.png", atlas.Meta.Image)
	}

	if got := atlas.Frames["walk_0000"].Frame; got.X != 0 || got.Y != 0 || got.W != 32 || got.H != 32 {
		t.Errorf("the first frame is %+v, want 32x32 at the origin", got)
	}
}

// A build runs the other way: a ZIP of frames in, a sheet and its metadata
// out, and the sheet really is a PNG of the size the metadata claims.
func TestBuildingASheetProducesASheetAndItsMetadata(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New, vukemtest.Options{Env: sandbox(t)})
	key := issueKey(t, app, "spritebuild@example.com", "sprite:sheet")

	fileID := upload(t, app, key, "frames.zip", framesZip(t, 4, 32))

	submitted := app.Client(t).WithHeader("Authorization", "Bearer "+key).
		PostJSON("/api/v1/tools/sprite-build", map[string]any{
			"file_id": fileID,
			"options": map[string]any{"layout": "grid", "metadata": []string{"json-array"}},
		}).AssertCreated()

	id, _ := submitted.JSONPath("id")
	conversionID, _ := id.(string)

	drain(t, app)

	done := app.Client(t).WithHeader("Authorization", "Bearer "+key).
		Get("/api/v1/conversions/" + conversionID).AssertOK()

	if status, _ := done.JSONPath("status"); status != string(jobs.Finished) {
		t.Fatalf("status = %v, want finished. The whole response:\n%s", status, done.Body())
	}

	out := outputsOf(t, app, conversionID)
	if len(out) != 2 {
		t.Fatalf("the build produced %d files, want the sheet and the metadata: %v", len(out), mimesOf(out))
	}

	var sheet, meta string

	for _, f := range out {
		switch f.MIME {
		case "image/png":
			sheet = fetch(t, app, f)
		case "application/json":
			meta = fetch(t, app, f)
		}
	}

	if sheet == "" || meta == "" {
		t.Fatalf("want one png and one json, got %v", mimesOf(out))
	}

	decoded, err := png.Decode(strings.NewReader(sheet))
	if err != nil {
		t.Fatalf("the sheet is not a readable PNG: %v", err)
	}

	var atlas struct {
		Frames []struct {
			Filename string `json:"filename"`
		} `json:"frames"`
		Meta struct {
			Size struct {
				W int `json:"w"`
				H int `json:"h"`
			} `json:"size"`
		} `json:"meta"`
	}

	if err := json.Unmarshal([]byte(meta), &atlas); err != nil {
		t.Fatalf("the metadata is not readable JSON: %v", err)
	}

	if len(atlas.Frames) != 4 {
		t.Errorf("the metadata describes %d frames, want 4", len(atlas.Frames))
	}

	if atlas.Meta.Size.W != decoded.Bounds().Dx() || atlas.Meta.Size.H != decoded.Bounds().Dy() {
		t.Errorf("the metadata says %dx%d and the sheet is %s",
			atlas.Meta.Size.W, atlas.Meta.Size.H, decoded.Bounds())
	}
}

// Trimming and CSS cannot both be asked for, and the refusal happens at
// submission rather than in a worker: CSS has nowhere to record the offset,
// so the frames would be drawn in the wrong place.
func TestTrimmingWithCSSIsRefusedAtSubmission(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New, vukemtest.Options{Env: sandbox(t)})
	key := issueKey(t, app, "spritetrimcss@example.com", "sprite:sheet")

	fileID := upload(t, app, key, "hero.png", sheetPNG(t, 2, 1, 16))

	app.Client(t).WithHeader("Authorization", "Bearer "+key).
		PostJSON("/api/v1/tools/sprite-split", map[string]any{
			"file_id": fileID,
			"options": map[string]any{
				"mode": "grid", "cell_w": 16, "cell_h": 16,
				"trim": true, "metadata": []string{"css"},
			},
		}).AssertJSONValidationErrors("options.metadata")
}

// A grid that does not tile the sheet is refused with the remainder named,
// and the message reaches the owner rather than being replaced by the
// taxonomy's generic one.
func TestAGridThatDoesNotTileIsRefusedWithTheRemainder(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New, vukemtest.Options{Env: sandbox(t)})
	key := issueKey(t, app, "spritegrid@example.com", "sprite:sheet")

	// 100x100 with a 32x32 cell leaves four pixels over in each direction.
	fileID := upload(t, app, key, "hero.png", solidPNG(t, 100, 100))

	submitted := app.Client(t).WithHeader("Authorization", "Bearer "+key).
		PostJSON("/api/v1/tools/sprite-split", map[string]any{
			"file_id": fileID,
			"options": map[string]any{"mode": "grid", "cell_w": 32, "cell_h": 32},
		}).AssertCreated()

	id, _ := submitted.JSONPath("id")
	conversionID, _ := id.(string)

	drain(t, app)

	done := app.Client(t).WithHeader("Authorization", "Bearer "+key).
		Get("/api/v1/conversions/" + conversionID).AssertOK()

	if status, _ := done.JSONPath("status"); status != string(jobs.Errored) {
		t.Fatalf("status = %v, want error. The whole response:\n%s", status, done.Body())
	}

	message, _ := done.JSONPath("error_message")

	text, _ := message.(string)
	for _, want := range []string{"100", "32"} {
		if !strings.Contains(text, want) {
			t.Errorf("error_message = %q, want the sheet and cell sizes named in it", text)
		}
	}
}

// A ZIP submitted to the splitter, or a sheet to the packer, is refused at
// submission with the detected type named.
func TestEachSpriteToolRefusesTheOtherOnesInput(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New, vukemtest.Options{Env: sandbox(t)})
	key := issueKey(t, app, "spritewrongtype@example.com", "sprite:sheet")

	zipID := upload(t, app, key, "frames.zip", framesZip(t, 2, 16))
	pngID := upload(t, app, key, "hero.png", sheetPNG(t, 2, 1, 16))

	app.Client(t).WithHeader("Authorization", "Bearer "+key).
		PostJSON("/api/v1/tools/sprite-split", map[string]any{
			"file_id": zipID,
			"options": map[string]any{"mode": "grid", "cell_w": 16, "cell_h": 16},
		}).AssertJSONValidationErrors("file_id")

	app.Client(t).WithHeader("Authorization", "Bearer "+key).
		PostJSON("/api/v1/tools/sprite-build", map[string]any{"file_id": pngID}).
		AssertJSONValidationErrors("file_id")
}

// Both tools carry the same ability, and a key without it is refused.
func TestTheSpriteToolsNeedTheirAbility(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New, vukemtest.Options{Env: sandbox(t)})
	key := issueKey(t, app, "spritenoability@example.com", "pixels:svg")

	fileID := upload(t, app, key, "hero.png", sheetPNG(t, 2, 1, 16))

	app.Client(t).WithHeader("Authorization", "Bearer "+key).
		PostJSON("/api/v1/tools/sprite-split", map[string]any{
			"file_id": fileID,
			"options": map[string]any{"mode": "grid", "cell_w": 16, "cell_h": 16},
		}).AssertStatus(http.StatusForbidden)
}

// The format list is public and names every writer, because the interface
// reads it instead of holding a list of its own.
func TestTheSpriteFormatListIsPublicAndComplete(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New, vukemtest.Options{Env: sandbox(t)})

	got := app.Client(t).Get("/api/v1/tools/sprite-split/formats").AssertOK()

	var caps struct {
		Formats []struct {
			Name      string `json:"name"`
			Extension string `json:"extension"`
			Imports   string `json:"imports"`
		} `json:"formats"`
		Modes      []string `json:"modes"`
		Layouts    []string `json:"layouts"`
		MaxFormats int      `json:"max_formats"`
	}

	if err := json.Unmarshal([]byte(got.Body()), &caps); err != nil {
		t.Fatalf("the format list is not readable JSON: %v\n%s", err, got.Body())
	}

	if len(caps.Formats) != len(spritesheet.MetaFormats()) {
		t.Errorf("the endpoint publishes %d formats, the library writes %d",
			len(caps.Formats), len(spritesheet.MetaFormats()))
	}

	for _, f := range caps.Formats {
		if f.Extension == "" || f.Imports == "" {
			t.Errorf("%s is published without an extension or a sentence saying what reads it", f.Name)
		}
	}

	if !slices.Equal(caps.Modes, tools.SpriteModes()) {
		t.Errorf("modes = %v, want %v", caps.Modes, tools.SpriteModes())
	}

	if !slices.Equal(caps.Layouts, tools.SpriteLayouts()) {
		t.Errorf("layouts = %v, want %v", caps.Layouts, tools.SpriteLayouts())
	}

	if caps.MaxFormats != spritesheet.MaxMetaFormats {
		t.Errorf("max_formats = %d, want %d", caps.MaxFormats, spritesheet.MaxMetaFormats)
	}
}

// Asking for more metadata formats than the cap is a 422 on the field.
func TestTooManyMetadataFormatsAreRefused(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New, vukemtest.Options{Env: sandbox(t)})
	key := issueKey(t, app, "spritetoomany@example.com", "sprite:sheet")

	fileID := upload(t, app, key, "hero.png", sheetPNG(t, 2, 1, 16))

	all := make([]string, 0, len(spritesheet.MetaFormats()))
	for _, f := range spritesheet.MetaFormats() {
		all = append(all, string(f.Name))
	}

	if len(all) <= spritesheet.MaxMetaFormats {
		t.Skipf("only %d formats exist, so the cap of %d cannot be exceeded",
			len(all), spritesheet.MaxMetaFormats)
	}

	app.Client(t).WithHeader("Authorization", "Bearer "+key).
		PostJSON("/api/v1/tools/sprite-split", map[string]any{
			"file_id": fileID,
			"options": map[string]any{"mode": "grid", "cell_w": 16, "cell_h": 16, "metadata": all},
		}).AssertStatus(http.StatusUnprocessableEntity)
}

// mimesOf lists the outputs' media types, for a failure message that says
// what was actually produced.
func mimesOf(out []models.File) []string {
	names := make([]string, 0, len(out))
	for i := range out {
		names = append(names, out[i].MIME)
	}

	return names
}

// sheetPNG is a cols x rows sheet of cell-sized frames, each a different
// colour so a frame can be told from its neighbour.
func sheetPNG(t *testing.T, cols, rows, cell int) []byte {
	t.Helper()

	img := image.NewNRGBA(image.Rect(0, 0, cols*cell, rows*cell))

	for row := range rows {
		for col := range cols {
			shade := uint8((row*cols + col + 1) * 20)

			for y := range cell {
				for x := range cell {
					img.SetNRGBA(col*cell+x, row*cell+y,
						color.NRGBA{R: shade, G: uint8(x * 8), B: uint8(y * 8), A: 255})
				}
			}
		}
	}

	return encodePNG(t, img)
}

// solidPNG is a plain opaque image, for the cases that are about the grid
// rather than about the pixels.
func solidPNG(t *testing.T, w, h int) []byte {
	t.Helper()

	img := image.NewNRGBA(image.Rect(0, 0, w, h))

	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: 128, A: 255})
		}
	}

	return encodePNG(t, img)
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode a PNG: %v", err)
	}

	return buf.Bytes()
}

// framesZip is an archive of n identical square frames, for the packer.
func framesZip(t *testing.T, n, cell int) []byte {
	t.Helper()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	for i := range n {
		f, err := zw.Create("frame_" + string(rune('a'+i)) + ".png")
		if err != nil {
			t.Fatal(err)
		}

		if _, err := f.Write(solidPNG(t, cell, cell)); err != nil {
			t.Fatal(err)
		}
	}

	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	return buf.Bytes()
}

// zipNames lists an archive's entries.
func zipNames(t *testing.T, raw string) []string {
	t.Helper()

	zr, err := zip.NewReader(bytes.NewReader([]byte(raw)), int64(len(raw)))
	if err != nil {
		t.Fatalf("the result is not a readable ZIP: %v", err)
	}

	out := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		out = append(out, f.Name)
	}

	return out
}
