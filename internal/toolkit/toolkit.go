// Package toolkit is the one table of tools behind the CLI and the MCP
// server, and the client that runs them.
//
// The table (tools.json) is generated from the catalogue and the OpenAPI
// document by the test in generate_test.go, so a tool cannot be offered
// without an API operation and an API operation cannot be missed. Nothing here
// converts a file: every call goes to the HTTP API.
package toolkit

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
)

// Kind says where a tool runs.
type Kind string

// The two kinds of tool.
const (
	// Pipeline tools run on the server and are called through the API.
	Pipeline Kind = "pipeline"
	// Browser tools run in the visitor's browser and have no API call.
	Browser Kind = "browser"
)

// Files says what a pipeline tool reads.
type Files struct {
	// Field is the request field: "file_id" for one upload, "file_ids" for a
	// list.
	Field string `json:"field"`
	Min   int    `json:"min"`
	Max   int    `json:"max"`
}

// Tool is one entry of the table.
type Tool struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Summary     string `json:"summary"`
	Description string `json:"description,omitempty"`
	Group       string `json:"group"`
	Kind        Kind   `json:"kind"`
	// Area and Command place the tool in the CLI: `vukem-tools <area> <command>`.
	Area    string `json:"area,omitempty"`
	Command string `json:"command,omitempty"`
	// Operation is the OpenAPI operationId of the call.
	Operation string `json:"operation,omitempty"`
	// Files is nil for a tool that reads no upload, such as a download.
	Files *Files `json:"files,omitempty"`
	// Options is the JSON Schema of the request's options object, with every
	// reference resolved.
	Options json.RawMessage `json:"options,omitempty"`
	// OptionsRequired is true when the request cannot omit options.
	OptionsRequired bool `json:"options_required,omitempty"`
}

//go:embed tools.json
var rawTable []byte

var (
	loadOnce sync.Once
	table    []Tool
	errTable error
)

// All returns every tool, sorted by name.
func All() []Tool { return slices.Clone(load()) }

func load() []Tool {
	loadOnce.Do(func() { errTable = json.Unmarshal(rawTable, &table) })

	if errTable != nil {
		// The file is embedded and checked by the drift test, so this is a
		// build defect, not a runtime condition.
		panic(fmt.Sprintf("toolkit: tools.json is not valid: %v", errTable))
	}

	return table
}

// Find returns the tool called name.
func Find(name string) (Tool, bool) {
	for i := range load() {
		if table[i].Name == name {
			return table[i], true
		}
	}

	return Tool{}, false
}

// ByCommand returns the pipeline tool at `<area> <command>`.
func ByCommand(area, command string) (Tool, bool) {
	for i := range load() {
		if t := &table[i]; t.Kind == Pipeline && t.Area == area && t.Command == command {
			return *t, true
		}
	}

	return Tool{}, false
}

// Areas lists the CLI areas, sorted.
func Areas() []string {
	seen := map[string]bool{}

	for i := range load() {
		if table[i].Kind == Pipeline {
			seen[table[i].Area] = true
		}
	}

	out := make([]string, 0, len(seen))
	for a := range seen {
		out = append(out, a)
	}

	slices.Sort(out)

	return out
}

// Placement is where a tool lives in the CLI. Most names are
// "<area>-<command>"; the few that are not are placed by hand.
func Placement(name string) (area, command string) {
	switch name {
	case "images-to-pdf":
		return "image", "to-pdf"
	case "pixels-to-svg":
		return "image", "to-svg"
	case "word-to-pdf", "excel-to-pdf", "powerpoint-to-pdf":
		return "office", name
	}

	area, command, ok := strings.Cut(name, "-")
	if !ok {
		return "misc", name
	}

	return area, command
}
