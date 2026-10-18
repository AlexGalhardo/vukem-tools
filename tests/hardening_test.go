package tests

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/AlexGalhardo/vukem/vukemtest"

	"github.com/AlexGalhardo/vukem-tools/bootstrap"
)

// The headers every response must carry.
//
// These are asserted rather than assumed because the framework sets some of
// them and the app sets others, and "the framework probably does that" is how
// a site ends up without a CSP. Asserted on a page, on a 404 and on an API
// route, because a header set by page middleware alone misses both.
func TestEveryResponseCarriesTheSecurityHeaders(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)

	paths := []struct {
		name string
		path string
	}{
		{"a page", "/"},
		{"a 404", "/no-such-page"},
		{"an API route", "/api/v1/tools"},
		{"the health probe", "/health"},
	}

	for _, p := range paths {
		t.Run(p.name, func(t *testing.T) {
			t.Parallel()

			res := app.Client(t).Get(p.path)

			// nosniff: without it a browser may decide an uploaded file is
			// HTML and run it. That matters more here than on most sites,
			// because this one serves files somebody else uploaded.
			if got := res.Header("X-Content-Type-Options"); got != "nosniff" {
				t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
			}

			// Framing: a conversion form inside somebody else's page is a
			// clickjacking surface.
			frame := res.Header("X-Frame-Options")
			csp := res.Header("Content-Security-Policy")

			if frame == "" && !strings.Contains(csp, "frame-ancestors") {
				t.Errorf("neither X-Frame-Options nor a CSP frame-ancestors directive is set\n"+
					"X-Frame-Options=%q\nCSP=%q", frame, csp)
			}

			// Referrer-Policy: a conversion URL carries a conversion id, and
			// the default policy would send it to any third party the page
			// links to.
			if got := res.Header("Referrer-Policy"); got == "" {
				t.Error("Referrer-Policy is not set")
			}

			if csp == "" {
				t.Error("Content-Security-Policy is not set")
			}
		})
	}
}

// The CSP must not defeat itself. A nonce is worth nothing next to
// unsafe-inline, because a browser that understands the nonce ignores
// unsafe-inline but an attacker only needs one of them to work.
func TestTheContentSecurityPolicyIsWorthHaving(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)

	csp := app.Client(t).Get("/").Header("Content-Security-Policy")
	if csp == "" {
		t.Fatal("no Content-Security-Policy on the home page")
	}

	for _, forbidden := range []string{"unsafe-inline", "unsafe-eval"} {
		if strings.Contains(csp, forbidden) {
			t.Errorf("the CSP contains %q, which defeats the nonce: %s", forbidden, csp)
		}
	}

	if !strings.Contains(csp, "nonce-") {
		t.Errorf("the CSP has no nonce, so the inline theme script cannot run: %s", csp)
	}

	// default-src or script-src must be bounded; a CSP without either bounds
	// nothing.
	if !strings.Contains(csp, "default-src") && !strings.Contains(csp, "script-src") {
		t.Errorf("the CSP bounds neither default-src nor script-src: %s", csp)
	}
}

// A browser form must be protected against a cross-site submission, and an
// API route authenticated by a token must not be: a token is not sent by the
// browser automatically, so there is nothing to forge, and requiring a CSRF
// token there would break every API client.
//
// vukem's csrf uses http.CrossOriginProtection — Fetch Metadata and Origin,
// not a token — so the assertion is about an unlabelled cross-origin POST
// being refused, not about a hidden field.
func TestACrossSiteFormPostIsRefused(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)

	// A POST that claims to come from somewhere else, the way a malicious
	// page's form submission would.
	res := app.Client(t).
		WithHeader("Origin", "https://evil.example").
		WithHeader("Sec-Fetch-Site", "cross-site").
		PostForm("/login", url.Values{"email": {"a@example.com"}, "password": {"whatever"}})

	if res.Status() != http.StatusForbidden {
		t.Errorf("a cross-site POST to /login returned %d, want 403", res.Status())
	}
}

// An API route is reachable cross-origin, because it is authenticated by a
// token rather than by a cookie.
func TestAnAPIRouteIsNotBlockedByTheBrowserProtection(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)

	res := app.Client(t).
		WithHeader("Origin", "https://script.example").
		WithHeader("Sec-Fetch-Site", "cross-site").
		Get("/api/v1/tools")

	if res.Status() == http.StatusForbidden {
		t.Error("a cross-origin GET to an API route was refused; a token-authenticated " +
			"API must not be behind the browser protection")
	}
}

// A request body limit must be explicit. Without one, a POST of arbitrary
// size is read into memory before any handler sees it, which is a denial of
// service that needs no account.
//
// The resumable upload endpoint is deliberately excluded: its whole purpose
// is large bodies, and it has its own 500 MB cap.
func TestAnOversizedBodyIsRefused(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)

	// Well above anything a form or a JSON body needs, well below the upload
	// cap, so this tests the ordinary limit and not the upload one.
	body := strings.NewReader(strings.Repeat("a", 12<<20))

	res := app.Client(t).Do(http.MethodPost, "/api/v1/tokens", body, "application/json")

	// 413 is the right answer. 401 would mean authentication ran first and
	// the body was read anyway, which is the thing being guarded against.
	if res.Status() != http.StatusRequestEntityTooLarge {
		t.Errorf("a 12 MiB body returned %d, want 413", res.Status())
	}
}

// The control for the test above: a small body must not be refused, or the
// 413 proves nothing about the limit. A test that passes because the endpoint
// always fails is worse than no test.
func TestASmallBodyIsNotRefusedAsTooLarge(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)

	res := app.Client(t).Do(http.MethodPost, "/api/v1/tokens",
		strings.NewReader(`{"device_name":"x","abilities":["pixels:svg"]}`), "application/json")

	if res.Status() == http.StatusRequestEntityTooLarge {
		t.Fatal("a 45-byte body was refused as too large, so the 413 above means nothing")
	}

	// Unauthenticated, so 401 is the expected answer; the point is only that
	// it is not 413.
	if res.Status() != http.StatusUnauthorized {
		t.Logf("unauthenticated POST returned %d (not 413, which is what this test checks)",
			res.Status())
	}
}

// The control for the cross-site test: a same-site POST must reach the
// handler, or the 403 above would just mean "/login always refuses".
func TestASameSiteFormPostReachesTheHandler(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)

	res := app.Client(t).
		WithHeader("Sec-Fetch-Site", "same-origin").
		PostForm("/login", url.Values{"email": {"nobody@example.com"}, "password": {"wrong-password"}})

	if res.Status() == http.StatusForbidden {
		t.Fatalf("a same-origin POST to /login was also refused with 403, so the cross-site " +
			"test proves nothing")
	}
}
