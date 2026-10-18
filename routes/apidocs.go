package routes

import (
	"net/http"

	"github.com/AlexGalhardo/vukem"
	"github.com/AlexGalhardo/vukem/apidocs"
	"github.com/AlexGalhardo/vukem/openapi"
	"github.com/AlexGalhardo/vukem/router"
	"github.com/AlexGalhardo/vukem/web"

	"github.com/AlexGalhardo/vukem-tools/app/abilities"
	"github.com/AlexGalhardo/vukem-tools/internal/detect"
	"github.com/AlexGalhardo/vukem-tools/internal/httpx"
	"github.com/AlexGalhardo/vukem-tools/internal/jobs"
)

// The security scheme names used by the OpenAPI document. Both authenticate
// the same opaque key; a client sends one or the other.
const (
	// SchemeBearer is "Authorization: Bearer <key>". It is the name vukem
	// predefines, so api.Op.Security may use it.
	SchemeBearer = "bearer"
	// SchemeAPIKey is the X-API-Key header (internal/httpx).
	SchemeAPIKey = "apiKey"
)

// The OpenAPI tags, so the reference groups the operations.
const (
	tagTokens    = "API keys"
	tagAbilities = "Abilities"
	tagAccount   = "Account"
)

// APIPrefix is the path the document covers: /openapi.json describes the
// JSON API, not the Inertia pages.
const APIPrefix = "/api/v1"

// OpenAPIPath and DocsPath are where the document and the reference live.
const (
	OpenAPIPath = "/openapi.json"
	DocsPath    = "/docs"
)

const apiDescription = "The JSON API of Vukem Tools.\n\n" +
	"Authenticate every call with an API key, either as `Authorization: Bearer <key>` or as the " +
	"`" + httpx.APIKeyHeader + "` header. Issue a key with `POST /api/v1/tokens`; it carries only the " +
	"abilities you asked for, and the secret is shown once.\n\n" +
	"Errors are RFC 9457 problem documents (`application/problem+json`). A `422` adds an `errors` " +
	"object mapping each rejected field to its messages.\n\n" +
	"Rate limits: 120 requests per minute per IP, 60 per minute per key, and 5 key issues per " +
	"minute per IP. Over the limit is a `429` with `Retry-After`.\n\n" +
	"See /docs for the reference and docs/api.md in the repository for the overview."

// APIDocs serves the OpenAPI document and the Scalar API reference.
//
// Both are public: they describe a public API and contain no secret. The
// document is built per request, from the live route table, so a route added
// without a description cannot go unnoticed (tests/openapi_test.go diffs the
// two).
func APIDocs(r *router.Router, app *vukem.App) error {
	r.Get(OpenAPIPath, func(c *web.Ctx) error {
		doc, err := OpenAPIDocument(app)
		if err != nil {
			return err
		}

		return c.JSON(http.StatusOK, doc)
	}).Name("openapi").Doc(openapi.Skip{})

	// InProduction: the reference is the documentation of a public API, and
	// the Scalar bundle is embedded, so serving it costs no network call.
	apidocs.Serve(app, r, DocsPath, apidocs.Config{
		Title:   app.Config.Name + " API",
		SpecURL: OpenAPIPath,
		// No Theme: Scalar's default theme follows the system colour scheme,
		// and vukem/apidocs ships its AA contrast fix for that theme only.
		Authentication: map[string]any{"preferredSecurityScheme": SchemeBearer},
		InProduction:   true,
	})

	return nil
}

// OpenAPIDocument builds the OpenAPI 3.1 document for the JSON API.
//
// It is openapi.Build plus what the generator cannot derive from the routes:
// the X-API-Key security scheme and one example per request and response.
func OpenAPIDocument(app *vukem.App) (*openapi.Document, error) {
	// Routes() needs the router built: group prefixes and names are resolved
	// there, exactly as App.OpenAPI does it.
	if _, err := app.Router.Handler(); err != nil {
		return nil, err
	}

	doc, err := openapi.Build(app.Router.Routes(), openapi.Info{
		Title:       app.Config.Name + " API",
		Version:     app.Config.APIVersion,
		Description: apiDescription,
		Servers:     []string{app.Config.URL},
		PathPrefix:  APIPrefix,
	})
	if err != nil {
		return nil, err
	}

	describeSecurity(doc)
	describeExamples(doc)

	return doc, nil
}

// describeSecurity documents the X-API-Key alternative.
//
// workaround(vukem): api.Op.Security may only name a scheme the document it
// ends up in declares, and App.OpenAPI() (which vukemtest's
// AssertMatchesOpenAPI and the codegen commands use) builds its document with
// no Info.SecuritySchemes and no hook to add one. Naming "apiKey" in an Op
// would therefore make App.OpenAPI() fail with ErrUnknownSecurityScheme, so
// the Ops name only "bearer" and the second scheme is attached here.
func describeSecurity(doc *openapi.Document) {
	if doc.Components.SecuritySchemes == nil {
		return // nothing secured: no scheme to extend
	}

	doc.Components.SecuritySchemes[SchemeBearer] = openapi.Schema{
		"type":         "http",
		"scheme":       "bearer",
		"bearerFormat": "opaque",
		"description":  "An API key from POST /api/v1/tokens, sent as `Authorization: Bearer <key>`.",
	}
	doc.Components.SecuritySchemes[SchemeAPIKey] = openapi.Schema{
		"type": "apiKey",
		"in":   "header",
		"name": httpx.APIKeyHeader,
		"description": "The same API key in the `" + httpx.APIKeyHeader +
			"` header. `Authorization` wins when both are sent.",
	}

	for _, methods := range doc.Paths {
		for _, op := range methods {
			if len(op.Security) == 0 {
				continue // a public operation stays public
			}

			// A second entry in the list is an alternative, not an extra
			// requirement: either header authenticates the call.
			op.Security = append(op.Security, map[string][]string{SchemeAPIKey: {}})
		}
	}
}

// describeExamples attaches one example to every request body and response.
//
// workaround(vukem): openapi.MediaType carries a schema and nothing else, and
// no struct tag feeds an example, so the only place an example fits is the
// JSON Schema `examples` keyword of the schema itself.
func describeExamples(doc *openapi.Document) {
	for name, ex := range componentExamples() {
		if sc, ok := doc.Components.Schemas[name]; ok {
			sc["examples"] = []any{ex}
		}
	}

	// Both shared error responses reference the one Problem schema, so each
	// gets its example through an allOf wrapper rather than on the component.
	exampleResponse(doc, "Problem", problemExample())
	exampleResponse(doc, "ValidationError", validationProblemExample())

	notes := parameterNotes()

	for _, methods := range doc.Paths {
		for _, op := range methods {
			for i := range op.Parameters {
				p := &op.Parameters[i]

				note, ok := notes[op.OperationID+"."+p.Name]
				if !ok {
					continue
				}

				p.Description = note.description
				p.Schema["examples"] = []any{note.example}
			}

			ex, ok := requestExamples()[op.OperationID]
			if !ok || op.RequestBody == nil {
				continue
			}

			for _, mt := range op.RequestBody.Content {
				mt.Schema["examples"] = []any{ex}
			}
		}
	}
}

// paramNote documents one path or query parameter.
//
// workaround(vukem): openapi.Parameter is filled from the input type's fields,
// but only a query parameter keeps the field's `doc` tag; a path parameter
// loses it, and neither carries an example.
type paramNote struct {
	description string
	example     any
}

// parameterNotes maps "<operationId>.<parameter>" to its documentation.
func parameterNotes() map[string]paramNote {
	return map[string]paramNote{
		"tokens.revoke.token": {
			description: "The id of the key to revoke, as returned in the id field when it was issued.",
			example:     "k_7f3c1b9e",
		},
	}
}

// exampleResponse gives one shared response its own example without touching
// the schema the other one references.
func exampleResponse(doc *openapi.Document, name string, ex any) {
	resp, ok := doc.Components.Responses[name]
	if !ok {
		return
	}

	for ct, mt := range resp.Content {
		resp.Content[ct] = openapi.MediaType{
			Schema: openapi.Schema{"allOf": []any{mt.Schema}, "examples": []any{ex}},
		}
	}
}

// requestExamples maps an operationId to its request body example.
func requestExamples() map[string]any {
	return map[string]any{
		"conversions.store": map[string]any{
			"tool":            "pixels-to-svg",
			"file_id":         "01JQ8Z5X0K7M3N9P2R4T6V8W0Y",
			"idempotency_key": "sprite-batch-7",
			"options": map[string]any{
				"mode":       "rect",
				"alpha":      "opacity",
				"max_colors": 64,
				"tolerance":  0,
				"scale":      1,
				"simplify":   0,
			},
		},
		"tokens.store": map[string]any{
			"email":       "you@example.com",
			"password":    "your-account-password",
			"device_name": "cli",
			"abilities":   []any{abilities.ImageConvert, abilities.PDFCompress},
		},
	}
}

// examplePlaceholderKey shows the shape of an issued key, "vk_<id>.<secret>",
// without being one: a real secret comes from crypto/rand.Text (base32), so a
// hyphenated sentence cannot be mistaken for one, and
// TestNoExampleLeaksACredential asserts it.
const examplePlaceholderKey = "vk_7f3c1b9e.replace-this-with-the-key-you-receive"

// componentExamples maps a component schema name to its example. Nothing here
// is a real credential: the keys are obvious placeholders and the addresses
// use the reserved example.com domain (RFC 2606).
func componentExamples() map[string]any {
	return map[string]any{
		"TokenIssued": map[string]any{
			"id":         "k_7f3c1b9e",
			"token":      examplePlaceholderKey,
			"type":       "Bearer",
			"abilities":  []any{abilities.ImageConvert, abilities.PDFCompress},
			"expires_at": "2026-02-04T10:00:00Z",
		},
		"TokenList":         map[string]any{"data": []any{tokenExample()}},
		"Token":             tokenExample(),
		"AbilityList":       map[string]any{"data": toAny(abilities.All())},
		"ToolList":          map[string]any{"data": []any{"pixels-to-svg"}},
		"TypeList":          map[string]any{"data": toAny(detect.Types())},
		"ConversionView":    conversionExample(),
		"ConversionList":    map[string]any{"data": []any{conversionExample()}},
		"ErrorCatalog":      map[string]any{"data": errorCatalogExample()},
		"ErrorCatalogEntry": errorCatalogExample()[0],
		"User": map[string]any{
			"id":                1,
			"created_at":        "2026-01-05T09:58:12Z",
			"updated_at":        "2026-01-05T09:58:12Z",
			"name":              "Ada Lovelace",
			"email":             "you@example.com",
			"email_verified_at": "2026-01-05T10:01:44Z",
		},
	}
}

// conversionExample is one conversion, shown mid-run so every field that is
// null before a conversion ends is visible rather than guessed at.
func conversionExample() map[string]any {
	return map[string]any{
		"id":            "01JQ8Z5X0K7M3N9P2R4T6V8W0Y",
		"tool":          "pixels-to-svg",
		"status":        "running",
		"progress":      60,
		"phase":         "tracing",
		"bytes_in":      18_452,
		"bytes_out":     0,
		"error_type":    "",
		"error_message": "",
		"started_at":    "2026-02-03T10:00:02Z",
		"finished_at":   nil,
		"expires_at":    "2026-02-03T11:00:02Z",
		"created_at":    "2026-02-03T10:00:00Z",
	}
}

// errorCatalogExample is the real taxonomy, built from the same source the
// endpoint serves, so the example cannot drift from the answer.
func errorCatalogExample() []any {
	out := make([]any, 0, len(jobs.Catalog()))
	for _, info := range jobs.Catalog() {
		out = append(out, map[string]any{
			"type":    string(info.Type),
			"message": info.Message,
			"retry":   info.Retry,
		})
	}

	return out
}

// tokenExample is one row of GET /api/v1/tokens. user_id is empty because the
// listing query does not select it.
func tokenExample() map[string]any {
	return map[string]any{
		"id":           "k_7f3c1b9e",
		"user_id":      "",
		"name":         "cli",
		"abilities":    []any{abilities.ImageConvert, abilities.PDFCompress},
		"last_used_at": "2026-01-06T14:22:03Z",
		"expires_at":   "2026-02-04T10:00:00Z",
		"revoked_at":   nil,
	}
}

func problemExample() map[string]any {
	return map[string]any{
		"type":       "about:blank",
		"title":      "Unauthorized",
		"status":     401,
		"detail":     "This action requires authentication.",
		"instance":   "/api/v1/me",
		"request_id": "01JQZ8R4T6WQ2N9K3V5Y7B0C1D",
	}
}

func validationProblemExample() map[string]any {
	return map[string]any{
		"type":       "about:blank",
		"title":      "Validation failed",
		"status":     422,
		"detail":     "1 field(s) are invalid.",
		"instance":   "/api/v1/tokens",
		"request_id": "01JQZ8R4T6WQ2N9K3V5Y7B0C1D",
		"errors": map[string]any{
			"abilities": []any{"Unknown abilities: image:resize. Allowed: " + abilities.All()[0] + ", ..."},
		},
	}
}

func toAny(in []string) []any {
	out := make([]any, len(in))
	for i, v := range in {
		out[i] = v
	}

	return out
}
