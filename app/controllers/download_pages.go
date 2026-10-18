package controllers

import (
	"slices"
	"time"

	"github.com/AlexGalhardo/vukem/inertia"
	"github.com/AlexGalhardo/vukem/orm"
	"github.com/AlexGalhardo/vukem/web"

	"github.com/AlexGalhardo/vukem-tools/app/catalog"
	"github.com/AlexGalhardo/vukem-tools/app/tools"
	"github.com/AlexGalhardo/vukem-tools/internal/editor"
)

// The page handlers for the download and edit tools (S3).
//
// They are a file of their own rather than more cases in Pages.Tool, which
// renders one component for every tool name. Each one is registered at its
// exact path, and net/http.ServeMux prefers the more specific pattern, so
// "/tools/video-download" wins over "/tools/{name}" without that handler
// needing to know this domain exists. Three domains are being built at once,
// and a switch over tool names in a shared file would be a conflict per tool.

// DownloadPages renders the pages of the download and edit domains.
type DownloadPages struct {
	// live names the tools the pipeline actually has registered, so a page
	// cannot be served for a tool whose engine the image does not carry.
	live     []string
	maxBytes int64
	tusPath  string
	// clipLimit is the longest output a cut may produce, zero for no cap.
	clipLimit time.Duration
}

// NewDownloadPages returns the page handlers.
//
// maxBytes and tusPath are passed in rather than read here for the same
// reason Pages takes them: the upload limit and the endpoint are the server's
// to state and the interface's to display, not to hardcode.
func NewDownloadPages(live []string, maxBytes int64, tusPath string, clipLimit time.Duration) *DownloadPages {
	return &DownloadPages{live: live, maxBytes: maxBytes, tusPath: tusPath, clipLimit: clipLimit}
}

// VideoDownload renders the download page: GET /tools/video-download.
//
// A tool that is not registered is a 404, not an empty form: without yt-dlp
// the submit route answers 503, and showing a page whose every button fails
// would be worse than saying the tool is not here.
func (p *DownloadPages) VideoDownload(c *web.Ctx) error {
	entry, ok := p.entry("video-download")
	if !ok {
		return orm.ErrNotFound
	}

	return inertia.Render(c, "Tools/VideoDownload", inertia.Props{
		"tool": entry,
		// The platform list is the server's to state: the interface must not
		// repeat a list it can get wrong, and the same list is published on
		// GET /api/v1/download-platforms for any other client.
		"platforms":      tools.DownloadPlatforms(),
		"maxClipSeconds": int(editor.MaxClip.Seconds()),
	})
}

// VideoClip renders the cut and crop page: GET /tools/video-clip.
func (p *DownloadPages) VideoClip(c *web.Ctx) error {
	return p.edit(c, "video-clip", "clip")
}

// VideoCompress renders the compression page: GET /tools/video-compress.
func (p *DownloadPages) VideoCompress(c *web.Ctx) error {
	return p.edit(c, "video-compress", "compress")
}

// edit renders the page both edit tools share.
//
// One component serves them because they differ only in the options panel and
// the submit call; two would drift apart, and the upload, the progress stream
// and the result links are the same code either way.
func (p *DownloadPages) edit(c *web.Ctx, name, mode string) error {
	entry, ok := p.entry(name)
	if !ok {
		return orm.ErrNotFound
	}

	props := inertia.Props{
		"tool":           entry,
		"mode":           mode,
		"maxBytes":       p.maxBytes,
		"tusPath":        p.tusPath,
		"clipModes":      tools.ClipModes(),
		"maxClipSeconds": int(editor.MaxClip.Seconds()),
		// 0 when there is no cap, which the page shows as "to the end" with
		// no upper bound rather than inventing one.
		"maxOutputSeconds": p.maxOutputSeconds(),
	}

	return inertia.Render(c, "Tools/VideoEdit", props)
}

// maxOutputSeconds is the longest output a cut may produce, read off the
// registered tool rather than from a setting this package would have to parse
// a second time.
func (p *DownloadPages) maxOutputSeconds() int {
	if p.clipLimit <= 0 {
		return 0
	}

	return int(p.clipLimit.Seconds())
}

// entry returns the catalogue entry for a tool that is actually registered.
func (p *DownloadPages) entry(name string) (catalog.Tool, bool) {
	found, ok := catalog.Find(name)
	if !ok || !slices.Contains(p.live, name) {
		return catalog.Tool{}, false
	}

	return found, true
}
