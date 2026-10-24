// Package catalog is what the site says it can do.
//
// It lists every tool, including the ones that are not built yet and the ones
// that will never be built, each with its reason. That is deliberate: a
// visitor comparing this with CloudConvert or iLovePDF needs to find the
// answer here rather than discover a gap by searching for something and
// getting nothing. A tool that is missing on purpose is information; a tool
// that is silently absent is a dead end.
//
// The entries for tools that run live in one place with the tools themselves:
// Available is derived from the pipeline's registry rather than written twice,
// so a tool cannot be advertised before it is registered.
package catalog

import (
	"slices"
	"strings"
)

// Status is how far along a tool is.
type Status string

// The statuses a tool can have.
const (
	// Live means it is registered and can be used now.
	Live Status = "live"
	// Planned means it is in the roadmap and not built yet.
	Planned Status = "planned"
	// Skipped means it will not be built, and Reason says why.
	Skipped Status = "skipped"
)

// Tool is one entry in the catalogue.
type Tool struct {
	// Name is the stable id, and the tool's URL segment. It matches the
	// pipeline's tool name for a live tool.
	Name string `json:"name"`
	// Title is what a person calls it.
	Title string `json:"title"`
	// Summary is one sentence saying what it does, written for somebody who
	// has the file in hand and is deciding whether this is the right tool.
	Summary string `json:"summary"`
	// In and Out are the formats, for the index: this is what somebody
	// scanning twenty tools is actually looking for.
	In  []string `json:"in"`
	Out []string `json:"out"`
	// Group is the heading it sits under.
	Group string `json:"group"`
	// Icon names an icon in resources/js/components/Icon.tsx.
	Icon string `json:"icon"`
	// Status and Reason. Reason is required for Skipped and is shown to the
	// visitor, because "not available" without a reason reads as neglect.
	Status Status `json:"status"`
	Reason string `json:"reason,omitempty"`
	// Keywords are extra search terms a person might type instead of the
	// title: the format they have, or the word another service uses.
	Keywords []string `json:"keywords,omitempty"`
	// Client marks a tool that runs in the visitor's browser (or needs only a
	// small server lookup) and so is never registered in the conversion
	// pipeline. It is live whenever its status says so.
	Client bool `json:"client,omitempty"`
}

// Groups, in the order the index shows them.
const (
	GroupImages    = "Images"
	GroupVideo     = "Video and audio"
	GroupDocuments = "PDF tools"
	GroupArchives  = "Archives and data"
	GroupDeveloper = "Developer tools"
	GroupBrazil    = "Brazil"
	GroupLLM       = "LLM and agents"
)

// tools is the catalogue. Order within a group is the order shown.
//
// This is the list the ROADMAP decided, with the S6 skips carrying the reason
// the owner recorded. Keeping the reasons here rather than only in the ROADMAP
// is what lets the site answer the comparison question itself.
// tools is the catalogue.
//
// Built from one slice per domain so domains can be added in parallel without
// every entry conflicting with every other. Display order is Groups, not the
// order here, so a domain's position in this list does not matter.
var tools = slices.Concat(
	pixelCatalog,
	imageCatalog,
	videoCatalog,
	audioCatalog,
	downloadCatalog,
	editCatalog,
	pdfCatalog,
	spriteCatalog,
	archiveCatalog,
	devCatalog,
)

// pixelCatalog is the pixel-art domain (S2).
var pixelCatalog = []Tool{
	{
		Name:     "pixels-to-svg",
		Title:    "Pixel art to SVG",
		Summary:  "Traces pixel art into clean vector shapes, one path per colour region, with no blurring or smoothing.",
		In:       []string{"PNG", "GIF", "BMP", "JPEG"},
		Out:      []string{"SVG"},
		Group:    GroupImages,
		Icon:     "vector",
		Status:   Live,
		Keywords: []string{"sprite", "vector", "trace", "pixelart", "game", "upscale"},
	},
}

// groupOrder is the order the index shows the groups in.
var groupOrder = []string{
	GroupImages, GroupVideo, GroupDocuments, GroupArchives, GroupDeveloper, GroupBrazil, GroupLLM,
}

// Group is a heading with its tools.
type Group struct {
	Name  string `json:"name"`
	Tools []Tool `json:"tools"`
}

// Grouped returns the catalogue in display order, with Live set for the tools
// in live.
//
// Taking the live set as an argument rather than reading a registry keeps this
// package free of the pipeline, and makes the one thing that could drift —
// advertising a tool that is not registered — impossible: a tool is Live here
// only because it was named there.
func Grouped(live []string) []Group {
	out := make([]Group, 0, len(groupOrder))

	for _, name := range groupOrder {
		group := Group{Name: name}

		for i := range tools {
			t := tools[i]
			if t.Group != name {
				continue
			}

			if t.Status == Live && !t.Client && !slices.Contains(live, t.Name) {
				// Registered nowhere, so it is not usable, whatever this file
				// claims. Planned is the honest status.
				t.Status = Planned
			}

			group.Tools = append(group.Tools, t)
		}

		if len(group.Tools) > 0 {
			out = append(out, group)
		}
	}

	return out
}

// All returns every entry, flat, in display order.
func All(live []string) []Tool {
	var out []Tool
	for _, g := range Grouped(live) {
		out = append(out, g.Tools...)
	}

	return out
}

// Find returns the entry with this name.
func Find(name string) (Tool, bool) {
	for i := range tools {
		if tools[i].Name == name {
			return tools[i], true
		}
	}

	return Tool{}, false
}

// SearchText is everything a search should match for a tool, lowercased and
// joined. It lives here so the client and any server-side search cannot
// disagree about what is searchable.
func (t Tool) SearchText() string {
	parts := []string{t.Name, t.Title, t.Summary, t.Group}
	parts = append(parts, t.In...)
	parts = append(parts, t.Out...)
	parts = append(parts, t.Keywords...)

	return strings.ToLower(strings.Join(parts, " "))
}

// Count returns how many entries are live, planned and skipped, for the
// footer: the honest headline number is how many work, not how many are
// listed.
func Count(live []string) (liveN, planned, skipped int) {
	all := All(live)

	for i := range all {
		switch all[i].Status {
		case Live:
			liveN++
		case Planned:
			planned++
		case Skipped:
			skipped++
		}
	}

	return liveN, planned, skipped
}

// NavTool is a usable tool as the sidebar shows it.
type NavTool struct {
	Name  string `json:"name"`
	Title string `json:"title"`
	Icon  string `json:"icon"`
	// Search is what the palette matches besides the title: formats and
	// keywords, without the summary, because this ships on every page.
	Search string `json:"search"`
}

// NavGroup is one dropdown of the sidebar.
type NavGroup struct {
	Name  string    `json:"name"`
	Tools []NavTool `json:"tools"`
}

// NavGroups returns the live tools by group, in display order, for the
// navigation that is on every page. Planned entries are left out: a link in
// a menu that leads nowhere is worse than no link, and the index page is where
// the full list lives.
func NavGroups(live []string) []NavGroup {
	var out []NavGroup

	for _, g := range Grouped(live) {
		nav := NavGroup{Name: g.Name}

		for i := range g.Tools {
			if t := g.Tools[i]; t.Status == Live {
				nav.Tools = append(
					nav.Tools,
					NavTool{Name: t.Name, Title: t.Title, Icon: t.Icon, Search: t.navSearch()},
				)
			}
		}

		if len(nav.Tools) > 0 {
			out = append(out, nav)
		}
	}

	return out
}

func (t Tool) navSearch() string {
	parts := []string{t.Name, t.Group}
	parts = append(parts, t.In...)
	parts = append(parts, t.Out...)
	parts = append(parts, t.Keywords...)

	return strings.ToLower(strings.Join(parts, " "))
}
