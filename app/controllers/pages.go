package controllers

import (
	"github.com/AlexGalhardo/vukem/inertia"
	"github.com/AlexGalhardo/vukem/web"

	"github.com/AlexGalhardo/vukem-tools/app/catalog"
	"github.com/AlexGalhardo/vukem-tools/app/licenses"
)

// Pages renders the public pages: the tool index and the licence notice.
type Pages struct {
	// live names the tools the pipeline actually has registered. The
	// catalogue only marks an entry as usable when it appears here, so the
	// site cannot advertise a tool that does not exist.
	live []string
}

// NewPages returns the public page handlers.
func NewPages(live []string) *Pages { return &Pages{live: live} }

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
