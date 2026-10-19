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

// tagSprites groups the sprite sheet operations in the OpenAPI document.
const tagSprites = "Sprite sheets"

// Sprites registers the sprite sheet routes (S7).
func Sprites(c *services.Conversions) func(r *router.Router, app *vukem.App) error {
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
		submit := ratelimit.Middleware(limiters("api.submit", 30, time.Minute), ratelimit.ByToken)

		r.Group("/api/v1", func(g *router.Group) {
			g.Use(middleware.JSONOnly(), httpx.AcceptAPIKeyHeader, perIP)

			// The format list is public and needs no key: it names no
			// account and carries no secret, and the tool pages read it
			// before anybody has signed in.
			api.Register(g, "GET /tools/sprite-split/formats", api.Op{
				Name:    "tools.sprite-split.formats",
				Summary: "The metadata formats, the modes and the limits",
				Description: "What sprite-split and sprite-build can produce, with one sentence " +
					"per metadata format saying what reads it. Published so the interface does " +
					"not hold a list of its own, which is the only way the two can agree.",
				Tags: []string{tagSprites},
			}, h.SpriteCapabilities)

			g.Group("", func(p *router.Group) {
				// Session first, key second. The order is load-bearing; see
				// routes/api_pdf.go for what goes wrong when it is not.
				p.Use(auth.Required(acc.Web, acc.API), perToken)

				api.Register(p, "POST /tools/sprite-split", api.Op{
					Name:    "tools.sprite-split",
					Summary: "Cut a sprite sheet into frames",
					Description: "Grid mode cuts the sheet on a grid you describe and is exact; " +
						"auto mode finds each frame's bounding box from the sheet's own content, " +
						"which is the answer for a hand-drawn sheet. A fully opaque sheet with " +
						"no background colour named is refused rather than guessed at. The " +
						"result is one archive — the frames and the metadata together, so one " +
						"download is self-contained — plus each metadata file on its own. " +
						"Trimming is refused with the css format, which has nowhere to record " +
						"the offset.",
					Tags:     []string{tagSprites},
					Security: []string{SchemeBearer},
				}, h.SpriteSplit).Use(submit)

				api.Register(p, "POST /tools/sprite-build", api.Op{
					Name:    "tools.sprite-build",
					Summary: "Pack frames into a sprite sheet",
					Description: "Takes a ZIP of frames. The grid layout gives every frame an " +
						"identical cell and is the default, because it is what Godot 4 and " +
						"Unity import with no metadata file at all; compact packs frames of " +
						"differing sizes more tightly. Frames that do not fit are refused with " +
						"the count that fitted and the size that would be needed, never " +
						"silently dropped. The packer never rotates a frame.",
					Tags:     []string{tagSprites},
					Security: []string{SchemeBearer},
				}, h.SpriteBuild).Use(submit)
			})
		}).Name("api.v1.")

		return nil
	}
}
