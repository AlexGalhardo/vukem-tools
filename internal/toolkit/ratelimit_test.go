package toolkit_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AlexGalhardo/vukem-tools/internal/toolkit"
)

// QA-C1: the API limits a client to a few requests a second and answers 429
// with Retry-After. A run used to give up on the first 429, abandoning a
// conversion that was still running on the server.
func TestARateLimitedRequestIsRetriedAfterTheWaitTheServerAsks(t *testing.T) {
	t.Parallel()

	backend, err := url.Parse(fakeAPI(t, "download", http.StatusNotFound))
	if err != nil {
		t.Fatal(err)
	}

	var refused, patchRefused atomic.Int32

	proxy := httputil.NewSingleHostReverseProxy(backend)
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/uploads" && refused.Add(1) <= 2 {
			w.Header().Set("Retry-After", "1")
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"detail":"Too many requests."}`))

			return
		}

		if r.Method == http.MethodPatch && patchRefused.Add(1) <= 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)

			return
		}

		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(front.Close)

	dir := t.TempDir()
	in := filepath.Join(dir, "in.png")

	if err := os.WriteFile(in, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}

	// The fake API answers the result link with an error, so the run ends at
	// the download; what matters is that it got past the two refusals.
	r := toolkit.NewRunner(toolkit.Config{BaseURL: front.URL, Key: "vk_k.secret"}, nil)
	r.Poll = 10 * time.Millisecond

	start := time.Now()
	_, runErr := r.Run(ctxT(t), mustTool(t, "pixels-to-svg"), toolkit.Call{
		Files: []string{in}, Output: filepath.Join(dir, "o.svg"),
	})

	if code := toolkit.AsError(runErr).Code; code == toolkit.CodeRateLimited {
		t.Fatalf("the run gave up on a 429: %v", runErr)
	}

	if refused.Load() < 3 {
		t.Fatalf("the uploads list was asked %d times, want a retry after each of two refusals", refused.Load())
	}

	if patchRefused.Load() < 2 {
		t.Fatalf("the file was sent %d times, want a retry after the refusal of its upload", patchRefused.Load())
	}

	if time.Since(start) < 2*time.Second {
		t.Fatalf("the run did not wait for Retry-After: %v", time.Since(start))
	}
}

// QA-C2: an upload refused by the tus handler answers plain text, and the
// sentence was dropped for the bare status text.
func TestAPlainTextRefusalKeepsItsSentence(t *testing.T) {
	t.Parallel()

	e := toolkit.FromStatus(http.StatusUnsupportedMediaType, []byte("That file is not a type this tool can read.\n"))
	if !strings.Contains(e.Message, "That file is not a type this tool can read.") {
		t.Fatalf("message = %q, want the server's sentence", e.Message)
	}
}

func TestRefusalMessagesDoNotDoubleTheirPeriod(t *testing.T) {
	t.Parallel()

	body := []byte(`{"detail":"It needs the pdf:compress ability."}`)

	for _, status := range []int{http.StatusForbidden, http.StatusTooManyRequests} {
		if msg := toolkit.FromStatus(status, body).Message; strings.Contains(msg, "..") {
			t.Errorf("status %d: %q has a doubled period", status, msg)
		}
	}
}

// QA-C8: the result was written straight over the output path, so a download
// that broke half way destroyed a good file that was already there.
func TestABrokenDownloadLeavesAnExistingOutputIntact(t *testing.T) {
	t.Parallel()

	var uploaded atomic.Bool

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/uploads", func(w http.ResponseWriter, _ *http.Request) {
		if !uploaded.Load() {
			_, _ = w.Write([]byte(`{"data":[]}`))

			return
		}

		_, _ = w.Write([]byte(`{"data":[{"id":"up1","size_bytes":4,"created_at":"` +
			time.Now().UTC().Format(time.RFC3339) + `"}]}`))
	})
	mux.HandleFunc("POST /uploads/tus/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "/uploads/tus/abc")
		w.WriteHeader(http.StatusCreated)
	})
	mux.HandleFunc("PATCH /uploads/tus/abc", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		uploaded.Store(true)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/v1/tools/pixels-to-svg", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"c1","status":"finished"}`))
	})
	mux.HandleFunc("GET /api/v1/conversions/c1/result", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"name":"a.svg","url":"/files/x?sig=1","size_bytes":100}]}`))
	})
	mux.HandleFunc("GET /files/x", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, _ = w.Write([]byte("0123456789"))
		w.(http.Flusher).Flush()
		panic(http.ErrAbortHandler)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	in := filepath.Join(dir, "in.png")
	out := filepath.Join(dir, "report.svg")

	for path, content := range map[string]string{in: "data", out: "the good report"} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	r := toolkit.NewRunner(toolkit.Config{BaseURL: srv.URL, Key: "vk_k.secret"}, nil)
	r.Poll = 10 * time.Millisecond

	_, err := r.Run(ctxT(t), mustTool(t, "pixels-to-svg"), toolkit.Call{Files: []string{in}, Output: out})
	if err == nil {
		t.Fatal("a download cut short reported success")
	}

	if code := toolkit.AsError(err).Code; code != toolkit.CodeNetwork {
		t.Fatalf("the run failed before the download: %v", err)
	}

	if got, _ := os.ReadFile(out); string(got) != "the good report" {
		t.Fatalf("the existing output is now %q, want it untouched", got)
	}

	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Fatalf("the folder holds %d entries, want in.png and report.svg only (no temp file)", len(entries))
	}
}
