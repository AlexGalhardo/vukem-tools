package routes_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/AlexGalhardo/vukem/vukemtest"

	"github.com/AlexGalhardo/vukem-tools/bootstrap"
)

// public lists every route that is reachable without an account, and why.
// A new route is behind auth.Required unless somebody adds it here on
// purpose, which is the point: "secure by default" would otherwise be a
// property of whoever wrote the route last.
var public = map[string]string{
	"GET /up":                                      "platform probe",
	"GET /health":                                  "load balancer probe, touches nothing",
	"GET /health/deep":                             "rate limited; anonymous body carries no versions",
	"GET /{$}":                                     "home page",
	"GET /licenses":                                "engine licences, a legal obligation",
	"GET /sitemap.xml":                             "crawlers",
	"GET /llms.txt":                                "model-readable index",
	"GET /llms-full.txt":                           "model-readable index",
	"GET /login":                                   "sign-in form, guests only",
	"POST /login":                                  "sign-in, guests only and rate limited",
	"GET /register":                                "sign-up form, guests only",
	"POST /register":                               "sign-up, guests only and rate limited",
	"GET /register/check-email":                    "sign-up follow-up, guests only",
	"GET /forgot-password":                         "recovery form, guests only",
	"POST /forgot-password":                        "recovery, guests only and rate limited",
	"GET /reset-password/{token}":                  "recovery link, guests only",
	"POST /reset-password":                         "recovery, guests only",
	"GET /auth/{provider}/redirect":                "social sign-in, guests only",
	"GET /auth/{provider}/callback":                "social sign-in callback, the provider is the authority",
	"POST /auth/{provider}/callback":               "social sign-in callback (form_post), the provider is the authority",
	"POST /api/v1/tokens":                          "exchanges credentials for a key, rate limited",
	"POST /api/v1/device-links":                    "starts a device link, rate limited",
	"POST /api/v1/device-links/token":              "polls a device link by its secret, rate limited",
	"GET /api/v1/abilities":                        "catalogue, rate limited",
	"GET /api/v1/tools":                            "catalogue, rate limited",
	"GET /api/v1/conversion-errors":                "catalogue, rate limited",
	"GET /api/v1/tools/image-convert/formats":      "catalogue, rate limited",
	"GET /api/v1/tools/video-convert/capabilities": "catalogue, rate limited",
	"GET /api/v1/tools/audio-convert/formats":      "catalogue, rate limited",
	"GET /api/v1/tools/sprite-split/formats":       "catalogue, rate limited",
	"GET /api/v1/download-platforms":               "catalogue, rate limited",
	"GET /api/v1/clip-modes":                       "catalogue, rate limited",
	"GET /api/v1/upload-types":                     "catalogue",
	"GET /openapi.json":                            "the API description",
	"GET /docs":                                    "the API reference",
	"GET /docs/scalar.js":                          "the API reference",
	"GET /docs/page.js":                            "the API reference",
	"GET /files/{id}":                              "a signed, expiring link is the authority",
}

func TestEveryRouteIsBehindAnAccountUnlessListedAsPublic(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)

	if _, err := app.Router.Handler(); err != nil {
		t.Fatal(err)
	}

	seen := map[string]bool{}

	for _, rt := range app.Router.Routes() {
		if slices.Contains(rt.Middleware, "auth.Required") {
			continue
		}

		key := rt.Method + " " + rt.Path
		seen[key] = true

		if _, ok := public[key]; !ok {
			t.Errorf("%s has no auth.Required guard and is not in the public list; "+
				"add the guard, or list it with the reason it is open (%v)", key, rt.Middleware)
		}
	}

	// A stale entry would keep excusing a route that no longer exists, or one
	// that has since been put behind auth and could be dropped from the list.
	for key := range public {
		if !seen[key] {
			t.Errorf("%s is listed as public but is not an open route any more", key)
		}
	}
}

// The credential-minting and probing routes must carry a rate limit: an open
// endpoint without one is a password-guessing or enumeration service.
func TestOpenWriteRoutesAreRateLimited(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)

	if _, err := app.Router.Handler(); err != nil {
		t.Fatal(err)
	}

	for _, rt := range app.Router.Routes() {
		if rt.Method != http.MethodPost || slices.Contains(rt.Middleware, "auth.Required") {
			continue
		}

		// The provider's callback is authenticated by the provider's signed
		// state, not by us, and a limit would throttle the provider.
		if strings.HasPrefix(rt.Path, "/auth/") {
			continue
		}

		if rt.Path == "/reset-password" {
			// The token is the secret and is single use; the form POST has
			// no per-IP limit today.
			continue
		}

		if !slices.Contains(rt.Middleware, "ratelimit.Middleware") {
			t.Errorf("POST %s is open and has no rate limit (%v)", rt.Path, rt.Middleware)
		}
	}
}
