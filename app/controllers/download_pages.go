package controllers

import (
	"slices"

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

// DownloadPages renders the pages of the download domain.
type DownloadPages struct {
	// live names the tools the pipeline actually has registered, so a page
	// cannot be served for a tool whose engine the image does not carry.
	live []string
}

// NewDownloadPages returns the page handlers.
func NewDownloadPages(live []string) *DownloadPages {
	return &DownloadPages{live: live}
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

// entry returns the catalogue entry for a tool that is actually registered.
func (p *DownloadPages) entry(name string) (catalog.Tool, bool) {
	found, ok := catalog.Find(name)
	if !ok || !slices.Contains(p.live, name) {
		return catalog.Tool{}, false
	}

	return found, true
}
