package controllers

import (
	"fmt"
	"maps"
	"slices"

	"github.com/AlexGalhardo/vukem/inertia"
	"github.com/AlexGalhardo/vukem/orm"
	"github.com/AlexGalhardo/vukem/web"

	"github.com/AlexGalhardo/vukem-tools/app/catalog"
	"github.com/AlexGalhardo/vukem-tools/app/licenses"
	"github.com/AlexGalhardo/vukem-tools/app/tools"
)

// Pages renders the public pages: the tool index and the licence notice.
type Pages struct {
	// live names the tools the pipeline actually has registered. The
	// catalogue only marks an entry as usable when it appears here, so the
	// site cannot advertise a tool that does not exist.
	live     []string
	maxBytes int64
	tusPath  string
}

// NewPages returns the public page handlers. maxBytes and tusPath are passed
// to the interface so the upload limit and the endpoint are the server's to
// state rather than the client's to hardcode.
func NewPages(live []string, maxBytes int64, tusPath string) *Pages {
	return &Pages{live: live, maxBytes: maxBytes, tusPath: tusPath}
}

// Index renders the tool index: GET /.
//
// The whole catalogue is sent, including the planned and skipped entries, so
// the search works offline from one payload and a visitor comparing with
// another service finds the answer here. It is a few kilobytes, and the
// alternative — a request per keystroke — would be slower and worse.
func (p *Pages) Index(c *web.Ctx) error {
	liveN, planned, skipped := catalog.Count(p.live)

	return inertia.Render(c, "Home", inertia.Props{
		"groups": catalog.Grouped(p.live),
		"counts": map[string]int{"live": liveN, "planned": planned, "skipped": skipped},
	})
}

// Tool renders one tool's page: GET /tools/{name}.
//
// Only a registered tool has a page: the catalogue may list a planned or
// skipped entry, but a page for one would be a form that cannot be submitted.
// An unregistered name is a 404, which is also what a stale bookmark gets
// after a tool is withdrawn.
func (p *Pages) Tool(c *web.Ctx) error {
	name := c.Request().PathValue("name")

	entry, ok := catalog.Find(name)
	if !ok || !slices.Contains(p.live, name) {
		return orm.ErrNotFound
	}

	page, ok := pages[name]
	if !ok {
		// Registered in the pipeline but with no page: a 500 rather than a
		// wrong page. Rendering some other tool's form would let somebody
		// submit the wrong options and read the failure as their file's
		// fault.
		return fmt.Errorf("controllers: tool %q is live but has no page", name)
	}

	props := inertia.Props{
		"tool":     entry,
		"maxBytes": p.maxBytes,
		"tusPath":  p.tusPath,
	}

	maps.Copy(props, page.props())

	return inertia.Render(c, page.component, props)
}

// toolPage is a tool's Inertia component and the options that component needs.
//
// A map rather than a switch, and the options behind a function rather than a
// value, so a domain adds its tools here without touching the handler and
// without building every tool's option list on every page render.
type toolPage struct {
	component string
	props     func() inertia.Props
}

// pages maps a tool name to its page.
//
// Keyed by the catalogue's name, which is also the pipeline's tool name and
// the URL segment, so the three cannot drift apart. A live tool missing from
// here is a server error, not a fallback: see Tool.
var pages = map[string]toolPage{ //nolint:gochecknoglobals // read-only table
	"pixels-to-svg": {
		component: "Tools/PixelsToSvg",
		props: func() inertia.Props {
			return inertia.Props{
				"modes":  tools.PixelSVGModes(),
				"alphas": tools.PixelSVGAlphas(),
			}
		},
	},

	// The S4 media tools.
	//
	// Only the enum lists are props. The formats, codecs and dithering modes
	// are fetched by the page from the capability endpoints instead, because
	// what this build can produce is probed at boot — Debian's libvips has
	// heifsave while libheif ships no encoder, so AVIF is readable and not
	// writable — and a server-rendered prop would be a second place for that
	// answer to live and drift.
	"image-convert": {
		component: "Tools/ImageConvert",
		props: func() inertia.Props {
			return inertia.Props{
				"resizeModes": tools.ImageResizeModes(),
				"flips":       tools.ImageFlips(),
				"rotations":   tools.ImageRotations(),
			}
		},
	},
	"video-convert": {
		component: "Tools/VideoConvert",
		// No props: the containers and codecs this build can encode come
		// from GET /api/v1/tools/video-convert/capabilities, which also
		// carries gpl_build so the page can explain why H.264 is missing
		// rather than only report that it is.
		props: func() inertia.Props { return inertia.Props{} },
	},
	"video-to-gif": {
		component: "Tools/VideoToGif",
		// No props: the dithering modes and the length limit come from the
		// same capability endpoint.
		props: func() inertia.Props { return inertia.Props{} },
	},
	"audio-convert": {
		component: "Tools/AudioConvert",
		// No props: the formats, sample rates and loudness target come from
		// GET /api/v1/tools/audio-convert/formats, which also says which
		// formats take a bit rate at all.
		props: func() inertia.Props { return inertia.Props{} },
	},
}

// Licenses renders the licence notice: GET /licenses.
//
// This page is a condition of shipping the engines, not a courtesy: some of
// them are AGPL-3.0 and GPL, which carry obligations on anyone who runs this.
// It says so in plain words rather than only listing SPDX identifiers.
func (*Pages) Licenses(c *web.Ctx) error {
	return inertia.Render(c, "Licenses", inertia.Props{
		"project":    licenses.Project(),
		"components": licenses.Components(),
		"copyleft":   licenses.Copyleft(),
	})
}
