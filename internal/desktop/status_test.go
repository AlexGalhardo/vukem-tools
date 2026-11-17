package desktop_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AlexGalhardo/vukem-tools/internal/desktop"
)

func TestReleaseLabel(t *testing.T) {
	t.Parallel()

	if got := desktop.ReleaseLabel("0.7.0"); got != "v0.7.0" {
		t.Fatalf("ReleaseLabel = %s", got)
	}

	if got := desktop.ReleaseLabel("v0.7.0"); got != "v0.7.0" {
		t.Fatalf("ReleaseLabel = %s", got)
	}

	if got := desktop.ReleaseLabel(""); got != "dev" {
		t.Fatalf("ReleaseLabel = %s", got)
	}
}

func TestOriginUp(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(srv.Close)

	if !desktop.OriginUp(context.Background(), srv.URL) {
		t.Fatal("OriginUp = false, want true")
	}

	if desktop.OriginUp(context.Background(), "http://127.0.0.1:1") {
		t.Fatal("OriginUp = true for a closed port")
	}
}

func TestStartStatusShowsTheRelease(t *testing.T) {
	t.Parallel()

	const target = "http://127.0.0.1:3000/tools/pdf-crop"

	page, err := desktop.StartStatus("0.7.0", target)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_ = page.Close(context.Background())
	})

	if !strings.HasPrefix(page.URL, "http://127.0.0.1:") {
		t.Fatalf("URL = %s", page.URL)
	}

	resp, err := http.Get(page.URL)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		t.Fatal(err)
	}

	text := string(body)
	for _, want := range []string{"v0.7.0", target, "class=\"release\"", "The web app is not running"} {
		if !strings.Contains(text, want) {
			t.Fatalf("page missing %q\n%s", want, text)
		}
	}
}
