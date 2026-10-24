package controllers_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/AlexGalhardo/vukem/web"

	"github.com/AlexGalhardo/vukem-tools/app/catalog"
	"github.com/AlexGalhardo/vukem-tools/app/controllers"
)

const llmsBase = "https://tools.example"

func TestLLMsServeTheIndexAndTheFullFile(t *testing.T) {
	t.Parallel()

	live := []string{"image-convert"}
	llms := controllers.NewLLMs("Vukem Tools", llmsBase+"/", live)

	for _, tc := range []struct {
		name    string
		handler func(*web.Ctx) error
		target  string
	}{
		{"index", llms.Index, "/llms.txt"},
		{"full", llms.Full, "/llms-full.txt"},
	} {
		rec, err := call(tc.handler, tc.target)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}

		if rec.Code != http.StatusOK {
			t.Fatalf("%s answers %d, want 200", tc.name, rec.Code)
		}

		if got := rec.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
			t.Errorf("%s Content-Type is %q", tc.name, got)
		}

		if got := rec.Header().Get("Cache-Control"); got != "public, max-age=3600" {
			t.Errorf("%s Cache-Control is %q", tc.name, got)
		}

		body := rec.Body.String()

		if !strings.HasPrefix(body, "# Vukem Tools\n\n> ") {
			t.Errorf("%s does not open with the H1 and the blockquote", tc.name)
		}

		// The base comes from APP_URL. The test request's Host is example.com.
		if strings.Contains(body, "example.com") {
			t.Errorf("%s takes its URLs from the request Host", tc.name)
		}

		if !strings.Contains(body, "https://tools.example/docs") {
			t.Errorf("%s does not link /docs under APP_URL", tc.name)
		}

		if !strings.Contains(body, "https://tools.example/tools/image-convert") {
			t.Errorf("%s does not link the live tool", tc.name)
		}
	}
}

func TestLLMsListOnlyLiveTools(t *testing.T) {
	t.Parallel()

	live := []string{"image-convert"}
	llms := controllers.NewLLMs("Vukem Tools", llmsBase, live)

	rec, err := call(llms.Index, "/llms.txt")
	if err != nil {
		t.Fatalf("index: %v", err)
	}

	body := rec.Body.String()

	for _, tool := range catalog.All(live) {
		linked := strings.Contains(body, "/tools/"+tool.Name+")")

		if tool.Status == catalog.Live && !linked {
			t.Errorf("live tool %s is missing from the index", tool.Name)
		}

		if tool.Status != catalog.Live && linked {
			t.Errorf("%s tool %s is listed, but it is not usable", tool.Status, tool.Name)
		}
	}
}

func TestLLMsFullCarriesTheAPIGuide(t *testing.T) {
	t.Parallel()

	llms := controllers.NewLLMs("Vukem Tools", llmsBase, []string{"image-convert"})

	rec, err := call(llms.Full, "/llms-full.txt")
	if err != nil {
		t.Fatalf("full: %v", err)
	}

	body := rec.Body.String()

	for _, want := range []string{"/api/v1/tokens", "Retry-After", "application/problem+json", "request_id"} {
		if !strings.Contains(body, want) {
			t.Errorf("llms-full.txt does not mention %q", want)
		}
	}
}
