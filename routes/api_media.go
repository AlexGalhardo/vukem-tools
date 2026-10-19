package routes

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
	"github.com/AlexGalhardo/vukem-tools/app/tools"
	"github.com/AlexGalhardo/vukem-tools/internal/httpx"
)

// tagMedia is the OpenAPI tag for the image, video and audio operations.
const tagMedia = "Media"

// Media registers the image, video and audio routes.
//
// They are a separate file from Conversions, not because the shape differs
// but because this domain is built in parallel with three others: a single
// route file would make every tool a merge conflict with every other tool
// being added at the same time.
//
// The capability routes are the reason this file reaches for the engines at
// all. Format support has to be surfaced by the API rather than hard-coded in
// the frontend, which means something must probe what the engines can do, and
// the right place to do it once is here, at boot.
func Media(c *services.Conversions) func(r *router.Router, app *vukem.App) error {
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

		media, err := resolveMediaEngines(app, c)
		if err != nil {
			return err
		}

		h := apiv1.NewMedia(
			apiv1.NewConversions(c.Pipeline(), c.Store(), c.Files(), c.Quota(), c.Hub()),
			media,
		)

		perIP := ratelimit.Middleware(limiters("api.ip", 120, time.Minute), ratelimit.ByIP)
		perToken := ratelimit.Middleware(limiters("api.token", 60, time.Minute), ratelimit.ByToken)
		// Starting work is limited harder than reading about it: a
		// conversion costs CPU, and the quota is the budget rather than the
		// brake.
		submit := ratelimit.Middleware(limiters("api.submit", 30, time.Minute), ratelimit.ByToken)

		r.Group("/api/v1", func(g *router.Group) {
			g.Use(middleware.JSONOnly(), httpx.AcceptAPIKeyHeader, perIP)

			// The capability routes are public, like the catalogue: a client
			// should be able to learn what this server can convert before it
			// has a key, and nothing here reveals anything about an account.
			api.Register(g, "GET /tools/image-convert/formats", api.Op{
				Name:    "tools.image-convert.formats",
				Summary: "List the image formats this server can read and write",
				Description: "Probed from the engine at boot rather than declared. A format whose " +
					"encoder is not in this build is reported as unwritable, so a client never " +
					"offers a conversion that would be refused.",
				Tags: []string{tagMedia},
			}, h.ImageCapabilities)

			api.Register(g, "GET /tools/video-convert/capabilities", api.Op{
				Name:    "tools.video-convert.capabilities",
				Summary: "List the containers and codecs this server can produce",
				Description: "H.264 and H.265 come from x264 and x265, which are GPL-only, so an " +
					"ffmpeg built without GPL support has neither. gpl_build says which build " +
					"this is, and codecs lists what can actually be encoded.",
				Tags: []string{tagMedia},
			}, h.VideoCapabilities)

			api.Register(g, "GET /tools/audio-convert/formats", api.Op{
				Name:        "tools.audio-convert.formats",
				Summary:     "List the audio formats this server can write",
				Description: "With whether each one takes a bit rate: FLAC and WAV are lossless and refuse one.",
				Tags:        []string{tagMedia},
			}, h.AudioCapabilities)

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

				// One route per tool, so the OpenAPI document describes each
				// tool's options exactly instead of saying "an object".
				api.Register(p, "POST /tools/image-convert", api.Op{
					Name:    "tools.image-convert",
					Summary: "Convert, resize, crop, rotate or flip an image",
					Description: "One libvips pass does all of it: a resize is a re-encode with a " +
						"target size, and the orientation and colour handling happen on the same " +
						"pass. EXIF is removed unless you ask for it, because a photograph's EXIF " +
						"often carries GPS coordinates and a device serial number.",
					Tags:     []string{tagMedia},
					Security: []string{SchemeBearer},
				}, h.ImageConvert).Use(submit)

				api.Register(p, "POST /tools/video-convert", api.Op{
					Name:    "tools.video-convert",
					Summary: "Convert a video between containers and codecs",
					Description: "Progress is reported on the conversion's event stream. A codec " +
						"the container cannot carry usefully, or that this build cannot encode, " +
						"is refused here rather than by the engine a minute later.",
					Tags:     []string{tagMedia},
					Security: []string{SchemeBearer},
				}, h.VideoConvert).Use(submit)

				api.Register(p, "POST /tools/video-to-gif", api.Op{
					Name:    "tools.video-to-gif",
					Summary: "Turn part of a video into an animated GIF",
					Description: "Two passes: a palette is computed from the frames in the clip and " +
						"the frames are then mapped onto it, which is the difference between a GIF " +
						"that looks like the video and one that bands. Capped at 15 seconds, " +
						"because a GIF's size grows linearly with its duration.",
					Tags:     []string{tagMedia},
					Security: []string{SchemeBearer},
				}, h.VideoToGIF).Use(submit)

				api.Register(p, "POST /tools/audio-convert", api.Op{
					Name:    "tools.audio-convert",
					Summary: "Convert audio, or extract a video's soundtrack",
					Description: "The same operation either way: a video input has its picture " +
						"dropped and its audio encoded. Loudness normalisation aims at -16 LUFS, " +
						"the figure streaming platforms use, and the two-pass form measures the " +
						"whole recording first so it actually hits the target.",
					Tags:     []string{tagMedia},
					Security: []string{SchemeBearer},
				}, h.AudioConvert).Use(submit)
			})
		}).Name("api.v1.")

		return nil
	}
}

// resolveMediaEngines probes what the media engines can do, once, at boot.
//
// It needs somewhere to write, because probing an image format means trying
// to encode four pixels: an operation being compiled in and a format being
// writable are different facts, and only an encode distinguishes them.
// ENGINE_WORKDIR is where conversions already write, so the probe uses a
// directory under it rather than a second configured path.
//
// This function is temporary and should be deleted. The media tools need the
// same probe, and once services.Conversions builds one in its own build() —
// the change that registers the tools at all — it should expose it and this
// whole function becomes `media := c.Media()`. Until then the probe runs
// twice per process: once here and once there. It is idempotent and costs
// about nine small processes, so running it twice is wasteful rather than
// wrong, but it is still two sources of one answer.
//
// Agreed with the owner; the accessor is theirs to add, in a file this
// domain does not own.
//
// A probe failure is a boot error on purpose. An engine that is present and
// cannot answer is a broken image, and a broken image should fail the deploy
// rather than the first conversion that needs it — which is the rule the rest
// of the engine resolution already follows.
func resolveMediaEngines(app *vukem.App, c *services.Conversions) (*tools.Media, error) {
	dir, err := mediaProbeDir(app)
	if err != nil {
		return nil, err
	}

	// Removed once the probe has answered. It holds four pixels and a handful
	// of trial encodes, none of which is worth anything after boot, and
	// leaving it behind is what made two processes sharing a working root
	// overwrite each other's probe image.
	defer func() {
		if err := os.RemoveAll(dir); err != nil {
			app.Log.Warn("routes: engine probe directory not removed", "dir", dir, "err", err)
		}
	}()

	// The probe is bounded. An engine that cannot encode four pixels
	// promptly is an engine that will not convert anything, and a deploy
	// should say so rather than hang.
	ctx, cancel := context.WithTimeout(context.Background(), mediaProbeTimeout)
	defer cancel()

	media, err := tools.ResolveMedia(ctx, c.Engines(), c.Runner(), dir)
	if err != nil {
		return nil, fmt.Errorf("routes: probe the media engines: %w", err)
	}

	app.Log.Info("media engines probed",
		"image_formats_writable", len(media.WritableImageFormats()),
		"video_codecs", len(media.WritableVideoCodecs()),
		"audio_formats", len(media.WritableAudioFormats()),
		"ffmpeg_gpl", media.FFmpeg.GPL)

	return media, nil
}

// mediaProbeTimeout bounds the whole capability probe. It is generous because
// it covers one trial encode per image format on a cold container.
const mediaProbeTimeout = 60 * time.Second

// mediaProbeDir returns a scratch directory for the capability probe, and
// creates it.
//
// Under the engine working directory rather than the system temporary one:
// ENGINE_WORKDIR is on the image's own filesystem, while /tmp is a tmpfs on
// several platforms, and the Dockerfile sets the former for exactly that
// reason.
func mediaProbeDir(app *vukem.App) (string, error) {
	root := app.Source.Get("ENGINE_WORKDIR")
	if root == "" {
		root = app.Source.Get("WORK_DIR")
	}

	if root == "" {
		root = defaultMediaProbeRoot
	}

	// Absolute, because the probe passes this directory to the engine both as
	// the command's working directory and inside the file arguments. A
	// relative root would be resolved twice — once by this process and again
	// by the engine against its own working directory — and the engine would
	// refuse a path that does not exist. ENGINE_WORKDIR is absolute in the
	// image, so only an unset or relative setting hit this, which is every
	// local run and the whole of ./tests/.
	base, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("routes: resolve the engine working root %s: %w", root, err)
	}

	// 0o700 because the engines run as this user and nothing else has any
	// business reading what they write.
	if err := os.MkdirAll(base, 0o700); err != nil {
		return "", fmt.Errorf("routes: prepare the engine working root %s: %w", base, err)
	}

	// A fresh directory per boot rather than a fixed "capability-probe",
	// because the probe writes one file under a fixed name and then reads it
	// back for every trial encode. Two apps sharing a working root — which is
	// every parallel test in ./tests/ that does not sandbox, and any two
	// processes on one volume — would overwrite each other's probe image
	// halfway through and the engine would report a truncated file. MkdirTemp
	// also creates it, with 0o700.
	dir, err := os.MkdirTemp(base, "capability-probe-")
	if err != nil {
		return "", fmt.Errorf("routes: prepare the engine probe directory under %s: %w", base, err)
	}

	return dir, nil
}

// defaultMediaProbeRoot matches the default working root the conversion
// service uses when nothing is configured.
const defaultMediaProbeRoot = "storage/work"
