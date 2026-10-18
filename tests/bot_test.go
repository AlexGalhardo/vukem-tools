package tests

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/AlexGalhardo/vukem/api"
	"github.com/AlexGalhardo/vukem/openapi"
	"github.com/AlexGalhardo/vukem/vukemtest"

	"github.com/AlexGalhardo/vukem-tools/app/services"
	"github.com/AlexGalhardo/vukem-tools/bootstrap"
	"github.com/AlexGalhardo/vukem-tools/internal/bot"
	"github.com/AlexGalhardo/vukem-tools/internal/jobs"
)

// The bot holds an API key, and the point of that key is that it carries as
// little as possible: a compromised bot should be able to start a download
// and nothing else.
//
// Asserting that against the live route table rather than against a list in
// the bot is what makes it hold. A list agrees with itself; the route table
// is the thing that actually decides what the key can reach, and the
// pipeline is the thing that actually decides which ability a route demands.

// botOperations returns the operationId of every route the bot can call,
// derived from the bot.API interface rather than from a list.
//
// The generated client names a method after its operationId, so the
// interface's method set *is* the set of operations the bot can reach:
// adding a call to the bot means adding a method here, which this test then
// requires a route for.
func botOperations(t *testing.T) []string {
	t.Helper()

	iface := reflect.TypeFor[bot.API]()

	names := make([]string, 0, iface.NumMethod())
	for method := range iface.Methods() {
		names = append(names, method.Name)
	}

	if len(names) == 0 {
		t.Fatal("bot.API has no methods, so this test would pass on anything")
	}

	return names
}

// apiOperations maps every documented operation's client method name to its
// operationId and the security it declares.
type apiOperation struct {
	id       string
	method   string
	path     string
	secured  bool
	toolName string
}

func apiOperations(t *testing.T, app *vukemtest.App) map[string]apiOperation {
	t.Helper()

	if _, err := app.Router.Handler(); err != nil {
		t.Fatal(err)
	}

	out := map[string]apiOperation{}

	for _, rt := range app.Router.Routes() {
		if _, skip := rt.Doc.(openapi.Skip); skip || rt.Method == "" || rt.Method == "ANY" {
			continue
		}

		op, ok := rt.Doc.(*api.Operation)
		if !ok {
			continue
		}

		out[clientMethodName(op.Name)] = apiOperation{
			id:       op.Name,
			method:   rt.Method,
			path:     rt.Path,
			secured:  len(op.Security) > 0,
			toolName: toolOf(op.Name),
		}
	}

	return out
}

// toolOf returns the tool an operationId submits to, or "" when it is not a
// submit route. The ids are "tools.<tool name>", which is the convention
// every per-tool route follows.
func toolOf(operationID string) string {
	name, ok := strings.CutPrefix(operationID, "tools.")
	if !ok {
		return ""
	}

	// "tools.video-download.metadata" is the probe for the download tool,
	// not a tool of its own.
	if cut, found := strings.CutSuffix(name, ".metadata"); found {
		return cut
	}

	return name
}

func TestEveryRouteTheBotCallsExistsInTheRouteTable(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)
	operations := apiOperations(t, app)

	for _, name := range botOperations(t) {
		op, ok := operations[name]
		if !ok {
			t.Errorf("the bot calls %s, which no route serves", name)
			continue
		}

		// Every one of them has to be behind the API guard: a bot that could
		// reach an unauthenticated route would be a bot whose key is
		// decoration.
		if !op.secured {
			t.Errorf("the bot calls %s (%s %s), which declares no security", name, op.method, op.path)
		}
	}
}

func TestTheBotsKeyHasNoAbilityItDoesNotUse(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)

	conversions := services.NewConversions()
	if err := conversions.Resolve(app.App); err != nil {
		t.Fatalf("resolve the pipeline: %v", err)
	}

	pipeline := conversions.Pipeline()
	operations := apiOperations(t, app)

	// What the routes the bot calls actually demand, read off the tools
	// rather than assumed: requireAbility asks the tool, so the tool is the
	// authority on which ability a submit route needs.
	needed := map[string]string{}

	for _, name := range botOperations(t) {
		op, ok := operations[name]
		if !ok {
			continue // reported by the test above
		}

		if op.toolName == "" {
			continue // reading or cancelling: the caller's own work, no ability
		}

		tool, ok := pipeline.Tool(op.toolName)
		if !ok {
			// The engine is missing from this machine, so the tool is not
			// registered. The ability it would need is still knowable, but
			// not from here, and the test must not pass by finding nothing.
			t.Skipf("the %s tool is not registered on this machine, so its ability cannot be read", op.toolName)
		}

		needed[tool.Ability()] = op.id
	}

	if len(needed) == 0 {
		t.Fatal("no ability was found to be needed, so this test would pass on an empty key")
	}

	declared := bot.Abilities()

	// Nothing the key carries goes unused.
	for _, ability := range declared {
		if _, used := needed[ability]; !used {
			t.Errorf("the bot's key carries %q, which no route it calls requires", ability)
		}
	}

	// And nothing it needs is missing, or the bot is broken in production
	// and nowhere else.
	for ability, operation := range needed {
		if !slices.Contains(declared, ability) {
			t.Errorf("the bot calls %s, which needs %q, and its key does not carry it", operation, ability)
		}
	}
}

func TestTheBotCannotReachAnAbilityCarryingRouteItDoesNotDeclare(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)

	conversions := services.NewConversions()
	if err := conversions.Resolve(app.App); err != nil {
		t.Fatalf("resolve the pipeline: %v", err)
	}

	pipeline := conversions.Pipeline()
	called := botOperations(t)
	operations := apiOperations(t, app)

	// The other direction: every submit route the bot does *not* call must
	// need an ability the bot does not hold, or the key is wider than the
	// bot's own method set suggests.
	for name, op := range operations {
		if op.toolName == "" || slices.Contains(called, name) {
			continue
		}

		tool, ok := pipeline.Tool(op.toolName)
		if !ok {
			continue // not registered on this machine
		}

		if slices.Contains(bot.Abilities(), tool.Ability()) {
			t.Errorf("the bot's key can submit %s (%s), which the bot never calls", op.id, tool.Ability())
		}
	}
}

func TestTheBotsAbilitiesAreAllRealAbilities(t *testing.T) {
	t.Parallel()

	// A typo here would mint a key that grants nothing and fails only at
	// call time, which is the failure the closed ability set exists to
	// prevent.
	app := vukemtest.New(t, bootstrap.New)

	res := app.Client(t).GetJSON("/api/v1/abilities").AssertOK()

	var body struct {
		Data []string `json:"data"`
	}

	// workaround(vukem): vukemtest.Response has no decode-into-struct helper,
	// so every test that needs a typed body unmarshals Body() by hand.
	if err := json.Unmarshal([]byte(res.Body()), &body); err != nil {
		t.Fatalf("decode the abilities: %v", err)
	}

	for _, ability := range bot.Abilities() {
		if !slices.Contains(body.Data, ability) {
			t.Errorf("the bot asks for %q, which the API does not publish", ability)
		}
	}
}

func TestTheBotsErrorsComeFromThePublishedTaxonomy(t *testing.T) {
	t.Parallel()

	// The bot shows the API's own error_message rather than inventing its
	// own wording. This is the check that there is one taxonomy to come
	// from: the codes the API publishes are the ones jobs.Catalog defines,
	// so the bot, the web interface and the API cannot word the same failure
	// three ways.
	app := vukemtest.New(t, bootstrap.New)

	res := app.Client(t).GetJSON("/api/v1/conversion-errors").AssertOK()

	var body struct {
		Data []struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"data"`
	}

	// workaround(vukem): see above.
	if err := json.Unmarshal([]byte(res.Body()), &body); err != nil {
		t.Fatalf("decode the error catalogue: %v", err)
	}

	if len(body.Data) != len(jobs.Catalog()) {
		t.Fatalf("the API publishes %d error codes and the taxonomy has %d", len(body.Data), len(jobs.Catalog()))
	}

	for _, entry := range body.Data {
		if entry.Message == "" {
			t.Errorf("%s has no message for the bot to show", entry.Type)
		}

		if !jobs.Known(jobs.ErrorType(entry.Type)) {
			t.Errorf("%s is published but is not in the taxonomy", entry.Type)
		}
	}
}
