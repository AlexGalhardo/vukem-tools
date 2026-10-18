package routes

import (
	"time"

	vukem "github.com/AlexGalhardo/vukem"
	"github.com/AlexGalhardo/vukem/api"
	"github.com/AlexGalhardo/vukem/auth"
	"github.com/AlexGalhardo/vukem/middleware"
	"github.com/AlexGalhardo/vukem/openapi"
	"github.com/AlexGalhardo/vukem/ratelimit"
	"github.com/AlexGalhardo/vukem/router"

	"github.com/AlexGalhardo/vukem-tools/app/abilities"
	"github.com/AlexGalhardo/vukem-tools/app/accounts"
	"github.com/AlexGalhardo/vukem-tools/app/controllers/apiv1"
	"github.com/AlexGalhardo/vukem-tools/app/services"
	"github.com/AlexGalhardo/vukem-tools/internal/httpx"
)

// Tags for the conversion operations in the OpenAPI document.
const (
	tagConversions = "Conversions"
	tagCatalog     = "Catalog"
)

// StreamPath is where a conversion's progress stream lives. It is exported so
// the tests and the hand-written API documentation name the same route.
const StreamPath = "/api/v1/conversions/{id}/events"

// Conversions registers the conversion routes. It takes the resolved
// pipeline rather than building its own, because the event hub must exist
// exactly once per process.
func Conversions(c *services.Conversions) func(r *router.Router, app *vukem.App) error {
	return func(r *router.Router, app *vukem.App) error {
		if err := c.Resolve(app); err != nil {
			return err
		}

		acc, err := accounts.New(app)
		if err != nil {
			return err
		}

		limiters, err := app.RateLimiters()
		if err != nil {
			return err
		}

		h := apiv1.NewConversions(c.Pipeline(), c.Store(), c.Files(), c.Hub())

		perIP := ratelimit.Middleware(limiters("api.ip", 120, time.Minute), ratelimit.ByIP)
		perToken := ratelimit.Middleware(limiters("api.token", 60, time.Minute), ratelimit.ByToken)
		// A stream costs a connection and a database listener for as long as
		// it is open, so opening them is limited harder than reading a row.
		openStream := ratelimit.Middleware(limiters("api.stream", 20, time.Minute), ratelimit.ByToken)
		// Starting work is limited harder than reading about it: a conversion
		// costs CPU, and the quota in S1.8 is the budget, not the brake.
		submit := ratelimit.Middleware(limiters("api.submit", 30, time.Minute), ratelimit.ByToken)

		r.Group("/api/v1", func(g *router.Group) {
			g.Use(middleware.JSONOnly(), httpx.AcceptAPIKeyHeader, perIP)

			// The catalogue is public: a client should be able to learn the
			// tools and the error codes before it has a key, and neither
			// reveals anything about an account.
			api.Register(g, "GET /tools", api.Op{
				Name:        "tools.index",
				Summary:     "List the conversions on offer",
				Description: "Every tool that can be submitted, by name.",
				Tags:        []string{tagCatalog},
			}, h.Tools)

			api.Register(g, "GET /conversion-errors", api.Op{
				Name:    "conversions.errors",
				Summary: "List the conversion error codes",
				Description: "Every code a failed conversion can carry, with the sentence to show and " +
					"whether the same input is worth submitting again. Published so a client does not " +
					"hardcode them.",
				Tags: []string{tagCatalog},
			}, h.Errors)

			g.Group("", func(p *router.Group) {
				p.Use(auth.Required(acc.API), perToken)

				api.Register(p, "POST /conversions", api.Op{
					Name:    "conversions.store",
					Summary: "Submit a conversion",
					Description: "Runs a tool over an upload of yours. The upload and the conversion " +
						"are bound in one transaction, so a conversion always has its input. Send an " +
						"idempotency_key to make a retry safe.",
					Tags:     []string{tagConversions},
					Security: []string{SchemeBearer},
				}, h.Store).Use(submit)

				api.Register(p, "GET /conversions", api.Op{
					Name:        "conversions.index",
					Summary:     "List the caller's conversions",
					Description: "Newest first. Scoped to the caller: another account's work is never listed.",
					Tags:        []string{tagConversions},
					Security:    []string{SchemeBearer},
				}, h.Index)

				api.Register(p, "GET /conversions/{id}", api.Op{
					Name:    "conversions.show",
					Summary: "Show one conversion",
					Description: "The polling endpoint: ask for it until status is finished, error or " +
						"cancelled. A client that can hold a connection open should use the event " +
						"stream instead.",
					Tags:     []string{tagConversions},
					Security: []string{SchemeBearer},
				}, h.Show)

				api.Register(p, "DELETE /conversions/{id}", api.Op{
					Name:    "conversions.cancel",
					Summary: "Cancel a conversion",
					Description: "Stops a conversion that has not ended and returns it as it now stands. " +
						"One that already ended is answered 409.",
					Tags:     []string{tagConversions},
					Security: []string{SchemeBearer},
				}, h.Cancel)

				// A stream has no response body an OpenAPI schema can
				// describe, so it is hidden from the generated document and
				// written up by hand in docs/api.md instead. Leaving it in
				// would make the generator emit a route with no content type.
				p.Get("/conversions/{id}/events", h.Stream).
					Name("conversions.events").
					Use(openStream).
					Doc(openapi.Skip{})
			})
		}).Name("api.v1.")

		return nil
	}
}

// RequiredAbilities is every ability a key needs for the conversion routes.
// Reading and cancelling need no tool ability: they are about the caller's own
// work, not about running a conversion.
func RequiredAbilities() []string { return abilities.All() }
