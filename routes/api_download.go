package routes

import (
	"net/http"
	"time"

	vukem "github.com/AlexGalhardo/vukem"
	"github.com/AlexGalhardo/vukem/api"
	"github.com/AlexGalhardo/vukem/auth"
	"github.com/AlexGalhardo/vukem/middleware"
	"github.com/AlexGalhardo/vukem/openapi"
	"github.com/AlexGalhardo/vukem/ratelimit"
	"github.com/AlexGalhardo/vukem/router"
	"github.com/AlexGalhardo/vukem/web"

	"github.com/AlexGalhardo/vukem-tools/app/accounts"
	"github.com/AlexGalhardo/vukem-tools/app/controllers"
	"github.com/AlexGalhardo/vukem-tools/app/controllers/apiv1"
	"github.com/AlexGalhardo/vukem-tools/app/services"
	"github.com/AlexGalhardo/vukem-tools/app/tools"
	"github.com/AlexGalhardo/vukem-tools/internal/httpx"
)

// Downloads registers the download and edit routes (S3).
//
// They are a route file of their own rather than more lines in Conversions
// for the reason the per-domain seams exist at all: three domains are being
// built at once, and a single file holding every tool's route makes each new
// tool a conflict with every other. The group, the middleware and the
// security scheme are the same as the conversion routes', so a client sees
// one coherent API whatever file a route is declared in.
func Downloads(c *services.Conversions) func(r *router.Router, app *vukem.App) error {
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
		capabilities := apiv1.NewCapabilities(c.Engines(), c.Pipeline())
		// The cut's output limit comes off the registered tool, so the page
		// and the engine cannot disagree about it.
		var clipLimit time.Duration

		if registered, ok := c.Pipeline().Tool("video-clip"); ok {
			if clip, ok := registered.(*tools.VideoClip); ok {
				clipLimit = clip.MaxOutputDuration()
			}
		}

		pages := controllers.NewDownloadPages(
			c.Pipeline().Tools(),
			services.MaxUploadBytes(app),
			TusPath,
			clipLimit,
		)

		perIP := ratelimit.Middleware(limiters("api.ip", 120, time.Minute), ratelimit.ByIP)
		perToken := ratelimit.Middleware(limiters("api.token", 60, time.Minute), ratelimit.ByToken)
		submit := ratelimit.Middleware(limiters("api.submit", 30, time.Minute), ratelimit.ByToken)
		// A metadata probe is a real request to somebody else's server, so it
		// is limited like a submission rather than like a read. Its own
		// bucket, not the submit bucket: a client checking a few URLs before
		// picking one must not spend its budget for actually downloading
		// them.
		probe := ratelimit.Middleware(limiters("api.probe", 30, time.Minute), ratelimit.ByToken)

		r.Group("/api/v1", func(g *router.Group) {
			// httpx.AcceptAPIKeyHeader runs before the guard, exactly as in
			// the other API groups: it rewrites X-API-Key into Authorization,
			// which is the only place vukem's token guard looks.
			g.Use(middleware.JSONOnly(), httpx.AcceptAPIKeyHeader, perIP)

			// Public, like the rest of the catalogue: a client should be able
			// to learn which URLs are accepted and what this server can do
			// before it has a key, and neither reveals anything about an
			// account.
			api.Register(g, "GET /download-platforms", api.Op{
				Name:    "downloads.platforms",
				Summary: "List the platforms a video can be downloaded from",
				Description: "The closed list. A URL outside it is refused before yt-dlp is started, " +
					"so this service cannot be used to fetch an arbitrary address.",
				Tags: []string{tagCatalog},
			}, h.DownloadPlatforms)

			api.Register(g, "GET /clip-modes", api.Op{
				Name:        "downloads.clip-modes",
				Summary:     "List the cut modes",
				Description: "trim keeps one range; split cuts the whole video into consecutive clips.",
				Tags:        []string{tagCatalog},
			}, h.ClipModes)

			g.Group("", func(p *router.Group) {
				// Session first, key second, and the order is load-bearing: Required stops
				// at the first guard whose failure is not "unauthenticated", so
				// putting the token guard first made a cookie-only request 401
				// before the session guard ever ran. routes/uploads.go already had
				// the working order, which is why uploading a file worked from the
				// browser while every tool submit that followed answered 401.
				//
				// requireAbility expects both: a session has no abilities and is
				// allowed everything its owner is, a key only what it was minted
				// with.
				p.Use(auth.Required(acc.Web, acc.API), perToken)

				// Behind the guard, unlike the rest of the catalogue, because
				// it reports each engine's exact version. Which tools exist is
				// public — the tool index says so — but which ffmpeg and which
				// Ghostscript build is running is the CVE surface of this
				// deployment, and that is not something to hand to an
				// unauthenticated caller. An operator reads it from
				// /health/deep instead.
				api.Register(p, "GET /capabilities", api.Op{
					Name:    "capabilities",
					Summary: "Report which engines this server has and what its limits are",
					Description: "Every external program, whether it was resolved at startup and the " +
						"version it reported, plus the registered tools and the numbers this server " +
						"enforces. An engine the image does not carry hides its tools, and this is " +
						"where that is visible.",
					Tags:     []string{tagCatalog},
					Security: []string{SchemeBearer},
				}, capabilities.Show)

				api.Register(p, "POST /tools/video-download", api.Op{
					Name:    "tools.video-download",
					Summary: "Download a video from a URL",
					Description: "Fetches a video from YouTube, Instagram, X or TikTok. This is the " +
						"one conversion with no upload: the input is the URL, so bytes_in is 0 " +
						"until the download has happened. Ask for audio to get MP3, for clips to " +
						"get consecutive segments delivered individually and as a zip, and for " +
						"compression to have the result re-encoded and whichever file is smaller " +
						"delivered. Downloading may breach a platform's terms of service and you " +
						"are responsible for what you download.",
					Tags:     []string{tagConversions},
					Security: []string{SchemeBearer},
				}, h.VideoDownload).Use(submit)

				api.Register(p, "POST /tools/video-download/metadata", api.Op{
					Name:    "tools.video-download.metadata",
					Summary: "Report what a platform says about a URL",
					Description: "The title, the uploader, the duration, the thumbnail, the available " +
						"formats and the expected size, without downloading anything. The size cap " +
						"is checked against estimated_bytes, so too_large tells you the download " +
						"will be refused before you submit it. A POST rather than a GET because a " +
						"URL in a query string ends up in every log between here and the caller.",
					// 200 rather than the 201 a POST defaults to: a probe
					// creates nothing, and the method is a POST only to keep
					// the URL out of the logs.
					Status:   http.StatusOK,
					Tags:     []string{tagConversions},
					Security: []string{SchemeBearer},
				}, h.DownloadMetadata).Use(probe)

				api.Register(p, "POST /tools/video-clip", api.Op{
					Name:    "tools.video-clip",
					Summary: "Cut, crop or split a video",
					Description: "Trims an uploaded video to a range, crops the frame, or splits it " +
						"into consecutive clips of up to a minute. A cut that lands on a keyframe " +
						"is copied rather than re-encoded, which is an order of magnitude cheaper; " +
						"frame_accurate forces the re-encode. A crop is validated against the real " +
						"stream dimensions, and a crop outside them is refused with those " +
						"dimensions named.",
					Tags:     []string{tagConversions},
					Security: []string{SchemeBearer},
				}, h.VideoClip).Use(submit)

				api.Register(p, "POST /tools/video-compress", api.Op{
					Name:    "tools.video-compress",
					Summary: "Make a video or an audio file smaller",
					Description: "Re-encodes an uploaded file and returns whichever is smaller, the " +
						"result or the original. When the original wins, the conversion still " +
						"finishes: bytes_out equals bytes_in and the result is the file you sent, " +
						"which is the honest answer for something that is already optimal.",
					Tags:     []string{tagConversions},
					Security: []string{SchemeBearer},
				}, h.VideoCompress).Use(submit)
			})
		}).Name("api.v1.")

		// The tool's own page, at its exact path.
		//
		// net/http.ServeMux prefers the more specific pattern, so this wins
		// over the "/tools/{name}" route that renders one component for every
		// tool — which means this domain gets its own page without a switch
		// over tool names in a file every domain shares. The Inertia
		// middleware and the shared props come from routes/web.go, which is
		// registered first.
		// Named page.* and hidden from the document: an Inertia page is not
		// an API operation, it has no response schema, and sharing the
		// "tools.video-download" name with the submit route makes
		// openapi.Build refuse the whole document for a duplicate
		// operationId. The same reason routes/conversions.go hides the event
		// stream.
		for _, page := range []struct {
			path    string
			name    string
			handler func(*web.Ctx) error
		}{
			{"/tools/video-download", "page.video-download", pages.VideoDownload},
			{"/tools/video-clip", "page.video-clip", pages.VideoClip},
			{"/tools/video-compress", "page.video-compress", pages.VideoCompress},
		} {
			r.Get(page.path, page.handler).
				Name(page.name).
				Use(auth.Required(acc.Web)).
				Doc(openapi.Skip{})
		}

		return nil
	}
}
