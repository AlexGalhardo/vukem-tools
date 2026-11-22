package desktop_test

import (
	"context"
	"errors"
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

// The Windows build has no console, so a start that fails (a corrupt
// database, a folder the user may not write to) used to close the app with
// nothing on screen. The window shows the reason instead.
func TestStartFailureShowsTheReasonInTheWindow(t *testing.T) {
	t.Parallel()

	cause := errors.New(
		`open C:\Users\ana\AppData\Roaming\vukem-tools\desktop.lock: Access is denied <script>alert(1)</script>`,
	)

	page, err := desktop.StartFailure("v1.2.3", cause, `C:\Users\ana\AppData\Roaming\vukem-tools`)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = page.Close(context.Background()) })

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, page.URL, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = resp.Body.Close() }()

	raw, _ := io.ReadAll(resp.Body)
	body := string(raw)

	for _, want := range []string{"could not start", "Access is denied", `C:\Users\ana\AppData\Roaming\vukem-tools`, "v1.2.3"} {
		if !strings.Contains(body, want) {
			t.Errorf("the page does not say %q:\n%s", want, body)
		}
	}

	if strings.Contains(body, "<script>") {
		t.Errorf("the error text is not escaped:\n%s", body)
	}
}
