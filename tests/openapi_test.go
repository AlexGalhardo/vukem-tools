package tests

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/AlexGalhardo/vukem/api"
	"github.com/AlexGalhardo/vukem/apidocs"
	"github.com/AlexGalhardo/vukem/openapi"
	"github.com/AlexGalhardo/vukem/vukemtest"

	"github.com/AlexGalhardo/vukem-tools/app/abilities"
	"github.com/AlexGalhardo/vukem-tools/app/controllers/apiv1"
	"github.com/AlexGalhardo/vukem-tools/bootstrap"
	"github.com/AlexGalhardo/vukem-tools/internal/httpx"
	"github.com/AlexGalhardo/vukem-tools/routes"
)

// document fetches /openapi.json with no credentials: it is documentation and
// must answer without authentication.
func document(t *testing.T, app *vukemtest.App) map[string]any {
	t.Helper()

	r := app.Client(t).Get(routes.OpenAPIPath).AssertOK().
		AssertHeaderContains("Content-Type", "application/json")

	var doc map[string]any
	if err := json.Unmarshal([]byte(r.Body()), &doc); err != nil {
		t.Fatalf("decode %s: %v", routes.OpenAPIPath, err)
	}

	return doc
}

// obj is one map in the document; a missing or wrongly typed key is a failure
// rather than a nil map, so a broken document cannot pass silently.
func obj(t *testing.T, v any, at string) map[string]any {
	t.Helper()

	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("%s is %T, want an object", at, v)
	}

	return m
}

func str(t *testing.T, m map[string]any, key, at string) string {
	t.Helper()

	s, ok := m[key].(string)
	if !ok || s == "" {
		t.Fatalf("%s.%s is %v, want a non-empty string", at, key, m[key])
	}

	return s
}

// operations lists every operation of the document, keyed by "METHOD /path".
func operations(t *testing.T, doc map[string]any) map[string]map[string]any {
	t.Helper()

	out := map[string]map[string]any{}

	for path, item := range obj(t, doc["paths"], "paths") {
		for method, op := range obj(t, item, "paths."+path) {
			out[strings.ToUpper(method)+" "+path] = obj(t, op, path+"."+method)
		}
	}

	return out
}

// The document is an OpenAPI 3.1 document, reachable without a key, and
// everything a reader needs is in it: both security schemes, a summary per
// operation, unique operationIds and no dangling $ref.
func TestOpenAPIDocumentIsPublicAndValid(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)
	doc := document(t, app)

	if got := doc["openapi"]; got != "3.1.0" {
		t.Errorf("openapi = %v, want 3.1.0", got)
	}

	info := obj(t, doc["info"], "info")
	str(t, info, "title", "info")
	str(t, info, "version", "info")
	str(t, info, "description", "info")

	servers, ok := doc["servers"].([]any)
	if !ok || len(servers) == 0 {
		t.Fatalf("servers = %v, want at least one", doc["servers"])
	}

	str(t, obj(t, servers[0], "servers.0"), "url", "servers.0")

	components := obj(t, doc["components"], "components")
	schemes := obj(t, components["securitySchemes"], "components.securitySchemes")

	bearer := obj(t, schemes[routes.SchemeBearer], "securitySchemes.bearer")
	if bearer["type"] != "http" || bearer["scheme"] != "bearer" {
		t.Errorf("bearer scheme = %v, want an HTTP bearer scheme", bearer)
	}

	apiKey := obj(t, schemes[routes.SchemeAPIKey], "securitySchemes.apiKey")
	if apiKey["type"] != "apiKey" || apiKey["in"] != "header" || apiKey["name"] != httpx.APIKeyHeader {
		t.Errorf("apiKey scheme = %v, want the %s header", apiKey, httpx.APIKeyHeader)
	}

	ids := map[string]string{}

	for key, op := range operations(t, doc) {
		id := str(t, op, "operationId", key)
		str(t, op, "summary", key)
		str(t, op, "description", key)

		if prev, dup := ids[id]; dup {
			t.Errorf("operationId %q is used by both %s and %s", id, prev, key)
		}

		ids[id] = key

		if len(obj(t, op["responses"], key+".responses")) == 0 {
			t.Errorf("%s documents no response", key)
		}
	}

	for _, ref := range refs(doc) {
		if resolve(doc, ref) == nil {
			t.Errorf("$ref %q does not resolve", ref)
		}
	}
}

// Every secured operation accepts either header, as two alternatives rather
// than as two requirements: a client sends one key, not both.
func TestSecuredOperationsAcceptEitherHeader(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)

	secured := 0

	for key, op := range operations(t, document(t, app)) {
		list, ok := op["security"].([]any)
		if !ok {
			continue
		}

		secured++

		var names []string

		for _, req := range list {
			for name := range obj(t, req, key+".security") {
				names = append(names, name)
			}
		}

		slices.Sort(names)

		if want := []string{routes.SchemeAPIKey, routes.SchemeBearer}; !slices.Equal(names, want) {
			t.Errorf("%s security = %v, want %v as alternatives", key, names, want)
		}

		if len(list) != 2 {
			t.Errorf("%s has %d security requirements, want 2 alternatives", key, len(list))
		}
	}

	// Counted from the router rather than written here: a number in a test
	// is one more thing to remember when a route is added, and forgetting it
	// is a test failure that teaches nothing.
	want := securedRoutes(t, app)
	if secured != want {
		t.Errorf("%d secured operations in the document, %d secured routes in the router", secured, want)
	}

	if want == 0 {
		t.Fatal("no secured route found: this test would pass on an empty document")
	}
}

// securedRoutes counts the /api/v1 routes that sit behind the API guard, by
// asking the router rather than by listing them.
func securedRoutes(t *testing.T, app *vukemtest.App) int {
	t.Helper()

	n := 0

	for _, rt := range app.Router.Routes() {
		if _, skip := rt.Doc.(openapi.Skip); skip || rt.Method == "" || rt.Method == "ANY" {
			continue
		}

		op, ok := rt.Doc.(*api.Operation)
		if !ok {
			continue
		}

		if len(op.Security) > 0 {
			n++
		}
	}

	return n
}

// A route the router serves but the document does not describe is a hole in
// the documentation, so CI fails on it instead of shipping it.
func TestEveryAPIRouteAppearsInTheDocument(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)

	if _, err := app.Router.Handler(); err != nil {
		t.Fatal(err)
	}

	var inRouter []string

	for _, rt := range app.Router.Routes() {
		if _, skip := rt.Doc.(openapi.Skip); skip || rt.Method == "" || rt.Method == "ANY" {
			continue
		}

		if rt.Path == routes.APIPrefix || strings.HasPrefix(rt.Path, routes.APIPrefix+"/") {
			inRouter = append(inRouter, rt.Method+" "+rt.Path)
		}
	}

	var inDocument []string
	for key := range operations(t, document(t, app)) {
		inDocument = append(inDocument, key)
	}

	slices.Sort(inRouter)
	slices.Sort(inDocument)

	if !slices.Equal(inRouter, inDocument) {
		t.Errorf("the router and the document disagree:\nrouter:   %v\ndocument: %v\n"+
			"undocumented: %v\nnot routed: %v",
			inRouter, inDocument, missing(inRouter, inDocument), missing(inDocument, inRouter))
	}

	if len(inRouter) == 0 {
		t.Fatal("no API route found: the diff would pass on an empty document")
	}
}

// Every request body and every response that has a body carries an example,
// the RFC 9457 problem documents included.
func TestEveryRequestAndResponseHasAnExample(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)
	doc := document(t, app)

	bodies := 0

	for key, op := range operations(t, doc) {
		if body, ok := op["requestBody"].(map[string]any); ok {
			bodies++

			for ct, mt := range obj(t, body["content"], key+".requestBody.content") {
				if !hasExample(doc, obj(t, mt, key+"."+ct)["schema"], 0) {
					t.Errorf("%s request body (%s) has no example", key, ct)
				}
			}
		}

		for status, resp := range obj(t, op["responses"], key+".responses") {
			r := obj(t, resolveResponse(t, doc, resp, key+"."+status), key+"."+status)

			content, ok := r["content"].(map[string]any)
			if !ok {
				continue // 204 and the like: no body to document
			}

			for ct, mt := range content {
				if !hasExample(doc, obj(t, mt, key+"."+status+"."+ct)["schema"], 0) {
					t.Errorf("%s response %s (%s) has no example", key, status, ct)
				}
			}
		}
	}

	if bodies == 0 {
		t.Fatal("no request body found: the assertion above would pass on anything")
	}

	// The error shape is documented once and shared; both variants carry their
	// own example, and the 422 one shows the errors map.
	responses := obj(t, obj(t, doc["components"], "components")["responses"], "components.responses")
	for _, name := range []string{"Problem", "ValidationError"} {
		if _, ok := responses[name]; !ok {
			t.Errorf("components.responses.%s is missing", name)
		}
	}

	if !strings.Contains(exampleJSON(t, doc, "ValidationError"), `"errors"`) {
		t.Error("the ValidationError example does not show the errors object")
	}
}

// The documentation is public, so nothing in it may be a usable credential or
// a real address.
func TestNoExampleLeaksACredential(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)

	// A live key, issued the way a client would, must not appear anywhere in
	// the document: the examples are written by hand and stay that way.
	key := issueKey(t, app, "leak@example.com", abilities.ImageConvert)
	app.Client(t).Get(routes.OpenAPIPath).AssertOK().AssertDontSee(key)

	doc := document(t, app)
	// A real key is "vk_<id>.<secret>" with both halves from crypto/rand.Text
	// (base32, alphanumeric). A placeholder with words and hyphens in it is
	// not one, and is what the examples use.
	liveKey := regexp.MustCompile(`^vk_[A-Za-z0-9]+\.[A-Za-z0-9]{20,}$`)

	for _, s := range exampleStrings(doc) {
		if strings.Contains(s, "@") && !strings.HasSuffix(s, "@example.com") {
			t.Errorf("example %q is not a reserved example.com address", s)
		}

		if liveKey.MatchString(s) {
			t.Errorf("example %q has the shape of a real key", s)
		}
	}
}

// The reference renders from the embedded bundle under the app's secure
// headers: the nonce of the CSP covers every script on the page, and nothing
// is fetched from another host.
func TestDocsPageServesScalarUnderTheCSP(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)

	// No credentials: the reference is documentation.
	page := app.Client(t).Get(routes.DocsPath).AssertOK().
		AssertHeaderContains("Content-Type", "text/html")

	csp := page.Header("Content-Security-Policy")
	if csp == "" {
		t.Fatal("no Content-Security-Policy on the reference page")
	}

	if strings.Contains(csp, "unsafe-inline") {
		t.Errorf("the CSP allows unsafe-inline: %s", csp)
	}

	nonce := regexp.MustCompile(`'nonce-([^']+)'`).FindStringSubmatch(csp)
	if nonce == nil {
		t.Fatalf("the CSP carries no nonce: %s", csp)
	}

	body := page.Body()

	scripts := regexp.MustCompile(`<script([^>]*)>`).FindAllStringSubmatch(body, -1)
	if len(scripts) < 3 {
		t.Fatalf("%d script tags, want the shim, the bundle and the inline call", len(scripts))
	}

	for _, s := range scripts {
		if !strings.Contains(s[1], `nonce="`+nonce[1]+`"`) {
			t.Errorf("script tag <script%s> is not covered by the CSP nonce %q", s[1], nonce[1])
		}
	}

	for _, s := range regexp.MustCompile(`<style([^>]*)>`).FindAllStringSubmatch(body, -1) {
		if !strings.Contains(s[1], `nonce="`+nonce[1]+`"`) {
			t.Errorf("style tag <style%s> is not covered by the CSP nonce %q", s[1], nonce[1])
		}
	}

	// Nothing may be loaded from another origin: the CSP would block it, and
	// an offline reader would see a broken page.
	if m := regexp.MustCompile(`(?:src|href)="(https?:)?//[^"]+"`).FindString(body); m != "" {
		t.Errorf("the page loads %s from another host", m)
	}

	bundle := routes.DocsPath + "/scalar.js?v=" + apidocs.ScalarVersion
	page.AssertSee(bundle)
	page.AssertSee(routes.OpenAPIPath)

	// The bundle is the pinned one, byte for byte.
	js := app.Client(t).Get(bundle).AssertOK().
		AssertHeaderContains("Content-Type", "text/javascript")

	if sum := sha256.Sum256([]byte(js.Body())); hex.EncodeToString(sum[:]) != apidocs.ScalarSHA256 {
		t.Errorf("the Scalar bundle is not the pinned %s build", apidocs.ScalarVersion)
	}

	app.Client(t).Get(routes.DocsPath+"/page.js").AssertOK().
		AssertHeaderContains("Content-Type", "text/javascript")
}

// The document and the handlers must not drift: every answer, the error shapes
// included, is validated against the document that describes it.
func TestResponsesMatchTheDocument(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)

	// No key: the 401 problem document.
	app.Client(t).GetJSON("/api/v1/me").AssertUnauthorized().AssertMatchesOpenAPI()

	// A rejected body: the 422 problem document, with the errors map.
	app.Client(t).PostJSON("/api/v1/tokens", map[string]any{"email": "not-an-address"}).
		AssertUnprocessable().AssertJSONValidationErrors("email", "password", "device_name", "abilities").
		AssertMatchesOpenAPI()

	// An unknown ability is a validation error on abilities, not a login
	// failure, and it matches the same documented shape.
	app.Client(t).PostJSON("/api/v1/tokens", map[string]any{
		"email": "drift@example.com", "password": "correct horse battery",
		"device_name": "cli", "abilities": []string{"image:resize"},
	}).AssertUnprocessable().AssertJSONValidationErrors("abilities").AssertMatchesOpenAPI()

	createAPIUser(t, app, "drift@example.com", true)

	issued := app.Client(t).PostJSON("/api/v1/tokens", map[string]any{
		"email": "drift@example.com", "password": "correct horse battery",
		"device_name": "cli", "abilities": []string{abilities.ImageConvert},
	}).AssertCreated().AssertMatchesOpenAPI()

	key, _ := issued.JSONPath("token")
	token, _ := key.(string)
	client := app.Client(t).WithToken(token)

	app.Client(t).GetJSON("/api/v1/abilities").AssertOK().AssertMatchesOpenAPI()
	client.GetJSON("/api/v1/me").AssertOK().AssertMatchesOpenAPI()
	client.GetJSON("/api/v1/tokens").AssertOK().AssertMatchesOpenAPI()

	// A key id that does not exist: the 404 problem document.
	client.Delete("/api/v1/tokens/does-not-exist").AssertNotFound().AssertMatchesOpenAPI()

	// And the 204, which documents no body.
	client.Delete("/api/v1/tokens/current").AssertNoContent().AssertMatchesOpenAPI()
}

// The cap on abilities is a literal in a `validate` tag, so it can only be
// kept honest by a test: adding a tool must widen it.
func TestAbilitiesCapMatchesTheAbilityList(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)

	if apiv1.MaxAbilitiesPerKey != len(abilities.All()) {
		t.Errorf("MaxAbilitiesPerKey = %d, want %d (the IssueInput validate tag needs the same number)",
			apiv1.MaxAbilitiesPerKey, len(abilities.All()))
	}

	doc := document(t, app)
	op := operations(t, doc)["POST /api/v1/tokens"]
	body := obj(t, op["requestBody"], "tokens.store.requestBody")
	schema := obj(t, obj(t, body["content"], "content")["application/json"], "application/json")
	props := obj(t, obj(t, schema["schema"], "schema")["properties"], "properties")
	list := obj(t, props["abilities"], "properties.abilities")

	if got, want := list["maxItems"], float64(len(abilities.All())); got != want {
		t.Errorf("abilities.maxItems = %v, want %v", got, want)
	}
}

// The committed clients are generated from this document, so a new operation
// that nobody regenerated for shows up here instead of in a client that cannot
// call it.
func TestGeneratedClientsCoverEveryOperation(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)

	goSrc := readFile(t, "../clients/apiclient/client.go")
	tsSrc := readFile(t, "../clients/typescript/client.ts")

	for key, op := range operations(t, document(t, app)) {
		id, _ := op["operationId"].(string)
		name := clientMethodName(id)

		if !strings.Contains(goSrc, "func (c *Client) "+name+"(") {
			t.Errorf("the Go client has no %s method for %s (%s)", name, id, key)
		}

		ts := strings.ToLower(name[:1]) + name[1:]
		if !strings.Contains(tsSrc, "\n  "+ts+"(") {
			t.Errorf("the TypeScript client has no %s method for %s (%s)", ts, id, key)
		}
	}
}

// clientMethodName is how both generators name an operation: "tokens.store"
// becomes TokensStore. The generators also apply Go initialisms (id, url,
// ...), which none of this API's operationIds contain.
func clientMethodName(operationID string) string {
	var b strings.Builder

	for _, part := range strings.FieldsFunc(operationID, func(r rune) bool {
		return r == '.' || r == '_' || r == '-'
	}) {
		b.WriteString(strings.ToUpper(part[:1]) + part[1:])
	}

	return b.String()
}

func readFile(t *testing.T, path string) string {
	t.Helper()

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	return string(b)
}

// missing returns the entries of a that b does not have.
func missing(a, b []string) []string {
	var out []string

	for _, v := range a {
		if !slices.Contains(b, v) {
			out = append(out, v)
		}
	}

	return out
}

// refs collects every $ref value in the document.
func refs(v any) []string {
	var out []string

	switch t := v.(type) {
	case map[string]any:
		for k, sub := range t {
			if k == "$ref" {
				if s, ok := sub.(string); ok {
					out = append(out, s)
				}

				continue
			}

			out = append(out, refs(sub)...)
		}
	case []any:
		for _, sub := range t {
			out = append(out, refs(sub)...)
		}
	}

	return out
}

// resolve follows a local JSON pointer ("#/components/schemas/Problem").
func resolve(doc map[string]any, ref string) any {
	if !strings.HasPrefix(ref, "#/") {
		return nil
	}

	var cur any = doc

	for part := range strings.SplitSeq(strings.TrimPrefix(ref, "#/"), "/") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}

		if cur, ok = m[part]; !ok {
			return nil
		}
	}

	return cur
}

// resolveResponse follows a response that is only a $ref to a shared one.
func resolveResponse(t *testing.T, doc map[string]any, resp any, at string) any {
	t.Helper()

	m := obj(t, resp, at)

	ref, ok := m["$ref"].(string)
	if !ok {
		return m
	}

	target := resolve(doc, ref)
	if target == nil {
		t.Fatalf("%s: $ref %q does not resolve", at, ref)
	}

	return target
}

// hasExample reports whether a schema documents an example, directly or
// through the $ref or allOf wrapper it is reached by.
func hasExample(doc map[string]any, schema any, depth int) bool {
	if depth > 8 {
		return false
	}

	m, ok := schema.(map[string]any)
	if !ok {
		return false
	}

	if ex, ok := m["examples"].([]any); ok && len(ex) > 0 {
		return true
	}

	if _, ok := m["example"]; ok {
		return true
	}

	if ref, ok := m["$ref"].(string); ok {
		return hasExample(doc, resolve(doc, ref), depth+1)
	}

	for _, sub := range asSlice(m["allOf"]) {
		if hasExample(doc, sub, depth+1) {
			return true
		}
	}

	return false
}

// exampleStrings collects every string inside every "examples" value.
func exampleStrings(v any) []string {
	var out []string

	switch t := v.(type) {
	case map[string]any:
		for k, sub := range t {
			if k == "examples" || k == "example" {
				out = append(out, allStrings(sub)...)
				continue
			}

			out = append(out, exampleStrings(sub)...)
		}
	case []any:
		for _, sub := range t {
			out = append(out, exampleStrings(sub)...)
		}
	}

	return out
}

// allStrings collects every string in v, however deeply nested.
func allStrings(v any) []string {
	switch t := v.(type) {
	case string:
		return []string{t}
	case []any:
		var out []string
		for _, sub := range t {
			out = append(out, allStrings(sub)...)
		}

		return out
	case map[string]any:
		var out []string
		for _, sub := range t {
			out = append(out, allStrings(sub)...)
		}

		return out
	default:
		return nil
	}
}

// exampleJSON returns the example of a shared response, as JSON.
func exampleJSON(t *testing.T, doc map[string]any, response string) string {
	t.Helper()

	resp := resolve(doc, "#/components/responses/"+response)
	if resp == nil {
		t.Fatalf("components.responses.%s is missing", response)
	}

	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}

	return string(b)
}

func asSlice(v any) []any {
	s, _ := v.([]any)

	return s
}
