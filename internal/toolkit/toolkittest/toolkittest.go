// Package toolkittest starts the real app for tests of the CLI and the MCP
// server: an HTTP server, an account with an API key, and a queue worker.
//
// The point is that those two never convert, so their tests must run against
// the actual API rather than a fake of it.
package toolkittest

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"maps"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/AlexGalhardo/vukem/orm"
	"github.com/AlexGalhardo/vukem/queue"
	"github.com/AlexGalhardo/vukem/vukemtest"

	"github.com/AlexGalhardo/vukem-tools/app/abilities"
	"github.com/AlexGalhardo/vukem-tools/app/accounts"
	"github.com/AlexGalhardo/vukem-tools/app/models"
	"github.com/AlexGalhardo/vukem-tools/bootstrap"
	"github.com/AlexGalhardo/vukem-tools/internal/pdf"
)

const password = "correct horse battery"

// Env is a running app and a key for it.
type Env struct {
	App *vukemtest.App
	URL string
	Key string
}

// Sandbox gives the app its own directories, so a test cannot write into the
// repository and two tests cannot see each other's files.
func Sandbox(tb testing.TB) map[string]string {
	tb.Helper()

	root := filepath.Join(tb.TempDir(), "sandbox")
	if err := os.MkdirAll(root, 0o700); err != nil {
		tb.Fatalf("sandbox: %v", err)
	}

	// The local storage disk keeps a directory handle open, and Windows
	// cannot remove a directory that has one; the retry gives it time to
	// close, as tests/pipeline_test.go does.
	tb.Cleanup(func() {
		for range 40 {
			if os.RemoveAll(root) == nil {
				return
			}

			time.Sleep(25 * time.Millisecond)
		}
	})

	return map[string]string{
		"UPLOAD_DIR":  filepath.Join(root, "uploads"),
		"WORK_DIR":    filepath.Join(root, "work"),
		"STORAGE_DIR": filepath.Join(root, "bucket"),
	}
}

// Options tunes Start.
type Options struct {
	// Grant is what the key may do; every ability when empty.
	Grant []string
	// Env overrides the app's configuration, such as MAX_UPLOAD_SIZE_MB.
	Env map[string]string
}

// Start boots the app behind an httptest server and runs a worker until the
// test ends.
func Start(t *testing.T, opt ...Options) *Env {
	t.Helper()

	var o Options
	if len(opt) > 0 {
		o = opt[0]
	}

	env := Sandbox(t)
	maps.Copy(env, o.Env)

	app := vukemtest.New(t, bootstrap.New, vukemtest.Options{Env: env})

	grant := o.Grant
	if len(grant) == 0 {
		grant = abilities.All()
	}

	key := issueKey(t, app, "toolkit@example.com", grant)

	srv := httptest.NewServer(app.Handler())
	t.Cleanup(srv.Close)

	q, err := app.Queue()
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup

	wg.Go(func() { _ = q.Work(ctx, queue.WorkOptions{Concurrency: 2}) })

	t.Cleanup(func() {
		cancel()
		wg.Wait()
	})

	return &Env{App: app, URL: srv.URL, Key: key}
}

func issueKey(t *testing.T, app *vukemtest.App, email string, grant []string) string {
	t.Helper()

	acc, err := accounts.New(app.App)
	if err != nil {
		t.Fatal(err)
	}

	hash, err := acc.Hasher.Make(password)
	if err != nil {
		t.Fatal(err)
	}

	user := models.User{Name: "Toolkit", Email: email, Password: hash}
	user.EmailVerifiedAt.V, user.EmailVerifiedAt.Valid = time.Now().UTC(), true

	if err := orm.Create(t.Context(), acc.DB, &user); err != nil {
		t.Fatal(err)
	}

	r := app.Client(t).PostJSON("/api/v1/tokens", map[string]any{
		"email": email, "password": password, "device_name": "toolkit", "abilities": grant,
	}).AssertCreated()

	raw, _ := r.JSONPath("token")

	key, _ := raw.(string)
	if key == "" {
		t.Fatal("the server issued no key")
	}

	return key
}

// WritePNG writes a small four-colour PNG into dir and returns its path.
func WritePNG(tb testing.TB, dir, name string) string {
	tb.Helper()

	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	palette := []color.NRGBA{{R: 0xff, A: 0xff}, {G: 0xff, A: 0xff}, {B: 0xff, A: 0xff}, {}}

	for y := range 4 {
		for x := range 4 {
			img.SetNRGBA(x, y, palette[(x+y)%len(palette)])
		}
	}

	path := filepath.Join(dir, name)

	file, err := os.Create(path)
	if err != nil {
		tb.Fatal(err)
	}

	if err := png.Encode(file, img); err != nil {
		_ = file.Close()
		tb.Fatal(err)
	}

	if err := file.Close(); err != nil {
		tb.Fatal(err)
	}

	return path
}

// WritePDF writes a PDF of pages pages into dir and returns its path.
func WritePDF(tb testing.TB, dir, name string, pages int) string {
	tb.Helper()

	a4, ok := pdf.PageSize("a4")
	if !ok {
		tb.Fatal("a4 page size missing")
	}

	specs := make([]pdf.PageSpec, 0, pages)
	for i := 1; i <= pages; i++ {
		specs = append(specs, pdf.PageSpec{
			Box: a4,
			Text: []pdf.TextStamp{
				{Text: "Fixture page " + strconv.Itoa(i), Font: pdf.Helvetica, Size: 24, X: 72, Y: 720},
			},
		})
	}

	path := filepath.Join(dir, name)

	file, err := os.Create(path)
	if err != nil {
		tb.Fatal(err)
	}

	if err := pdf.Write(file, specs); err != nil {
		_ = file.Close()
		tb.Fatal(err)
	}

	if err := file.Close(); err != nil {
		tb.Fatal(err)
	}

	return path
}
