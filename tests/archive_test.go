package tests

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/AlexGalhardo/vukem/orm"
	"github.com/AlexGalhardo/vukem/vukemtest"

	"github.com/AlexGalhardo/vukem-tools/app/models"
	"github.com/AlexGalhardo/vukem-tools/app/tools"
	"github.com/AlexGalhardo/vukem-tools/bootstrap"
	"github.com/AlexGalhardo/vukem-tools/internal/archive"
	"github.com/AlexGalhardo/vukem-tools/internal/jobs"
)

// The archive tools' cross-cutting gates: the two directions end to end
// through HTTP, the ability that guards them, and the one place the interface
// could silently disagree with the library.

// The formats the interface offers have to be the formats the library writes.
//
// They are declared twice — once in Go, once in the form table the page is
// generated from — because a `<select>` cannot be built from a Go constant.
// This is the gate that keeps the second copy honest: a format added to
// internal/archive and not offered, or offered and not written, fails here
// rather than shipping a control whose value the API refuses.
func TestTheInterfaceOffersEveryArchiveFormatAndNoOther(t *testing.T) {
	t.Parallel()

	src := readFile(t, "../resources/js/lib/forms/archive.ts")

	choices := archiveChoices(t, src)

	want := tools.ArchiveFormats()
	if len(want) == 0 {
		t.Fatal("tools.ArchiveFormats() is empty: the assertion below would pass on anything")
	}

	if !slices.Equal(choices, want) {
		t.Errorf("the interface offers %v, the library writes %v", choices, want)
	}
}

// archiveChoices reads the format select's choices out of the form table.
func archiveChoices(t *testing.T, src string) []string {
	t.Helper()

	const marker = "choices: ["

	_, rest, found := strings.Cut(src, marker)
	if !found {
		t.Fatal("the archive form table has no choices list")
	}

	list, _, closed := strings.Cut(rest, "]")
	if !closed {
		t.Fatal("the archive form table's choices list is not closed")
	}

	var out []string

	for part := range strings.SplitSeq(list, ",") {
		if quoted := strings.TrimSpace(part); quoted != "" {
			unquoted, err := strconv.Unquote(quoted)
			if err != nil {
				t.Fatalf("choice %s is not a quoted string: %v", quoted, err)
			}

			out = append(out, unquoted)
		}
	}

	return out
}

// Creating an archive runs from several uploads to one download, and what
// comes back really is a ZIP holding both files under the names they were
// uploaded with.
func TestCreatingAnArchiveRunsFromUploadsToOneDownload(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New, vukemtest.Options{Env: sandbox(t)})
	key := issueKey(t, app, "archivecreate@example.com", "archive:files")

	first := upload(t, app, key, "one.png", spritePNG(t))
	second := upload(t, app, key, "two.png", spritePNG(t))

	submitted := app.Client(t).WithHeader("Authorization", "Bearer "+key).
		PostJSON("/api/v1/tools/archive-create", map[string]any{
			"file_ids": []string{first, second},
			"options":  map[string]any{"format": "zip", "name": "bundle"},
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

	if status, _ := done.JSONPath("status"); status != string(jobs.Finished) {
		t.Fatalf("status = %v, want finished. The whole response:\n%s", status, done.Body())
	}

	out := outputsOf(t, app, conversionID)
	if len(out) != 1 {
		t.Fatalf("the conversion produced %d files, want one archive", len(out))
	}

	if out[0].MIME != "application/zip" {
		t.Errorf("the archive is %q, want application/zip", out[0].MIME)
	}

	if out[0].Name != "bundle.zip" {
		t.Errorf("the archive is named %q, want bundle.zip", out[0].Name)
	}

	body := fetch(t, app, out[0])

	zr, err := zip.NewReader(bytes.NewReader([]byte(body)), int64(len(body)))
	if err != nil {
		t.Fatalf("the download is not a readable ZIP: %v", err)
	}

	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}

	slices.Sort(names)

	if !slices.Equal(names, []string{"one.png", "two.png"}) {
		t.Errorf("the archive holds %v, want one.png and two.png", names)
	}
}

// Extracting runs the other way: one archive in, one result file per member,
// each with its own row and its own signed link.
func TestExtractingAnArchiveProducesOneResultPerMember(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New, vukemtest.Options{Env: sandbox(t)})
	key := issueKey(t, app, "archiveextract@example.com", "archive:files")

	fileID := upload(t, app, key, "bundle.zip", zipOf(t, map[string][]byte{
		"notes.txt":  []byte("the first file"),
		"README.txt": []byte("the second file"),
	}))

	submitted := app.Client(t).WithHeader("Authorization", "Bearer "+key).
		PostJSON("/api/v1/tools/archive-extract", map[string]any{
			"file_id": fileID,
		}).AssertCreated()

	id, _ := submitted.JSONPath("id")
	conversionID, _ := id.(string)

	drain(t, app)

	done := app.Client(t).WithHeader("Authorization", "Bearer "+key).
		Get("/api/v1/conversions/" + conversionID).
		AssertOK()

	if status, _ := done.JSONPath("status"); status != string(jobs.Finished) {
		t.Fatalf("status = %v, want finished. The whole response:\n%s", status, done.Body())
	}

	out := outputsOf(t, app, conversionID)
	if len(out) != 2 {
		t.Fatalf("the extraction produced %d files, want one per member", len(out))
	}

	got := map[string]string{}
	for _, f := range out {
		got[f.Name] = fetch(t, app, f)
	}

	want := map[string]string{"notes.txt": "the first file", "README.txt": "the second file"}
	for name, body := range want {
		if got[name] != body {
			t.Errorf("%s = %q, want %q", name, got[name], body)
		}
	}
}

// A file that is not an archive is refused while the caller is still on the
// phone, rather than by a worker after the fact.
func TestANonArchiveIsRefusedAtSubmission(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New, vukemtest.Options{Env: sandbox(t)})
	key := issueKey(t, app, "archivewrongtype@example.com", "archive:files")

	fileID := upload(t, app, key, "sprite.png", spritePNG(t))

	app.Client(t).WithHeader("Authorization", "Bearer "+key).
		PostJSON("/api/v1/tools/archive-extract", map[string]any{
			"file_id": fileID,
		}).AssertJSONValidationErrors("file_id")
}

// gz holds one stream, so asking for it with several files is a validation
// error on the format rather than an archive holding only the first.
func TestGzipWithSeveralFilesIsRefusedAtSubmission(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New, vukemtest.Options{Env: sandbox(t)})
	key := issueKey(t, app, "archivegz@example.com", "archive:files")

	first := upload(t, app, key, "one.png", spritePNG(t))
	second := upload(t, app, key, "two.png", spritePNG(t))

	app.Client(t).WithHeader("Authorization", "Bearer "+key).
		PostJSON("/api/v1/tools/archive-create", map[string]any{
			"file_ids": []string{first, second},
			"options":  map[string]any{"format": "gz"},
		}).AssertStatus(http.StatusUnprocessableEntity)
}

// Both directions carry the same ability, and a key without it is refused
// with a 403 rather than a conversion nobody asked for.
func TestTheArchiveToolsNeedTheirAbility(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New, vukemtest.Options{Env: sandbox(t)})
	key := issueKey(t, app, "archivenoability@example.com", "pixels:svg")

	fileID := upload(t, app, key, "bundle.zip", zipOf(t, map[string][]byte{"a.txt": []byte("a")}))

	app.Client(t).WithHeader("Authorization", "Bearer "+key).
		PostJSON("/api/v1/tools/archive-extract", map[string]any{
			"file_id": fileID,
		}).AssertStatus(http.StatusForbidden)

	app.Client(t).WithHeader("Authorization", "Bearer "+key).
		PostJSON("/api/v1/tools/archive-create", map[string]any{
			"file_ids": []string{fileID},
		}).AssertStatus(http.StatusForbidden)
}

// An archive that would unpack past the entry cap is refused by the worker,
// with the cap named in the message the owner sees.
func TestAnOversizedArchiveIsRefusedWithTheCapNamed(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New, vukemtest.Options{Env: sandbox(t)})
	key := issueKey(t, app, "archivecap@example.com", "archive:files")

	members := map[string][]byte{}
	for i := range archive.MaxEntries + 1 {
		members["file-"+strconv.Itoa(i)+".txt"] = []byte("x")
	}

	fileID := upload(t, app, key, "many.zip", zipOf(t, members))

	submitted := app.Client(t).WithHeader("Authorization", "Bearer "+key).
		PostJSON("/api/v1/tools/archive-extract", map[string]any{
			"file_id": fileID,
		}).AssertCreated()

	id, _ := submitted.JSONPath("id")
	conversionID, _ := id.(string)

	drain(t, app)

	done := app.Client(t).WithHeader("Authorization", "Bearer "+key).
		Get("/api/v1/conversions/" + conversionID).
		AssertOK()

	if status, _ := done.JSONPath("status"); status != string(jobs.Errored) {
		t.Fatalf("status = %v, want error. The whole response:\n%s", status, done.Body())
	}

	message, _ := done.JSONPath("error_message")

	text, _ := message.(string)
	if !strings.Contains(text, strconv.Itoa(archive.MaxEntries)) {
		t.Errorf("error_message = %q, want the cap of %d named in it", text, archive.MaxEntries)
	}
}

// zipOf builds a ZIP of these members, for a test that needs one as input.
func zipOf(t *testing.T, members map[string][]byte) []byte {
	t.Helper()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	for name, body := range members {
		f, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := f.Write(body); err != nil {
			t.Fatal(err)
		}
	}

	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	return buf.Bytes()
}

// outputsOf reads a conversion's output rows, oldest first.
func outputsOf(t *testing.T, app *vukemtest.App, conversionID string) []models.File {
	t.Helper()

	db, err := app.DB()
	if err != nil {
		t.Fatal(err)
	}

	out, err := orm.QueryOf[models.File](db).
		Where("conversion_id", conversionID).
		Where("role", models.FileOutput).
		Get(t.Context())
	if err != nil {
		t.Fatalf("read the conversion's outputs: %v", err)
	}

	return out
}

// fetch follows one output's signed link, exactly as the interface does.
func fetch(t *testing.T, app *vukemtest.App, f models.File) string {
	t.Helper()

	link := signedLink(t, app, f)

	req := httptest.NewRequest(http.MethodGet, link, http.NoBody)
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("the signed download of %s returned %d:\n%s", f.Name, rec.Code, rec.Body.String())
	}

	return rec.Body.String()
}
