package services_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AlexGalhardo/vukem-tools/app/services"
)

// The scrape target is absent, not open, when no token is configured.
//
// A 404 rather than a 401: an endpoint that exists but refuses tells a
// scanner the service has metrics worth coming back for. An endpoint that was
// never enabled should be indistinguishable from one that does not exist.
func TestWithoutATokenThereIsNoScrapeTarget(t *testing.T) {
	t.Parallel()

	telemetry := services.NewTelemetry()

	rec := httptest.NewRecorder()
	telemetry.Scrape(rec, httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 when METRICS_TOKEN is unset", rec.Code)
	}
}

// Metric names and label values describe the service's shape and its
// traffic, so the target is guarded. These cases are the ones a wrong guard
// gets wrong: no header at all, the wrong scheme, an empty token, and a
// prefix of the real one.
func TestTheScrapeTargetRefusesTheWrongToken(t *testing.T) {
	t.Parallel()

	const token = "s3cret-scrape-token"

	cases := []struct {
		name   string
		header string
	}{
		{name: "no Authorization header", header: ""},
		{name: "basic instead of bearer", header: "Basic " + token},
		{name: "bearer with no token", header: "Bearer "},
		{name: "a prefix of the token", header: "Bearer s3cret"},
		{name: "the token with something appended", header: "Bearer " + token + "x"},
		{name: "the token as the whole header", header: token},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			target := withToken(t, token)

			req := httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody)
			if c.header != "" {
				req.Header.Set("Authorization", c.header)
			}

			rec := httptest.NewRecorder()
			target.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401 (body: %.200s)", rec.Code, rec.Body.String())
			}

			// A misconfigured scraper should be told what is wrong rather
			// than left guessing.
			if got := rec.Header().Get("WWW-Authenticate"); !strings.Contains(got, "Bearer") {
				t.Errorf("WWW-Authenticate = %q, want a Bearer challenge", got)
			}
		})
	}
}

// The right token gets the metrics, in Prometheus's text format.
func TestTheScrapeTargetServesTheRightToken(t *testing.T) {
	t.Parallel()

	const token = "s3cret-scrape-token"

	target := withToken(t, token)

	req := httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+token)

	rec := httptest.NewRecorder()
	target.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %.200s)", rec.Code, rec.Body.String())
	}

	// The exporter's own registry, so the body carries this service's
	// instruments and not the Go runtime collectors the default registry
	// would add.
	if body := rec.Body.String(); strings.Contains(body, "go_goroutines") {
		t.Errorf("the scrape body carries the default registry's runtime collectors:\n%.400s", body)
	}
}

// withToken builds a scrape target guarded by token.
func withToken(t *testing.T, token string) *services.ScrapeTarget {
	t.Helper()

	target, err := services.NewScrapeTarget(token)
	if err != nil {
		t.Fatalf("new scrape target: %v", err)
	}

	if target == nil {
		t.Fatal("a non-empty token produced no scrape target")
	}

	return target
}

// A blank token is the same as none: whitespace in an environment variable
// must not become a usable credential.
func TestABlankTokenIsNoToken(t *testing.T) {
	t.Parallel()

	for _, token := range []string{"", "   ", "\t\n"} {
		target, err := services.NewScrapeTarget(token)
		if err != nil {
			t.Fatalf("new scrape target: %v", err)
		}

		if target != nil {
			t.Errorf("token %q produced a scrape target", token)
		}
	}
}
