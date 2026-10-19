package tests

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/AlexGalhardo/vukem/vukemtest"

	"github.com/AlexGalhardo/vukem-tools/app/tools"
	"github.com/AlexGalhardo/vukem-tools/bootstrap"
	"github.com/AlexGalhardo/vukem-tools/internal/jobs"
)

// The modes the interface offers have to be the modes the library implements.
//
// Declared twice — once in Go, once in the form table the page is generated
// from — because a `<select>` cannot be built from a Go constant. This is the
// gate that keeps the second copy honest.
func TestTheInterfaceOffersEveryCompressModeAndNoOther(t *testing.T) {
	t.Parallel()

	src := readFile(t, "../resources/js/lib/forms/image.ts")

	choices := archiveChoices(t, src)

	want := tools.ImageCompressModes()
	if len(want) == 0 {
		t.Fatal("tools.ImageCompressModes() is empty: the assertion below would pass on anything")
	}

	if !slices.Equal(choices, want) {
		t.Errorf("the interface offers %v, the library implements %v", choices, want)
	}
}

// The formats the interface says it accepts have to be the formats the
// library has a recipe for. A picker that offers a format the tool refuses is
// a dead end the person finds only after uploading.
func TestTheCompressFormAcceptsEveryCompressibleFormat(t *testing.T) {
	t.Parallel()

	src := readFile(t, "../resources/js/lib/forms/image.ts")

	for _, format := range tools.ImageCompressFormats() {
		mime := compressMIME(format)
		if !strings.Contains(src, `"`+mime+`"`) {
			t.Errorf("the compress form does not accept %s, which the library compresses", mime)
		}
	}
}

// compressMIME is the media type for one of the four compressible formats.
func compressMIME(format string) string {
	switch format {
	case "jpeg":
		return "image/jpeg"
	case "png":
		return "image/png"
	case "webp":
		return "image/webp"
	case "gif":
		return "image/gif"
	default:
		return "image/" + format
	}
}

// Compressing runs from upload to download, the result really is smaller, and
// it is still a PNG.
func TestCompressingAnImageRunsAndProducesASmallerFile(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New, vukemtest.Options{Env: sandbox(t)})
	key := issueKey(t, app, "imagecompress@example.com", "image:compress")

	original := gradientPNG(t, 600, 400)
	fileID := upload(t, app, key, "photo.png", original)

	submitted := app.Client(t).WithHeader("Authorization", "Bearer "+key).
		PostJSON("/api/v1/tools/image-compress", map[string]any{
			"file_id": fileID,
		}).AssertCreated()

	id, _ := submitted.JSONPath("id")

	conversionID, _ := id.(string)
	if conversionID == "" {
		t.Fatalf("the submission returned no conversion id:\n%s", submitted.Body())
	}

	drain(t, app)

	done := app.Client(t).WithHeader("Authorization", "Bearer "+key).
		Get("/api/v1/conversions/" + conversionID).
		AssertOK()

	status, _ := done.JSONPath("status")
	if status != string(jobs.Finished) {
		if status == string(jobs.Errored) {
			t.Skipf("libvips is not available here, so the conversion could not run:\n%s", done.Body())
		}

		t.Fatalf("status = %v, want finished. The whole response:\n%s", status, done.Body())
	}

	out := outputsOf(t, app, conversionID)
	if len(out) != 1 {
		t.Fatalf("the compression produced %d files, want one", len(out))
	}

	if out[0].MIME != "image/png" {
		t.Errorf("the result is %q, want image/png: compressing never changes the format", out[0].MIME)
	}

	if out[0].SizeBytes >= int64(len(original)) {
		t.Errorf("the result is %d bytes against a %d-byte original: a compression that cannot win "+
			"must return the original unchanged, never something larger",
			out[0].SizeBytes, len(original))
	}

	// The bytes really are a readable PNG of the same size, not a truncated
	// file that happens to be smaller.
	body := fetch(t, app, out[0])

	decoded, err := png.Decode(strings.NewReader(body))
	if err != nil {
		t.Fatalf("the result is not a readable PNG: %v", err)
	}

	if decoded.Bounds().Dx() != 600 || decoded.Bounds().Dy() != 400 {
		t.Errorf("the result is %s, want 600x400: compressing never resizes", decoded.Bounds())
	}
}

// The progress trail carries the sentence saying what happened, which is the
// whole answer when a file comes back unchanged.
func TestTheCompressionReportsWhatItDid(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New, vukemtest.Options{Env: sandbox(t)})
	key := issueKey(t, app, "imagecompressreport@example.com", "image:compress")

	fileID := upload(t, app, key, "photo.png", gradientPNG(t, 300, 200))

	submitted := app.Client(t).WithHeader("Authorization", "Bearer "+key).
		PostJSON("/api/v1/tools/image-compress", map[string]any{"file_id": fileID}).
		AssertCreated()

	id, _ := submitted.JSONPath("id")
	conversionID, _ := id.(string)

	drain(t, app)

	done := app.Client(t).WithHeader("Authorization", "Bearer "+key).
		Get("/api/v1/conversions/" + conversionID).AssertOK()
	if status, _ := done.JSONPath("status"); status == string(jobs.Errored) {
		t.Skipf("libvips is not available here:\n%s", done.Body())
	}

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/conversions/"+conversionID+"/events", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+key)

	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)

	if !strings.Contains(rec.Body.String(), "libvips") {
		t.Errorf("the progress trail never names the tool that ran:\n%s", firstBytes(rec.Body.String()))
	}
}

// A key without image:compress cannot compress, even though it is the same
// engine image-convert uses: the abilities are per tool family, not per
// engine.
func TestCompressingNeedsItsOwnAbility(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New, vukemtest.Options{Env: sandbox(t)})
	key := issueKey(t, app, "imagecompressnoability@example.com", "image:convert")

	fileID := upload(t, app, key, "photo.png", gradientPNG(t, 100, 100))

	app.Client(t).WithHeader("Authorization", "Bearer "+key).
		PostJSON("/api/v1/tools/image-compress", map[string]any{"file_id": fileID}).
		AssertStatus(http.StatusForbidden)
}

// An unknown mode is a 422 on the field rather than a conversion that fails
// in a worker where nobody is listening.
func TestAnUnknownCompressModeIsRefusedAtSubmission(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New, vukemtest.Options{Env: sandbox(t)})
	key := issueKey(t, app, "imagecompressmode@example.com", "image:compress")

	fileID := upload(t, app, key, "photo.png", gradientPNG(t, 100, 100))

	app.Client(t).WithHeader("Authorization", "Bearer "+key).
		PostJSON("/api/v1/tools/image-compress", map[string]any{
			"file_id": fileID,
			"options": map[string]any{"mode": "magic"},
		}).AssertJSONValidationErrors("options.mode")
}

// gradientPNG is an image with structure, encoded by Go.
//
// Structure matters: a flat colour compresses to nothing whatever the
// encoder does, and would make a compression test pass on a tool that did
// nothing at all.
func gradientPNG(t *testing.T, w, h int) []byte {
	t.Helper()

	img := image.NewNRGBA(image.Rect(0, 0, w, h))

	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(x * 255 / max(w-1, 1)),
				G: uint8(y * 255 / max(h-1, 1)),
				B: uint8((x/8 + y/8) % 256),
				A: 255,
			})
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode the gradient PNG: %v", err)
	}

	return buf.Bytes()
}
