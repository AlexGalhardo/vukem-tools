package routes

import (
	"time"

	vukem "github.com/AlexGalhardo/vukem"
	"github.com/AlexGalhardo/vukem/api"
	"github.com/AlexGalhardo/vukem/auth"
	"github.com/AlexGalhardo/vukem/middleware"
	"github.com/AlexGalhardo/vukem/ratelimit"
	"github.com/AlexGalhardo/vukem/router"

	"github.com/AlexGalhardo/vukem-tools/app/accounts"
	"github.com/AlexGalhardo/vukem-tools/app/controllers/apiv1"
	"github.com/AlexGalhardo/vukem-tools/app/services"
	"github.com/AlexGalhardo/vukem-tools/internal/httpx"
)

// tagArchives groups the archive operations in the OpenAPI document.
const tagArchives = "Archives"

// Archives registers the archive submit routes.
//
// Its own route file for the same reason the PDF domain has one: one file of
// submit routes would make every tool a conflict with every other tool being
// added at the same time. It takes the same resolved pipeline — Resolve is
// idempotent, and the event hub must exist exactly once per process.
func Archives(c *services.Conversions) func(r *router.Router, app *vukem.App) error {
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

		h := apiv1.NewConversions(c.Pipeline(), c.Store(), c.Files(), c.Quota(), c.Hub())

		perIP := ratelimit.Middleware(limiters("api.ip", 120, time.Minute), ratelimit.ByIP)
		perToken := ratelimit.Middleware(limiters("api.token", 60, time.Minute), ratelimit.ByToken)
		// The same submit limiter name as every other conversion route, so
		// the budget is "thirty conversions a minute" rather than thirty per
		// domain: a caller should not get more throughput by spreading work
		// across tools.
		submit := ratelimit.Middleware(limiters("api.submit", 30, time.Minute), ratelimit.ByToken)

		r.Group("/api/v1", func(g *router.Group) {
			g.Use(middleware.JSONOnly(), httpx.AcceptAPIKeyHeader, perIP)

			g.Group("", func(p *router.Group) {
				// Session first, key second. The order is load-bearing; see
				// routes/api_pdf.go for what goes wrong when it is not.
				p.Use(auth.Required(acc.Web, acc.API), perToken)

				api.Register(p, "POST /tools/archive-create",
					archiveOp("archive-create"), h.ArchiveCreate).Use(submit)
				api.Register(p, "POST /tools/archive-extract",
					archiveOp("archive-extract"), h.ArchiveExtract).Use(submit)
			})
		}).Name("api.v1.")

		return nil
	}
}

// archiveOp is the OpenAPI operation for an archive tool.
//
// It panics on an unknown path, which is right for a wiring mistake: the paths
// come from the registrations above, in the same file, and a typo should fail
// the boot rather than publish an operation with no summary.
func archiveOp(path string) api.Op {
	for _, route := range archiveOperations() {
		if route.path == path {
			return api.Op{
				Name:        "tools." + route.path,
				Summary:     route.summary,
				Description: route.description,
				Tags:        []string{tagArchives},
				Security:    []string{SchemeBearer},
			}
		}
	}

	panic("routes: no archive operation described for " + path)
}

// archiveRoute is one tool's documentation.
type archiveRoute struct {
	path        string
	summary     string
	description string
}

// archiveOperations is every archive tool's documentation.
//
// The descriptions are where the honest limits are written down, because this
// is what a client reads before deciding to use a tool. That gz holds one
// file, that extraction caps the entry count and the unpacked size, and that
// an entry's folders are flattened away are all things somebody would
// otherwise discover afterwards.
func archiveOperations() []archiveRoute {
	return []archiveRoute{
		{
			path:    "archive-create",
			summary: "Pack several files into one archive",
			description: "ZIP, TAR.GZ, TAR or GZ. The order of file_ids is the order of the " +
				"entries, and each entry keeps the name its upload had. gz is the exception: it " +
				"compresses a single stream, so submitting more than one file for it is a " +
				"validation error rather than an archive holding only the first. XZ and ZSTD are " +
				"not offered — neither is in the Go standard library, and the four formats here " +
				"cover what a web service is sent.",
		},
		{
			path:    "archive-extract",
			summary: "Unpack an archive into its files",
			description: "Each member becomes a result of its own with its own expiry, so all of " +
				"them have to be downloaded within the result lifetime. The format is read from " +
				"the archive's bytes rather than its name. Folders are flattened: only a " +
				"member's filename survives, and two members that then share a name are both " +
				"kept, numbered. An archive is refused if it holds more than 64 files or " +
				"unpacks to more than 512 MB, or if any member is a link or a device rather " +
				"than a file.",
		},
	}
}
