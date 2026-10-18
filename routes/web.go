// Package routes declares the HTTP routes.
package routes

import (
	"net/http"

	vukem "github.com/AlexGalhardo/vukem"
	"github.com/AlexGalhardo/vukem/inertia"
	"github.com/AlexGalhardo/vukem/middleware"
	"github.com/AlexGalhardo/vukem/router"

	"github.com/AlexGalhardo/vukem-tools/app/accounts"
	"github.com/AlexGalhardo/vukem-tools/app/controllers"
	"github.com/AlexGalhardo/vukem-tools/internal/httpx"
	"github.com/AlexGalhardo/vukem-tools/resources"
)

// Web registers the browser routes. vukem route:list shows them.
func Web(r *router.Router, app *vukem.App) error {
	if err := vukem.DefaultMiddleware(app, r); err != nil {
		return err
	}

	// Server level, so every response carries it: static files, 404s and the
	// health probe included.
	r.UseServer(httpx.DeployTarget(app.Source.Get("DEPLOY_TARGET")))
	r.UseServer(middleware.StaticFS(resources.Public(), middleware.StaticConfig{Prefix: "/"}))
	share, err := shareProps(app)
	if err != nil {
		return err
	}

	r.Use(app.Inertia.Middleware, share)

	r.Get("/", controllers.Home).Name("home")

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
