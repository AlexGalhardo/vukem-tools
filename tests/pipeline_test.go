package tests

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/AlexGalhardo/vukem/orm"
	"github.com/AlexGalhardo/vukem/queue"
	"github.com/AlexGalhardo/vukem/vukemtest"

	"github.com/AlexGalhardo/vukem-tools/app/models"
	"github.com/AlexGalhardo/vukem-tools/app/services"
	"github.com/AlexGalhardo/vukem-tools/bootstrap"
	"github.com/AlexGalhardo/vukem-tools/internal/files"
	"github.com/AlexGalhardo/vukem-tools/internal/jobs"
	"github.com/AlexGalhardo/vukem-tools/routes"
)

// sandbox gives the app its own directories for uploads, scratch space and
// the local stand-in for the bucket, so a test cannot write into the
// repository and two tests cannot see each other's files.
func sandbox(t *testing.T) map[string]string {
	t.Helper()

	root := t.TempDir()

	return map[string]string{
		"UPLOAD_DIR":  filepath.Join(root, "uploads"),
		"WORK_DIR":    filepath.Join(root, "work"),
		"STORAGE_DIR": filepath.Join(root, "bucket"),
	}
}

// spritePNG is a real 4x4 PNG with four colours, so the conversion has
// something to merge and something to keep apart.
func spritePNG(t *testing.T) []byte {
	t.Helper()

	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	palette := []color.NRGBA{
		{R: 0xff, A: 0xff},
		{G: 0xff, A: 0xff},
		{B: 0xff, A: 0xff},
		{},
	}

	for y := range 4 {
		for x := range 4 {
			img.SetNRGBA(x, y, palette[(x+y)%len(palette)])
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode the sprite: %v", err)
	}

	return buf.Bytes()
}

// upload runs the tus protocol: create, then write. It goes through the app's
// own handler so the whole middleware stack is exercised, and returns the
// stored file's id.
func upload(t *testing.T, app *vukemtest.App, key, name string, body []byte) string {
	t.Helper()

	create := httptest.NewRequest(http.MethodPost, routes.TusPath, nil)
	create.Header.Set("Tus-Resumable", "1.0.0")
	create.Header.Set("Upload-Length", strconv.Itoa(len(body)))
	create.Header.Set("Upload-Metadata", "filename "+base64.StdEncoding.EncodeToString([]byte(name)))
	create.Header.Set("Authorization", "Bearer "+key)

	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, create)

	if rec.Code != http.StatusCreated {
		t.Fatalf("tus create returned %d:\n%s", rec.Code, rec.Body.String())
	}

	location := rec.Header().Get("Location")
	if location == "" {
		t.Fatal("tus create sent no Location")
	}

	write := httptest.NewRequest(http.MethodPatch, location, bytes.NewReader(body))
	write.Header.Set("Tus-Resumable", "1.0.0")
	write.Header.Set("Upload-Offset", "0")
	write.Header.Set("Content-Type", "application/offset+octet-stream")
	write.Header.Set("Authorization", "Bearer "+key)

	rec = httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, write)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("tus write returned %d:\n%s", rec.Code, rec.Body.String())
	}

	// The tus protocol has no body to carry the stored id, so it is read back
	// from the one row the upload created.
	db, err := app.DB()
	if err != nil {
		t.Fatal(err)
	}

	f, err := orm.QueryOf[models.File](db).
		Where("name", name).
		OrderBy("id", orm.Desc).
		First(t.Context())
	if err != nil {
		t.Fatalf("the upload stored no file row: %v", err)
	}

	return f.ID
}

// drain runs the queue until it is empty, the way a worker process does.
func drain(t *testing.T, app *vukemtest.App) {
	t.Helper()

	q, err := app.Queue()
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	if err := q.Work(ctx, queue.WorkOptions{StopWhenEmpty: true, Concurrency: 1}); err != nil {
		t.Fatalf("work: %v", err)
	}
}

// The whole point of phases S1.6, S1.7 and S2.2 in one test: a file is
// uploaded, a conversion is submitted, a worker runs it, and the result comes
// back down a signed link as an SVG. If this passes, the pipeline is real.
func TestAConversionRunsFromUploadToDownload(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New, vukemtest.Options{Env: sandbox(t)})
	key := issueKey(t, app, "pipeline@example.com", "pixels:svg")

	fileID := upload(t, app, key, "sprite.png", spritePNG(t))

	submitted := app.Client(t).WithHeader("Authorization", "Bearer "+key).
		PostJSON("/api/v1/tools/pixels-to-svg", map[string]any{
			"file_id": fileID,
			"options": map[string]any{"mode": "rect"},
		}).AssertCreated()

	id, _ := submitted.JSONPath("id")

	conversionID, _ := id.(string)
	if conversionID == "" {
		t.Fatalf("the submission returned no conversion id:\n%s", submitted.Body())
	}

	if status, _ := submitted.JSONPath("status"); status != string(jobs.Pending) {
		t.Fatalf("a new conversion is %v, want pending", status)
	}

	drain(t, app)

	done := app.Client(t).WithHeader("Authorization", "Bearer "+key).
		Get("/api/v1/conversions/" + conversionID).
		AssertOK()

	status, _ := done.JSONPath("status")
	if status != string(jobs.Finished) {
		t.Fatalf("status = %v, want finished. The whole response:\n%s", status, done.Body())
	}

	if out, _ := done.JSONPath("bytes_out"); toInt(t, out) <= 0 {
		t.Fatalf("bytes_out = %v, want the size of the SVG", out)
	}

	// The result is downloadable through a signed link, and what comes back
	// is really an SVG rather than an error page with a 200.
	svg := download(t, app, conversionID)

	if !strings.HasPrefix(strings.TrimSpace(svg), "<svg") {
		t.Fatalf("the download is not an SVG:\n%s", firstBytes(svg))
	}

	if !strings.Contains(svg, "<rect") {
		t.Fatalf("rect mode produced no rectangles:\n%s", firstBytes(svg))
	}
}

// download finds the conversion's output, signs a link for it and follows it,
// exactly as the interface does.
func download(t *testing.T, app *vukemtest.App, conversionID string) string {
	t.Helper()

	db, err := app.DB()
	if err != nil {
		t.Fatal(err)
	}

	out, err := orm.QueryOf[models.File](db).
		Where("conversion_id", conversionID).
		Where("role", models.FileOutput).
		First(t.Context())
	if err != nil {
		t.Fatalf("the conversion produced no output file: %v", err)
	}

	if out.MIME != "image/svg+xml" {
		t.Fatalf("the output is %q, want image/svg+xml", out.MIME)
	}

	if !strings.HasSuffix(out.Name, ".svg") {
		t.Fatalf("the output is named %q, want an .svg name", out.Name)
	}

	link := signedLink(t, app, out)

	req := httptest.NewRequest(http.MethodGet, link, nil)
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("the signed download returned %d:\n%s", rec.Code, rec.Body.String())
	}

	if got := rec.Header().Get("Content-Type"); got != "image/svg+xml" {
		t.Fatalf("Content-Type = %q", got)
	}

	// Serving user-supplied bytes inline on our own origin is how an SVG
	// becomes a script, so this header is load-bearing rather than cosmetic.
	if got := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "attachment;") {
		t.Fatalf("Content-Disposition = %q, want an attachment", got)
	}

	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}

	return rec.Body.String()
}

// signedLink builds a download link through the app's own signer, so the test
// exercises the real signature rather than a hand-rolled one.
func signedLink(t *testing.T, app *vukemtest.App, f models.File) string {
	t.Helper()

	store := storeOf(t, app)

	link, err := store.DownloadURL(f, 5*time.Minute)
	if err != nil {
		t.Fatalf("sign a download link: %v", err)
	}

	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}

	// The handler is reached by path; the host in the link is APP_URL.
	return u.RequestURI()
}

// An unsigned or tampered link must not serve a file. The signature is the
// only authority on this route, so this is the test that stops it becoming a
// public file server.
func TestADownloadNeedsAValidSignature(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New, vukemtest.Options{Env: sandbox(t)})
	key := issueKey(t, app, "unsigned@example.com", "pixels:svg")

	fileID := upload(t, app, key, "sprite.png", spritePNG(t))

	for name, target := range map[string]string{
		"no signature at all": files.DownloadPath + fileID,
		"an invented one":     files.DownloadPath + fileID + "?expires=9999999999&signature=deadbeef",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, target, nil)
			rec := httptest.NewRecorder()
			app.Handler().ServeHTTP(rec, req)

			if rec.Code == http.StatusOK {
				t.Fatalf("%s served the file", name)
			}
		})
	}
}

// An upload may be converted once. Submitting the same file twice must be
// refused rather than quietly sharing one file between two conversions whose
// lifetimes then disagree.
func TestAnUploadCanBeConvertedOnlyOnce(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New, vukemtest.Options{Env: sandbox(t)})
	key := issueKey(t, app, "twice@example.com", "pixels:svg")

	fileID := upload(t, app, key, "sprite.png", spritePNG(t))
	body := map[string]any{"file_id": fileID}

	app.Client(t).WithHeader("Authorization", "Bearer "+key).
		PostJSON("/api/v1/tools/pixels-to-svg", body).AssertCreated()

	app.Client(t).WithHeader("Authorization", "Bearer "+key).
		PostJSON("/api/v1/tools/pixels-to-svg", body).
		AssertJSONValidationErrors("file_id")
}

// A key must carry the tool's ability. A key minted for one tool cannot start
// another, which is the whole reason abilities exist.
func TestAKeyWithoutTheAbilityCannotSubmit(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New, vukemtest.Options{Env: sandbox(t)})
	key := issueKey(t, app, "wrongability@example.com", "pdf:compress")

	fileID := upload(t, app, key, "sprite.png", spritePNG(t))

	app.Client(t).WithHeader("Authorization", "Bearer "+key).
		PostJSON("/api/v1/tools/pixels-to-svg", map[string]any{
			"file_id": fileID,
		}).AssertStatus(http.StatusForbidden)
}

// A file of a type the tool cannot read is refused at submission, while the
// caller is still on the phone, rather than by a worker after the fact.
func TestAFileTheToolCannotReadIsRefusedAtSubmission(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New, vukemtest.Options{Env: sandbox(t)})
	key := issueKey(t, app, "wrongtype@example.com", "pixels:svg")

	// A real PDF header, so it is identified as a PDF rather than refused as
	// unrecognised bytes.
	fileID := upload(t, app, key, "paper.pdf", []byte("%PDF-1.7\n1 0 obj\n<< /Type /Catalog >>\n"))

	app.Client(t).WithHeader("Authorization", "Bearer "+key).
		PostJSON("/api/v1/tools/pixels-to-svg", map[string]any{
			"file_id": fileID,
		}).AssertJSONValidationErrors("file_id")
}

// The ROADMAP's own assert: a file renamed to a type it is not must be
// rejected, with a message naming what it actually is.
func TestARenamedFileIsRejectedOnUpload(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New, vukemtest.Options{Env: sandbox(t)})
	key := issueKey(t, app, "renamed@example.com", "pixels:svg")

	// An MP4, named as if it were a PNG.
	body := []byte("\x00\x00\x00\x20ftypisom\x00\x00\x02\x00isomiso2avc1mp41")

	create := httptest.NewRequest(http.MethodPost, routes.TusPath, nil)
	create.Header.Set("Tus-Resumable", "1.0.0")
	create.Header.Set("Upload-Length", strconv.Itoa(len(body)))
	create.Header.Set("Upload-Metadata", "filename "+base64.StdEncoding.EncodeToString([]byte("holiday.png")))
	create.Header.Set("Authorization", "Bearer "+key)

	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, create)
	if rec.Code != http.StatusCreated {
		t.Fatalf("tus create returned %d", rec.Code)
	}

	write := httptest.NewRequest(http.MethodPatch, rec.Header().Get("Location"), bytes.NewReader(body))
	write.Header.Set("Tus-Resumable", "1.0.0")
	write.Header.Set("Upload-Offset", "0")
	write.Header.Set("Content-Type", "application/offset+octet-stream")
	write.Header.Set("Authorization", "Bearer "+key)

	rec = httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, write)

	if rec.Code == http.StatusNoContent {
		t.Fatal("an MP4 named .png was accepted")
	}

	if !strings.Contains(rec.Body.String(), "MP4") {
		t.Fatalf("the refusal does not name the detected type:\n%s", rec.Body.String())
	}

	// Nothing may have been stored.
	db, err := app.DB()
	if err != nil {
		t.Fatal(err)
	}

	count, err := orm.QueryOf[models.File](db).Count(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if count != 0 {
		t.Fatalf("%d files were stored for a rejected upload", count)
	}
}

// An upload endpoint that is open is free disk for anybody who finds it.
func TestTheUploadEndpointNeedsAuthentication(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New, vukemtest.Options{Env: sandbox(t)})

	create := httptest.NewRequest(http.MethodPost, routes.TusPath, nil)
	create.Header.Set("Tus-Resumable", "1.0.0")
	create.Header.Set("Upload-Length", "10")

	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, create)

	if rec.Code == http.StatusCreated {
		t.Fatal("an anonymous upload was accepted")
	}
}

// The expired-result sweep must delete the object as well as the row, and
// running it twice must be harmless — the ROADMAP's idempotency assert, here
// against the real app rather than a unit rig.
func TestTheSweepRemovesAnExpiredResultFromTheBucket(t *testing.T) {
	t.Parallel()

	env := sandbox(t)
	// One minute, so the test can push past it without waiting.
	env["RESULT_TTL_MINUTES"] = "1"

	app := vukemtest.New(t, bootstrap.New, vukemtest.Options{Env: env})
	key := issueKey(t, app, "sweep@example.com", "pixels:svg")

	fileID := upload(t, app, key, "sprite.png", spritePNG(t))

	app.Client(t).WithHeader("Authorization", "Bearer "+key).
		PostJSON("/api/v1/tools/pixels-to-svg", map[string]any{
			"file_id": fileID,
		}).AssertCreated()

	drain(t, app)

	store := storeOf(t, app)

	// Nothing is expired yet.
	deleted, err := store.Sweep(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}

	if deleted != 0 {
		t.Fatalf("the sweep deleted %d files that are not expired", deleted)
	}

	db, err := app.DB()
	if err != nil {
		t.Fatal(err)
	}

	// Age every file past its lifetime, which is what the clock would do.
	past := time.Now().UTC().Add(-time.Hour)
	if _, err := orm.QueryOf[models.File](db).
		Where("role", models.FileOutput).
		Update(t.Context(), orm.Set{"expires_at": past}); err != nil {
		t.Fatal(err)
	}

	deleted, err = store.Sweep(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}

	if deleted != 1 {
		t.Fatalf("the sweep deleted %d files, want 1", deleted)
	}

	// The object itself must be gone from the store, not just its row.
	bucket := env["STORAGE_DIR"]

	var left int

	if err := filepath.WalkDir(bucket, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !d.IsDir() && !strings.HasPrefix(d.Name(), ".") {
			left++
		}

		return nil
	}); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}

	if left != 1 {
		// The input survives: only the output expired.
		t.Fatalf("%d objects left in the bucket, want only the input", left)
	}

	again, err := store.Sweep(t.Context(), 0)
	if err != nil {
		t.Fatalf("the second sweep failed: %v", err)
	}

	if again != 0 {
		t.Fatalf("the second sweep deleted %d files, want 0", again)
	}
}

// storeOf resolves a file store over the running app's own database, disk and
// signing key, so a test signs and sweeps exactly what the app would.
//
// It resolves a second services.Conversions rather than reaching into the
// app's: the signer is derived from APP_KEY and the disk from the same
// configuration, so the two are interchangeable, and a test helper that needs
// no production accessor is one less thing exported for testing's sake.
func storeOf(t *testing.T, app *vukemtest.App) *files.Store {
	t.Helper()

	c := services.NewConversions()
	if err := c.Resolve(app.App); err != nil {
		t.Fatalf("resolve the conversion services: %v", err)
	}

	return c.Files()
}

// toInt reads a JSON number, which decodes as float64.
func toInt(t *testing.T, v any) int64 {
	t.Helper()

	switch n := v.(type) {
	case float64:
		return int64(n)
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			t.Fatalf("%v is not an integer: %v", v, err)
		}

		return i
	default:
		t.Fatalf("%v is %T, want a number", v, v)

		return 0
	}
}

// firstBytes keeps a failure message readable when the body is a whole SVG.
func firstBytes(s string) string {
	if len(s) <= 400 {
		return s
	}

	return s[:400] + "..."
}
