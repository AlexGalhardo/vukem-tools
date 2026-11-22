package toolkit_test

import (
	"bytes"
	"encoding/json"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/AlexGalhardo/vukem/vukemtest"

	"github.com/AlexGalhardo/vukem-tools/app/catalog"
	"github.com/AlexGalhardo/vukem-tools/bootstrap"
	"github.com/AlexGalhardo/vukem-tools/internal/toolkit"
	"github.com/AlexGalhardo/vukem-tools/internal/toolkit/toolkittest"
)

// updateEnv rewrites tools.json from the catalogue and the OpenAPI document:
//
//	TOOLKIT_UPDATE=1 go test ./internal/toolkit/ -run TestTheTableMatchesTheCatalogueAndTheAPI
const updateEnv = "TOOLKIT_UPDATE"

const toolsPrefix = "/api/v1/tools/"

// liveEntries is every catalogue entry whose own status is live. The
// catalogue downgrades a server tool to planned unless the pipeline names it,
// so the raw entry is read instead.
func liveEntries() []catalog.Tool {
	var out []catalog.Tool

	listed := catalog.All(nil)

	for i := range listed {
		if raw, ok := catalog.Find(listed[i].Name); ok && raw.Status == catalog.Live {
			out = append(out, raw)
		}
	}

	return out
}

// generate builds the table from the two sources of truth.
func generate(t *testing.T) []toolkit.Tool {
	t.Helper()

	app := vukemtest.New(t, bootstrap.New, vukemtest.Options{Env: toolkittest.Sandbox(t)})

	var doc map[string]any
	if err := json.Unmarshal([]byte(app.Client(t).Get("/openapi.json").AssertOK().Body()), &doc); err != nil {
		t.Fatalf("decode the OpenAPI document: %v", err)
	}

	paths, _ := doc["paths"].(map[string]any)

	var out []toolkit.Tool

	for path, item := range paths {
		name, ok := strings.CutPrefix(path, toolsPrefix)
		if !ok || strings.Contains(name, "/") {
			continue
		}

		methods, _ := item.(map[string]any)

		post, _ := methods["post"].(map[string]any)
		if post == nil {
			continue
		}

		out = append(out, pipelineTool(t, doc, name, post))
	}

	live := liveEntries()

	for i := range live {
		if entry := &live[i]; entry.Client {
			out = append(out, toolkit.Tool{
				Name: entry.Name, Title: entry.Title, Summary: entry.Summary, Group: entry.Group, Kind: toolkit.Browser,
			})
		}
	}

	slices.SortFunc(out, func(a, b toolkit.Tool) int { return strings.Compare(a.Name, b.Name) })

	return out
}

func pipelineTool(t *testing.T, doc map[string]any, name string, post map[string]any) toolkit.Tool {
	t.Helper()

	tool := toolkit.Tool{Name: name, Kind: toolkit.Pipeline, Operation: str(post["operationId"])}
	tool.Area, tool.Command = toolkit.Placement(name)
	tool.Description = str(post["description"])

	if entry, ok := catalog.Find(name); ok {
		tool.Title, tool.Summary, tool.Group = entry.Title, entry.Summary, entry.Group
	} else {
		t.Errorf("%s has an API operation and no catalogue entry", name)
	}

	body, _ := dig(post, "requestBody", "content", "application/json", "schema").(map[string]any)
	props, _ := body["properties"].(map[string]any)

	switch {
	case props["file_id"] != nil:
		tool.Files = &toolkit.Files{Field: "file_id", Min: 1, Max: 1}
	case props["file_ids"] != nil:
		ids, _ := props["file_ids"].(map[string]any)
		tool.Files = &toolkit.Files{Field: "file_ids", Min: intOf(ids["minItems"], 1), Max: intOf(ids["maxItems"], 64)}
	}

	required, _ := body["required"].([]any)
	tool.OptionsRequired = slices.Contains(required, any("options"))

	if opts, ok := props["options"].(map[string]any); ok {
		raw, err := json.Marshal(resolve(t, doc, opts))
		if err != nil {
			t.Fatalf("%s options: %v", name, err)
		}

		tool.Options = raw
	}

	return tool
}

// resolve inlines every $ref, and folds the {allOf:[{$ref}], description}
// wrapper the generator emits around a described reference.
func resolve(t *testing.T, doc map[string]any, v any) any {
	t.Helper()

	switch node := v.(type) {
	case map[string]any:
		if ref, ok := node["$ref"].(string); ok {
			return resolve(t, doc, lookup(t, doc, ref))
		}

		if all, ok := node["allOf"].([]any); ok && len(all) == 1 {
			merged, _ := resolve(t, doc, all[0]).(map[string]any)
			out := map[string]any{}

			maps.Copy(out, merged)

			for k, val := range node {
				if k != "allOf" {
					out[k] = resolve(t, doc, val)
				}
			}

			return out
		}

		out := make(map[string]any, len(node))

		for k, val := range node {
			if k == "examples" {
				continue
			}

			out[k] = resolve(t, doc, val)
		}

		return out
	case []any:
		out := make([]any, len(node))
		for i, val := range node {
			out[i] = resolve(t, doc, val)
		}

		return out
	default:
		return v
	}
}

func lookup(t *testing.T, doc map[string]any, ref string) any {
	t.Helper()

	name, ok := strings.CutPrefix(ref, "#/components/schemas/")
	if !ok {
		t.Fatalf("unsupported $ref %q", ref)
	}

	return dig(doc, "components", "schemas", name)
}

func dig(v any, keys ...string) any {
	for _, k := range keys {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}

		v = m[k]
	}

	return v
}

func str(v any) string {
	s, _ := v.(string)

	return s
}

func intOf(v any, fallback int) int {
	if f, ok := v.(float64); ok {
		return int(f)
	}

	return fallback
}

func TestTheTableMatchesTheCatalogueAndTheAPI(t *testing.T) {
	want, err := json.MarshalIndent(generate(t), "", "\t")
	if err != nil {
		t.Fatal(err)
	}

	want = append(want, '\n')

	if os.Getenv(updateEnv) == "1" {
		if err := os.WriteFile("tools.json", want, 0o600); err != nil {
			t.Fatal(err)
		}

		return
	}

	got, err := os.ReadFile("tools.json")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n")), want) {
		t.Fatal("tools.json is out of date with the catalogue or the OpenAPI document. Regenerate it: " +
			updateEnv + "=1 go test ./internal/toolkit/ -run TestTheTableMatchesTheCatalogueAndTheAPI")
	}
}

func TestEveryLiveCatalogueToolHasOneEntryAndNothingElse(t *testing.T) {
	t.Parallel()

	listed := map[string]toolkit.Tool{}

	for _, tool := range toolkit.All() {
		if _, dup := listed[tool.Name]; dup {
			t.Errorf("%s is listed twice", tool.Name)
		}

		listed[tool.Name] = tool
	}

	live := liveEntries()

	if len(live) < 50 {
		t.Fatalf("only %d live catalogue tools: the check would pass on an empty catalogue", len(live))
	}

	for _, entry := range live {
		tool, ok := listed[entry.Name]
		if !ok {
			t.Errorf("live catalogue tool %s has no entry in the table", entry.Name)

			continue
		}

		want := toolkit.Pipeline
		if entry.Client {
			want = toolkit.Browser
		}

		if tool.Kind != want {
			t.Errorf("%s is %s in the table, want %s", entry.Name, tool.Kind, want)
		}
	}

	if len(listed) != len(live) {
		t.Errorf("the table has %d entries, the catalogue has %d live tools", len(listed), len(live))
	}
}
