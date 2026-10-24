package routes

import (
	"net/http"
	"strings"
	"time"

	"github.com/AlexGalhardo/vukem"
	"github.com/AlexGalhardo/vukem/api"
	"github.com/AlexGalhardo/vukem/auth"
	"github.com/AlexGalhardo/vukem/middleware"
	"github.com/AlexGalhardo/vukem/ratelimit"
	"github.com/AlexGalhardo/vukem/router"

	"github.com/AlexGalhardo/vukem-tools/app/accounts"
	"github.com/AlexGalhardo/vukem-tools/app/controllers/apiv1"
	"github.com/AlexGalhardo/vukem-tools/app/devicelink"
	"github.com/AlexGalhardo/vukem-tools/internal/httpx"
)

// API registers /api/v1: token issuance and revocation, /me, CORS from
// CORS_ALLOWED_ORIGINS and rate limiting (vukem install:api).
func API(r *router.Router, app *vukem.App) error {
	acc, err := accounts.New(app)
	if err != nil {
		return err
	}

	// RATE_LIMIT_STORE=cache shares the limits between servers (CACHE_STORE=database or Redis).
	limiters, err := app.RateLimiters()
	if err != nil {
		return err
	}

	var origins []string

	for o := range strings.SplitSeq(app.Source.Get("CORS_ALLOWED_ORIGINS"), ",") {
		if o = strings.TrimSpace(o); o != "" {
			origins = append(origins, o)
		}
	}

	cors, err := middleware.CORS(middleware.CORSConfig{AllowedOrigins: origins, MaxAge: time.Hour})
	if err != nil {
		return err
	}

	r.UseServer(cors) // preflight OPTIONS requests have no route

	h := apiv1.NewTokens(acc)

	// Short-lived link state lives in the cache, so it needs no table and
	// disappears on its own.
	appCache, err := app.Cache()
	if err != nil {
		return err
	}

	links := apiv1.NewDeviceLinks(devicelink.New(appCache.Store(), nil), acc, app.Config.URL)
	// The IP limit runs first: tokens are only trusted as a rate-limit key
	// once auth.Required has verified them.
	perIP := ratelimit.Middleware(limiters("api.ip", 120, time.Minute), ratelimit.ByIP)
	perToken := ratelimit.Middleware(limiters("api.token", 60, time.Minute), ratelimit.ByToken)
	issue := ratelimit.Middleware(limiters("api.tokens", 5, time.Minute), ratelimit.ByIP)

	// Every route is registered with api.Register: the input and output types
	// are what routes/apidocs.go generates /openapi.json from, so a route
	// that cannot be described cannot be registered.
	r.Group("/api/v1", func(g *router.Group) {
		// httpx.AcceptAPIKeyHeader runs before the guard: it rewrites
		// X-API-Key into Authorization, which is the only place vukem's token
		// guard looks (see framework-feedback).
		g.Use(middleware.JSONOnly(), httpx.AcceptAPIKeyHeader, perIP)

		api.Register(g, "POST /tokens", api.Op{
			Name:    "tokens.store",
			Summary: "Issue an API key",
			Description: "Exchanges email and password for an API key limited to the abilities asked for. " +
				"The secret is returned once and never stored: only its SHA-256 hash is.",
			Tags: []string{tagTokens},
		}, h.Issue).Use(issue)

		// A program that cannot show a login form starts a link, the person
		// approves it in the browser, and the program collects a key. Both
		// calls are public: the link's codes are the credential, and starting
		// one grants nothing until a signed-in person approves it.
		api.Register(g, "POST /device-links", api.Op{
			Name:    "devicelinks.start",
			Summary: "Start linking a program to an account",
			Description: "For a program that cannot show a login form, such as a chat bot. Returns a secret " +
				"device_code to poll with and a short user_code to show the person, who opens verification_url " +
				"in a browser, checks the code and approves. What the key may do is fixed by the client kind, " +
				"not asked for. Rate limited per IP.",
			Tags: []string{tagTokens},
		}, links.Start).Use(issue)

		api.Register(g, "POST /device-links/token", api.Op{
			Name:    "devicelinks.token",
			Summary: "Collect the key of an approved link",
			Description: "Poll every `interval` seconds. status is pending until the person decides, then " +
				"approved with the key (shown once and made at that moment), denied, or expired. A link that " +
				"never existed reads as expired.",
			Tags: []string{tagTokens},
			// A poll creates nothing, so it answers 200 rather than the 201
			// a POST gets by default.
			Status: http.StatusOK,
		}, links.Token)

		api.Register(g, "GET /abilities", api.Op{
			Name:        "abilities",
			Summary:     "List the abilities a key may be granted",
			Description: "The closed set of abilities, so a client does not have to hardcode them.",
			Tags:        []string{tagAbilities},
		}, h.Abilities)

		g.Group("", func(p *router.Group) {
			// Both guards, session first. The order is load-bearing: Required
			// stops at the first guard whose failure is not "unauthenticated", so
			// a token guard in front answers 401 to a cookie-only request before
			// the session guard runs. The app's own pages call these endpoints
			// with the session they already have.
			//
			// Accepting the session here is safe because an unsafe request still
			// has to clear two checks it cannot forge cross-site: vukem's
			// CrossOriginProtection reads Sec-Fetch-Site and Origin, and
			// JSONOnly requires a Content-Type that a simple form POST cannot
			// set without a preflight. requireAbility already expects this case:
			// a session has no abilities and is allowed everything its owner is,
			// while a key is held to the abilities it was minted with.
			p.Use(auth.Required(acc.Web, acc.API), perToken)

			api.Register(p, "GET /me", api.Op{
				Name:        "me",
				Summary:     "Show the authenticated account",
				Description: "The account the key belongs to. Useful to check a key is live.",
				Tags:        []string{tagAccount},
				Security:    []string{SchemeBearer},
			}, h.Me)

			api.Register(p, "GET /tokens", api.Op{
				Name:        "tokens.index",
				Summary:     "List the caller's API keys",
				Description: "Newest first, revoked keys included. Secrets are never listed.",
				Tags:        []string{tagTokens},
				Security:    []string{SchemeBearer},
			}, h.Index)

			api.Register(p, "DELETE /tokens/current", api.Op{
				Name:        "tokens.destroy",
				Summary:     "Revoke the key used by this request",
				Description: "The row is kept for the audit trail, marked revoked.",
				Tags:        []string{tagTokens},
				Security:    []string{SchemeBearer},
			}, h.Revoke)

			api.Register(p, "DELETE /tokens/{token}", api.Op{
				Name:    "tokens.revoke",
				Summary: "Revoke one of the caller's keys by id",
				Description: "Scoped to the caller: another account's key id is answered exactly like " +
					"an id that does not exist.",
				Tags:     []string{tagTokens},
				Security: []string{SchemeBearer},
			}, h.RevokeOne)
		})
	}).Name("api.v1.")

	return nil
}
