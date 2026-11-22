package tests

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AlexGalhardo/vukem/orm"
	"github.com/AlexGalhardo/vukem/queue"
	"github.com/AlexGalhardo/vukem/vukemtest"

	"github.com/AlexGalhardo/vukem-tools/app/models"
	"github.com/AlexGalhardo/vukem-tools/bootstrap"
	"github.com/AlexGalhardo/vukem-tools/internal/jobs"
)

// localApp boots the app in local mode: one SQLite file, the queue in the
// database and storage on disk, all under a data directory of the test's own.
func localApp(t *testing.T) (*vukemtest.App, string) {
	t.Helper()

	// A subdirectory removed with retries, for the reason sandbox explains.
	dir := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { removeWithRetry(t, dir) })

	return vukemtest.New(t, bootstrap.New, vukemtest.Options{Env: bootstrap.LocalEnv(dir)}), dir
}

// submitPixels queues one pixels-to-svg conversion and returns its id.
func submitPixels(t *testing.T, app *vukemtest.App, key, fileID string) string {
	t.Helper()

	r := app.Client(t).WithHeader("Authorization", "Bearer "+key).
		PostJSON("/api/v1/tools/pixels-to-svg", map[string]any{
			"file_id": fileID,
			"options": map[string]any{"mode": "rect"},
		}).AssertCreated()

	id, _ := r.JSONPath("id")

	s, _ := id.(string)
	if s == "" {
		t.Fatalf("no conversion id:\n%s", r.Body())
	}

	return s
}

// The acceptance test of local mode: one process, one SQLite file, a queued
// conversion that finishes with progress events and leaves its result on the
// local disk, with nothing written outside the data directory.
func TestLocalModeRunsAConversionOnSQLite(t *testing.T) {
	t.Parallel()

	app, dir := localApp(t)

	db, err := app.DB()
	if err != nil {
		t.Fatal(err)
	}

	if got := db.Dialect().Name(); got != "sqlite" {
		t.Fatalf("local mode runs on %q, want sqlite", got)
	}

	if _, err := os.Stat(filepath.Join(dir, "vukem-tools.db")); err != nil {
		t.Fatalf("the database file is not in the data directory: %v", err)
	}

	key := issueKey(t, app, "local@example.com", "pixels:svg")
	fileID := upload(t, app, key, "sprite.png", spritePNG(t))
	id := submitPixels(t, app, key, fileID)

	drain(t, app)

	done := app.Client(t).WithHeader("Authorization", "Bearer "+key).
		Get("/api/v1/conversions/" + id).AssertOK()

	if status, _ := done.JSONPath("status"); status != string(jobs.Finished) {
		t.Fatalf("status = %v, want finished:\n%s", status, done.Body())
	}

	events, err := orm.QueryOf[models.ConversionEvent](db).
		Where("conversion_id", id).
		Get(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if len(events) < 2 {
		t.Fatalf("%d progress events were recorded, want a trail", len(events))
	}

	if svg := download(t, app, id); !strings.HasPrefix(strings.TrimSpace(svg), "<svg") {
		t.Fatalf("the download is not an SVG:\n%s", firstBytes(svg))
	}

	if n := countFiles(t, filepath.Join(dir, "files")); n == 0 {
		t.Fatal("no result was written under <data dir>/files")
	}
}

// Five conversions at once on one SQLite file: the single writer and the busy
// timeout must absorb them without "database is locked".
func TestLocalModeSurvivesFiveConcurrentConversions(t *testing.T) {
	t.Parallel()

	app, _ := localApp(t)
	key := issueKey(t, app, "five@example.com", "pixels:svg")

	const n = 5

	ids := make([]string, n)

	for i := range n {
		fileID := upload(t, app, key, "sprite.png", spritePNG(t))
		ids[i] = submitPixels(t, app, key, fileID)
	}

	q, err := app.Queue()
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	// Five workers, and concurrent reads from the API while they write.
	var wg sync.WaitGroup

	wg.Go(func() {
		for _, id := range ids {
			app.Client(t).WithHeader("Authorization", "Bearer "+key).
				Get("/api/v1/conversions/" + id)
		}
	})

	if err := q.Work(ctx, queue.WorkOptions{StopWhenEmpty: true, Concurrency: n}); err != nil {
		t.Fatalf("work: %v", err)
	}

	wg.Wait()

	for _, id := range ids {
		r := app.Client(t).WithHeader("Authorization", "Bearer "+key).
			Get("/api/v1/conversions/" + id)

		if r.Status() != http.StatusOK {
			t.Fatalf("conversion %s: %d", id, r.Status())
		}

		if status, _ := r.JSONPath("status"); status != string(jobs.Finished) {
			t.Fatalf("conversion %s is %v, want finished:\n%s", id, status, r.Body())
		}
	}

	if logs := app.Logs(); strings.Contains(logs, "database is locked") {
		t.Fatalf("SQLite reported a lock:\n%s", logs)
	}
}

// A local result expires in five minutes and the sweep deletes it from the
// disk, exactly as in production.
func TestLocalModeResultsExpireAndAreSwept(t *testing.T) {
	t.Parallel()

	app, dir := localApp(t)
	key := issueKey(t, app, "expire@example.com", "pixels:svg")

	id := submitPixels(t, app, key, upload(t, app, key, "sprite.png", spritePNG(t)))
	drain(t, app)

	db, err := app.DB()
	if err != nil {
		t.Fatal(err)
	}

	out, err := orm.QueryOf[models.File](db).
		Where("conversion_id", id).
		Where("role", models.FileOutput).
		First(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if got := out.ExpiresAt.Sub(out.CreatedAt); got < 4*time.Minute || got > 6*time.Minute {
		t.Fatalf("a result lives %s, want about five minutes", got)
	}

	before := countFiles(t, filepath.Join(dir, "files"))

	past := time.Now().UTC().Add(-time.Hour)
	if _, err := orm.QueryOf[models.File](db).
		Where("role", models.FileOutput).
		Update(t.Context(), orm.Set{"expires_at": past}); err != nil {
		t.Fatal(err)
	}

	if deleted, err := storeOf(t, app).Sweep(t.Context(), 0); err != nil || deleted != 1 {
		t.Fatalf("sweep = %d, %v, want 1 deleted", deleted, err)
	}

	if after := countFiles(t, filepath.Join(dir, "files")); after != before-1 {
		t.Fatalf("%d objects on disk after the sweep, want %d", after, before-1)
	}
}

// countFiles counts the regular, non-hidden files under dir.
func countFiles(t *testing.T, dir string) int {
	t.Helper()

	var n int

	err := filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !d.IsDir() && !strings.HasPrefix(d.Name(), ".") {
			n++
		}

		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}

	return n
}
