package routes

import (
	"context"
	"time"

	vukem "github.com/AlexGalhardo/vukem"
	"github.com/AlexGalhardo/vukem/api"
	"github.com/AlexGalhardo/vukem/auth"
	"github.com/AlexGalhardo/vukem/openapi"
	"github.com/AlexGalhardo/vukem/ratelimit"
	"github.com/AlexGalhardo/vukem/router"
	"github.com/AlexGalhardo/vukem/upload"

	"github.com/AlexGalhardo/vukem-tools/app/accounts"
	"github.com/AlexGalhardo/vukem-tools/app/controllers"
	"github.com/AlexGalhardo/vukem-tools/app/services"
	"github.com/AlexGalhardo/vukem-tools/internal/detect"
	"github.com/AlexGalhardo/vukem-tools/internal/files"
	"github.com/AlexGalhardo/vukem-tools/internal/httpx"
	"github.com/AlexGalhardo/vukem-tools/internal/ingest"
)

// TusPath is where resumable uploads are mounted. It is a subtree, so an
// upload URL is TusPath plus the upload id.
const TusPath = "/uploads/tus/"

// Uploads registers the resumable upload endpoint and the signed download
// route.
//
// Both sit outside /api/v1 on purpose. A resumable upload speaks tus, not
// JSON, so the API's JSONOnly middleware would refuse every request; and a
// download is a file, not a document, so it must not be behind a guard that
// answers with a problem document.
func Uploads(c *services.Conversions) func(r *router.Router, app *vukem.App) error {
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

		ing, err := newIngester(app, c)
		if err != nil {
			return err
		}

		// Either guard may own an upload: the interface authenticates with a
		// session, a script with an API key, and both reach the same endpoint.
		h := controllers.NewUploads(ing, c.Files(), app.Log, "web", "api")

		tus, err := upload.Tus(h.TusConfig(
			services.UploadRoot(app), TusPath, services.MaxUploadBytes(app), services.UploadExpiry(),
		))
		if err != nil {
			return err
		}

		app.OnShutdown(func(context.Context) error { return tus.Close() })
		c.SetUploadCleanup(tus.Cleanup)

		// Creating an upload is limited per user, not per IP: several people
		// behind one address must not share a budget, and one account must
		// not be able to fill the disk with abandoned uploads.
		starts := ratelimit.Middleware(limiters("uploads.create", 30, time.Minute), ratelimit.ByToken)

		r.Handle(TusPath, tus).
			Name("uploads.tus").
			Use(httpx.AcceptAPIKeyHeader, auth.Required(acc.Web, acc.API), starts).
			Doc(openapi.Skip{})

		// The signature is the authority here, so there is no auth guard: a
		// download link has to work in another tab, in curl, and in a
		// download manager that carries no cookie.
		r.Get(files.DownloadPath+"{id}", h.Download).
			Name("files.download").
			Use(c.Signer().Middleware()).
			Doc(openapi.Skip{})

		r.Group("/api/v1", func(g *router.Group) {
			api.Register(g, "GET /upload-types", api.Op{
				Name:        "uploads.types",
				Summary:     "List the file types an upload may have",
				Description: "Every type the service accepts at the door, so a client does not hardcode them.",
				Tags:        []string{tagCatalog},
			}, h.Types)
		}).Name("api.v1.")

		return nil
	}
}

// newIngester builds the upload acceptance path: what types come in, how
// large, and what scans them.
func newIngester(app *vukem.App, c *services.Conversions) (*ingest.Ingester, error) {
	var scanner upload.Scanner
	if addr := app.Source.Get("CLAMAV_ADDR"); addr != "" {
		scanner = upload.ClamAV(addr)
	}

	return ingest.New(ingest.Options{
		Files:   c.Files(),
		Scanner: scanner,
		// Every type the service recognises is accepted at the door. The tool
		// narrows it again when it runs, because the file and the tool are
		// chosen in two separate requests: refusing here what some tool could
		// have used would mean re-uploading to change your mind.
		Accepted: detect.Types(),
		MaxBytes: services.MaxUploadBytes(app),
		Log:      app.Log,
	})
}
