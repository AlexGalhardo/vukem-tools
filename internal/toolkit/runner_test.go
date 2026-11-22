package toolkit_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/AlexGalhardo/vukem-tools/app/abilities"
	"github.com/AlexGalhardo/vukem-tools/clients/apiclient"
	"github.com/AlexGalhardo/vukem-tools/internal/testenv"
	"github.com/AlexGalhardo/vukem-tools/internal/toolkit"
	"github.com/AlexGalhardo/vukem-tools/internal/toolkit/toolkittest"
)

func runner(env *toolkittest.Env) *toolkit.Runner {
	r := toolkit.NewRunner(toolkit.Config{BaseURL: env.URL, Key: env.Key}, nil)
	r.Poll = 50 * time.Millisecond

	return r
}

func ctxT(t *testing.T) context.Context {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	t.Cleanup(cancel)

	return ctx
}

func TestEveryPipelineToolHasAGeneratedClientMethod(t *testing.T) {
	t.Parallel()

	client := reflect.TypeFor[*apiclient.Client]()

	for _, tool := range toolkit.All() {
		if tool.Kind != toolkit.Pipeline {
			continue
		}

		var method strings.Builder

		method.WriteString("Tools")

		for part := range strings.SplitSeq(tool.Name, "-") {
			method.WriteString(strings.ToUpper(part[:1]) + part[1:])
		}

		if _, ok := client.MethodByName(method.String()); !ok {
			t.Errorf(
				"%s has no generated client method %s: regenerate clients/ (docs/api.md)",
				tool.Name,
				method.String(),
			)
		}
	}
}

func TestAnImageGoesThroughTheAPIToAnSVGOnDisk(t *testing.T) {
	t.Parallel()

	env := toolkittest.Start(t)
	dir := t.TempDir()
	in := toolkittest.WritePNG(t, dir, "sprite.png")
	out := filepath.Join(dir, "out", "sprite.svg")

	res, err := runner(env).Run(ctxT(t), mustTool(t, "pixels-to-svg"), toolkit.Call{
		Files: []string{in}, Options: map[string]any{"mode": "rect"}, Output: out,
	})
	if err != nil {
		t.Fatal(err)
	}

	raw, readErr := os.ReadFile(out)
	if readErr != nil {
		t.Fatal(readErr)
	}

	if !strings.HasPrefix(strings.TrimSpace(string(raw)), "<svg") {
		t.Fatalf("the output is not an SVG: %.80s", raw)
	}

	if len(res.Outputs) != 1 || res.Outputs[0].Path != out || res.Outputs[0].SizeBytes != int64(len(raw)) {
		t.Fatalf("result = %+v, want one output at %s of %d bytes", res, out, len(raw))
	}

	if res.Status != "finished" || res.ConversionID == "" || res.ExpiresAt == nil {
		t.Fatalf("result = %+v, want a finished conversion with an expiry", res)
	}
}

func TestSeveralFilesGoInAndSeveralComeOut(t *testing.T) {
	t.Parallel()

	env := toolkittest.Start(t)
	dir := t.TempDir()
	a := toolkittest.WritePNG(t, dir, "a.png")
	b := toolkittest.WritePNG(t, dir, "b.png")
	zip := filepath.Join(dir, "pack.zip")

	r := runner(env)

	if _, err := r.Run(ctxT(t), mustTool(t, "archive-create"), toolkit.Call{
		Files: []string{a, b}, Options: map[string]any{"format": "zip", "name": "pack"}, Output: zip,
	}); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(dir, "unpacked")

	res, err := r.Run(ctxT(t), mustTool(t, "archive-extract"), toolkit.Call{Files: []string{zip}, Output: dest})
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Outputs) != 2 {
		t.Fatalf("extract gave %d files, want 2: %+v", len(res.Outputs), res.Outputs)
	}

	for _, o := range res.Outputs {
		if info, statErr := os.Stat(o.Path); statErr != nil || info.Size() != o.SizeBytes || o.SizeBytes == 0 {
			t.Errorf("%s: stat %v, reported %d bytes", o.Path, statErr, o.SizeBytes)
		}
	}
}

func TestAPDFIsCutThroughTheAPI(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("qpdf"); err != nil {
		testenv.MissingEngine(t, "qpdf is not installed")
	}

	if runtime.GOOS == "windows" {
		// The server runs every engine under prlimit, which only Linux has.
		testenv.MissingEngine(t, "the server cannot run engine subprocesses on Windows (prlimit)")
	}

	env := toolkittest.Start(t)
	dir := t.TempDir()
	in := toolkittest.WritePDF(t, dir, "in.pdf", 2)
	out := filepath.Join(dir, "cropped.pdf")

	res, err := runner(env).Run(ctxT(t), mustTool(t, "pdf-crop"), toolkit.Call{
		Files: []string{in}, Options: map[string]any{"left": 36, "bottom": 36, "right": 36, "top": 36}, Output: out,
	})
	if err != nil {
		t.Fatal(err)
	}

	head, _ := os.ReadFile(out)
	if !strings.HasPrefix(string(head), "%PDF") || res.Outputs[0].SizeBytes == 0 {
		t.Fatalf("the output is not a PDF: %.20q (%+v)", head, res)
	}
}

// Failures carry a code an agent can branch on, and never the key.
func TestFailuresHaveStableCodesAndNeverShowTheKey(t *testing.T) {
	t.Parallel()

	env := toolkittest.Start(t, toolkittest.Options{
		Grant: []string{abilities.ImageConvert},
		Env:   map[string]string{"MAX_UPLOAD_SIZE_MB": "1"},
	})
	dir := t.TempDir()
	png := toolkittest.WritePNG(t, dir, "a.png")
	big := filepath.Join(dir, "big.png")

	if err := os.WriteFile(big, make([]byte, 2<<20), 0o600); err != nil {
		t.Fatal(err)
	}

	secret := "vk_deadbeef.this-is-a-secret-that-must-not-leak"

	cases := []struct {
		name    string
		key     string
		tool    string
		file    string
		options map[string]any
		want    string
	}{
		{"a bad key", secret, "pixels-to-svg", png, nil, toolkit.CodeUnauthorized},
		{"no key", "", "pixels-to-svg", png, nil, toolkit.CodeMissingKey},
		{"a key without the ability", env.Key, "pixels-to-svg", png, nil, toolkit.CodeMissingAbility},
		{"a file over the limit", env.Key, "image-convert", big, map[string]any{"to": "png"}, toolkit.CodeFileTooLarge},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			r := toolkit.NewRunner(toolkit.Config{BaseURL: env.URL, Key: tc.key}, nil)

			_, err := r.Run(ctxT(t), mustTool(t, tc.tool), toolkit.Call{
				Files: []string{tc.file}, Options: tc.options, Output: filepath.Join(dir, "o.bin"),
			})
			if err == nil {
				t.Fatal("the call succeeded")
			}

			te := toolkit.AsError(err)
			if te.Code != tc.want {
				t.Fatalf("code = %q (%s), want %q", te.Code, te.Message, tc.want)
			}

			for _, key := range []string{secret, env.Key} {
				if strings.Contains(err.Error(), key) || strings.Contains(te.Message, key) {
					t.Fatalf("the error shows the key: %v", err)
				}
			}
		})
	}
}

func TestRefusedStatusesMapToStableCodes(t *testing.T) {
	t.Parallel()

	for status, want := range map[int]string{
		http.StatusUnauthorized:          toolkit.CodeUnauthorized,
		http.StatusForbidden:             toolkit.CodeMissingAbility,
		http.StatusGone:                  toolkit.CodeResultExpired,
		http.StatusTooManyRequests:       toolkit.CodeRateLimited,
		http.StatusRequestEntityTooLarge: toolkit.CodeFileTooLarge,
		http.StatusUnprocessableEntity:   toolkit.CodeInvalidInput,
		http.StatusInternalServerError:   toolkit.CodeServer,
	} {
		if got := toolkit.FromStatus(status, nil).Code; got != want {
			t.Errorf("status %d -> %q, want %q", status, got, want)
		}
	}
}

// fakeAPI is the smallest server a run can pass through, with one step
// failing on demand. The pipeline itself is exercised against the real app
// above; this is for the failures the real app cannot be made to produce on
// request, such as an upload that expired between two calls.
func fakeAPI(t *testing.T, failAt string, status int) string {
	t.Helper()

	problem := func(w http.ResponseWriter, code int) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(code)
		_, _ = w.Write([]byte(`{"detail":"gone for the test"}`))
	}

	uploaded := false
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/v1/uploads", func(w http.ResponseWriter, _ *http.Request) {
		if !uploaded {
			_, _ = w.Write([]byte(`{"data":[]}`))

			return
		}

		_, _ = w.Write(
			[]byte(
				`{"data":[{"id":"up1","size_bytes":4,"created_at":"` + time.Now().UTC().Format(time.RFC3339) + `"}]}`,
			),
		)
	})
	mux.HandleFunc("POST /uploads/tus/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "/uploads/tus/abc")
		w.WriteHeader(http.StatusCreated)
	})
	mux.HandleFunc("PATCH /uploads/tus/abc", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		uploaded = true

		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/v1/tools/pixels-to-svg", func(w http.ResponseWriter, _ *http.Request) {
		if failAt == "submit" {
			problem(w, status)

			return
		}

		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"c1","status":"finished"}`))
	})
	mux.HandleFunc("GET /api/v1/conversions/c1/result", func(w http.ResponseWriter, _ *http.Request) {
		if failAt == "result" {
			problem(w, status)

			return
		}

		_, _ = w.Write([]byte(`{"data":[{"name":"a.svg","url":"/files/x?sig=1","size_bytes":3}]}`))
	})
	mux.HandleFunc("GET /files/x", func(w http.ResponseWriter, _ *http.Request) { problem(w, status) })

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv.URL
}

func TestExpiredThingsAreReportedWithTheirOwnCodes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		failAt string
		status int
		want   string
	}{
		{"an upload that expired before submit", "submit", http.StatusNotFound, toolkit.CodeNotFound},
		{"a result that expired before it was listed", "result", http.StatusGone, toolkit.CodeResultExpired},
		{
			"a result link that expired before it was fetched",
			"download",
			http.StatusNotFound,
			toolkit.CodeResultExpired,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			in := filepath.Join(dir, "in.png")

			if err := os.WriteFile(in, []byte("data"), 0o600); err != nil {
				t.Fatal(err)
			}

			r := toolkit.NewRunner(toolkit.Config{BaseURL: fakeAPI(t, tc.failAt, tc.status), Key: "vk_k.secret"}, nil)
			r.Poll = 10 * time.Millisecond

			_, err := r.Run(ctxT(t), mustTool(t, "pixels-to-svg"), toolkit.Call{
				Files: []string{in}, Output: filepath.Join(dir, "o.svg"),
			})
			if got := toolkit.AsError(err).Code; got != tc.want {
				t.Fatalf("code = %q (%v), want %q", got, err, tc.want)
			}

			if _, statErr := os.Stat(filepath.Join(dir, "o.svg")); statErr == nil {
				t.Error("a failed download left a file behind")
			}
		})
	}
}
