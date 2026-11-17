// Package agent is the shared surface for the CLI, the local HTTP API and MCP.
//
// All three list the same registered conversion tools and crop a PDF by calling
// internal/pdf. Nothing here reimplements a conversion.
package agent

import (
	"slices"

	"github.com/AlexGalhardo/vukem-tools/app/catalog"
	"github.com/AlexGalhardo/vukem-tools/app/tools"
)

// Tool is one user-facing conversion an agent can call.
type Tool struct {
	Name    string `json:"name"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
}

// Tools lists every conversion tool this build registers.
//
// Image, video and download tools stay out when their engine was not resolved,
// which is the same rule GET /api/v1/tools uses. Browser-only catalogue entries
// are not conversion tools and are not listed.
func Tools() []Tool {
	registered := tools.All(nil, tools.Engines{}, nil)
	out := make([]Tool, 0, len(registered))

	for _, item := range registered {
		name := item.Name()
		entry, ok := catalog.Find(name)
		tool := Tool{Name: name}

		if ok {
			tool.Title = entry.Title
			tool.Summary = entry.Summary
		}

		out = append(out, tool)
	}

	slices.SortFunc(out, func(a, b Tool) int {
		if a.Name < b.Name {
			return -1
		}

		if a.Name > b.Name {
			return 1
		}

		return 0
	})

	return out
}
