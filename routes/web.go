// Package routes declares the HTTP routes.
package routes

import (
	"net/http"
	"time"

	vukem "github.com/AlexGalhardo/vukem"
	"github.com/AlexGalhardo/vukem/auth"
	"github.com/AlexGalhardo/vukem/inertia"
	"github.com/AlexGalhardo/vukem/middleware"
	"github.com/AlexGalhardo/vukem/ratelimit"
	"github.com/AlexGalhardo/vukem/router"

	"github.com/AlexGalhardo/vukem-tools/app/accounts"
	"github.com/AlexGalhardo/vukem-tools/app/catalog"
	"github.com/AlexGalhardo/vukem-tools/app/controllers"
	"github.com/AlexGalhardo/vukem-tools/app/services"
	"github.com/AlexGalhardo/vukem-tools/internal/httpx"
	"github.com/AlexGalhardo/vukem-tools/resources"
)

// Web registers the browser routes. vukem route:list shows them.
//
// It takes the resolved pipeline so the tool index can mark an entry as usable
// only when that tool is actually registered: the catalogue is a claim, the
// registry is the fact.
func Web(
	t *services.Telemetry,
	c *services.Conversions,
) func(r *router.Router, app *vukem.App) error {
	return func(r *router.Router, app *vukem.App) error {
		return registerWeb(t, c, r, app)
	}
}

func registerWeb(
	telemetry *services.Telemetry,
	conversions *services.Conversions,
	r *router.Router,
	app *vukem.App,
) error {
	if err := conversions.Resolve(app); err != nil {
		return err
	}

	if err := vukem.DefaultMiddlewareWith(app, r, vukem.MiddlewareOptions{
		SecureHeaders: func(cfg *middleware.SecureHeadersConfig) {
			// blob: in img-src, for the sprite pages.
			//
			// Previewing a file somebody just dropped means URL.createObjectURL,
			// which produces a blob: URL — and the default policy's
			// "img-src 'self' data:" refuses it, so the preview silently did
			// not appear. A data: URL would work and is the wrong fix: it is
			// base64, so a 64 MB sheet becomes an 85 MB string held in memory
			// beside the decoded image.
			//
			// It is a narrow widening. A blob: URL is minted by this page's
			// own JavaScript from a file the person chose, is unguessable, and
			// dies with the document; it cannot name anything on another
			// origin. The directive is images only — script-src and the rest
			// are untouched, and vukem refuses a scheme source in a script
			// directive anyway.
			cfg.CSPSources = map[string][]string{"img-src": {"blob:"}}
		},
	}); err != nil {
		return err
	}

	// Server level, so every response carries it: static files, 404s and the
	// health probe included.
	r.UseServer(httpx.DeployTarget(app.Source.Get("DEPLOY_TARGET")))
	// Outside the static handler, so a missing asset still 404s as an asset
	// while a missing page becomes a redirect to the home page.
	r.UseServer(redirectNotFound)
	r.UseServer(middleware.StaticFS(resources.Public(), middleware.StaticConfig{Prefix: "/"}))
	acc, err := accounts.New(app)
	if err != nil {
		return err
	}

	share, err := shareProps(app, conversions.Pipeline().Tools())
	if err != nil {
		return err
	}

	r.Use(app.Inertia.Middleware, share)

	pages := controllers.NewPages(
		conversions.Pipeline().Tools(),
		services.MaxUploadBytes(app),
		TusPath,
	)

	db, err := app.DB()
	if err != nil {
		return err
	}

	health := controllers.NewHealth(controllers.HealthOptions{
		DB:      db,
		Disk:    conversions.Disk(),
		Engines: conversions.Engines(),
		Now:     app.Now,
		Version: app.Source.Get("APP_VERSION"),
	})

	// Registered before the Inertia middleware runs on them: a health probe
	// must not depend on the page pipeline it is meant to outlive. The
	// shallow one is what a load balancer polls; the deep one touches every
	// dependency and is for an operator, a deploy gate and an alert.
	//
	// The name is health.live, not health: vukem already registers GET /up
	// under that name, and a collision makes the router refuse to boot. /up
	// stays for platforms that probe it by convention; /health is the
	// documented one and also reports uptime and the build.
	r.Get("/health", health.Shallow).Name("health.live")
	r.Get("/health/deep", health.Deep).Name("health.deep")

	// The scrape target, guarded by METRICS_TOKEN and absent without one.
	// Server level like the probes: a scraper has no session, and metrics
	// must stay readable when the page pipeline is not.
	r.UseServer(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.URL.Path == "/metrics" && req.Method == http.MethodGet {
				telemetry.Scrape(w, req)

				return
			}

			next.ServeHTTP(w, req)
		})
	})

	r.Get("/", pages.Index).Name("home")
	r.Get("/licenses", pages.Licenses).Name("licenses")
	// Absolute URLs built from APP_URL, not from the request's Host: see the
	// controller. No auth, because a crawler has no account — which is also
	// why the tool pages are not in it.
	r.Get("/sitemap.xml", controllers.NewSitemap(app.Config.URL).Serve).Name("sitemap")
	// The llmstxt.org index: the live tools, read by a model in one request.
	llms := controllers.NewLLMs(app.Config.Name, app.Config.URL, conversions.Pipeline().Tools())
	r.Get("/llms.txt", llms.Index).Name("llms")
	r.Get("/llms-full.txt", llms.Full).Name("llms.full")
	// A tool page needs an account: every conversion does, so showing the
	// form to somebody who cannot submit it would be a dead end.
	r.Get("/tools/{name}", pages.Tool).Name("tools.show").Use(auth.Required(acc.Web))

	// The two registry lookups behind the Brazil tools. Signed in and limited
	// per IP, because each request costs an outbound call to a free public
	// service whose own limits this app would otherwise spend for everybody.
	limiters, err := app.RateLimiters()
	if err != nil {
		return err
	}

	lookup := controllers.NewLookup(nil)
	lookupLimit := ratelimit.Middleware(limiters("lookup", 30, time.Minute), ratelimit.ByIP)
	r.Get("/lookup/cep/{cep}", lookup.CEP).Name("lookup.cep").Use(auth.Required(acc.Web), lookupLimit)
	r.Get("/lookup/cnpj/{cnpj}", lookup.CNPJ).Name("lookup.cnpj").Use(auth.Required(acc.Web), lookupLimit)

	return nil
}

// shareProps adds the props every page receives (usePage().props).
//
// socialProviders lists only the providers that actually have credentials, so
// the sign-in pages never render a button that leads to an error. Resolved
// once at boot, not per request: the credentials cannot change under a running
// process.
func shareProps(app *vukem.App, live []string) (func(http.Handler) http.Handler, error) {
	s, err := accounts.NewSocial(app, socialProviders...)
	if err != nil {
		return nil, err
	}

	configured := s.Names()
	if configured == nil {
		configured = []string{}
	}

	// The pages need to know, because with password sign-in off the email and
	// password fields would post to a route that does not exist.
	passwordAuth := PasswordAuthEnabled(app)

	// The sidebar lists the tools on every page, so they are shared rather
	// than asked for by each page. The set cannot change under a running
	// process, so it is built once.
	toolGroups := catalog.NavGroups(live)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			inertia.Share(r.Context(), "appName", app.Config.Name)
			inertia.Share(r.Context(), "socialProviders", configured)
			inertia.Share(r.Context(), "passwordAuth", passwordAuth)
			inertia.Share(r.Context(), "toolGroups", toolGroups)
			next.ServeHTTP(w, r)
		})
	}, nil
}
