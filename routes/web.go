// Package routes declares the HTTP routes.
package routes

import (
	"net/http"

	vukem "github.com/AlexGalhardo/vukem"
	"github.com/AlexGalhardo/vukem/auth"
	"github.com/AlexGalhardo/vukem/inertia"
	"github.com/AlexGalhardo/vukem/middleware"
	"github.com/AlexGalhardo/vukem/router"

	"github.com/AlexGalhardo/vukem-tools/app/accounts"
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
func Web(c *services.Conversions) func(r *router.Router, app *vukem.App) error {
	return func(r *router.Router, app *vukem.App) error {
		return registerWeb(c, r, app)
	}
}

func registerWeb(conversions *services.Conversions, r *router.Router, app *vukem.App) error {
	if err := conversions.Resolve(app); err != nil {
		return err
	}

	if err := vukem.DefaultMiddleware(app, r); err != nil {
		return err
	}

	// Server level, so every response carries it: static files, 404s and the
	// health probe included.
	r.UseServer(httpx.DeployTarget(app.Source.Get("DEPLOY_TARGET")))
	r.UseServer(middleware.StaticFS(resources.Public(), middleware.StaticConfig{Prefix: "/"}))
	acc, err := accounts.New(app)
	if err != nil {
		return err
	}

	share, err := shareProps(app)
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
	r.Get("/health", health.Shallow).Name("health")
	r.Get("/health/deep", health.Deep).Name("health.deep")

	r.Get("/", pages.Index).Name("home")
	r.Get("/licenses", pages.Licenses).Name("licenses")
	// A tool page needs an account: every conversion does, so showing the
	// form to somebody who cannot submit it would be a dead end.
	r.Get("/tools/{name}", pages.Tool).Name("tools.show").Use(auth.Required(acc.Web))

	return nil
}

// shareProps adds the props every page receives (usePage().props).
//
// socialProviders lists only the providers that actually have credentials, so
// the sign-in pages never render a button that leads to an error. Resolved
// once at boot, not per request: the credentials cannot change under a running
// process.
func shareProps(app *vukem.App) (func(http.Handler) http.Handler, error) {
	s, err := accounts.NewSocial(app, socialProviders...)
	if err != nil {
		return nil, err
	}

	configured := s.Names()
	if configured == nil {
		configured = []string{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			inertia.Share(r.Context(), "appName", app.Config.Name)
			inertia.Share(r.Context(), "socialProviders", configured)
			next.ServeHTTP(w, r)
		})
	}, nil
}
