package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/AlexGalhardo/vukem-tools/internal/mcp"
	"github.com/AlexGalhardo/vukem-tools/internal/testenv"
	"github.com/AlexGalhardo/vukem-tools/internal/toolkit"
	"github.com/AlexGalhardo/vukem-tools/internal/toolkit/toolkittest"
)

func newServer(env *toolkittest.Env, key string) *mcp.Server {
	r := toolkit.NewRunner(toolkit.Config{BaseURL: env.URL, Key: key}, nil)
	r.Poll = 50 * time.Millisecond

	return mcp.New(r, "test")
}

// rpc sends one request through the real stdio loop and returns the answer.
func rpc(t *testing.T, srv *mcp.Server, method string, params any) map[string]any {
	t.Helper()

	raw, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()

	var out, errOut bytes.Buffer

	if code := srv.Serve(ctx, bytes.NewReader(append(raw, '\n')), &out, &errOut); code != 0 {
		t.Fatalf("Serve exited %d: %s", code, errOut.String())
	}

	var answer map[string]any
	if err := json.Unmarshal(out.Bytes(), &answer); err != nil {
		t.Fatalf("the answer is not JSON: %v\n%s", err, out.String())
	}

	return answer
}

// call runs tools/call and returns the tool result.
func call(t *testing.T, srv *mcp.Server, name string, args map[string]any) map[string]any {
	t.Helper()

	answer := rpc(t, srv, "tools/call", map[string]any{"name": name, "arguments": args})

	result, ok := answer["result"].(map[string]any)
	if !ok {
		t.Fatalf("tools/call %s answered %v, want a result", name, answer)
	}

	return result
}

func structured(t *testing.T, result map[string]any) map[string]any {
	t.Helper()

	s, ok := result["structuredContent"].(map[string]any)
	if !ok {
		t.Fatalf("no structuredContent in %v", result)
	}

	return s
}

func TestInitializeAndListEveryTool(t *testing.T) {
	t.Parallel()

	srv := mcp.New(toolkit.NewRunner(toolkit.Config{BaseURL: "http://127.0.0.1:1"}, nil), "9.9.9")

	init := rpc(t, srv, "initialize", map[string]any{"protocolVersion": "2025-03-26"})["result"].(map[string]any)
	if init["protocolVersion"] != "2025-03-26" || init["serverInfo"].(map[string]any)["version"] != "9.9.9" {
		t.Fatalf("initialize = %v", init)
	}

	list := rpc(t, srv, "tools/list", nil)["result"].(map[string]any)["tools"].([]any)

	var names []string

	for _, item := range list {
		tool := item.(map[string]any)
		names = append(names, tool["name"].(string))

		schema := tool["inputSchema"].(map[string]any)
		if schema["type"] != "object" {
			t.Errorf("%v has no object input schema", tool["name"])
		}

		meta := tool["_meta"].(map[string]any)
		if meta["kind"] == "pipeline" {
			if !slices.Contains(schema["required"].([]any), any("output")) {
				t.Errorf("%v does not require an output", tool["name"])
			}
		}
	}

	var want []string
	for _, tl := range toolkit.All() {
		want = append(want, tl.Name)
	}

	slices.Sort(names)
	slices.Sort(want)

	if !slices.Equal(names, want) || len(names) != 54 {
		t.Fatalf("tools/list has %d names, the table has %d (want 54)", len(names), len(want))
	}
}

func TestANotificationGetsNoAnswer(t *testing.T) {
	t.Parallel()

	srv := mcp.New(toolkit.NewRunner(toolkit.Config{BaseURL: "http://127.0.0.1:1"}, nil), "x")

	var out bytes.Buffer

	in := strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n")
	if code := srv.Serve(t.Context(), in, &out, &out); code != 0 || out.Len() != 0 {
		t.Fatalf("exit %d, output %q, want silence", code, out.String())
	}
}

func TestUnknownAndBrowserToolsAreRefused(t *testing.T) {
	t.Parallel()

	srv := mcp.New(toolkit.NewRunner(toolkit.Config{BaseURL: "http://127.0.0.1:1", Key: "k"}, nil), "x")

	unknown := rpc(t, srv, "tools/call", map[string]any{"name": "no-such-tool"})
	if e, _ := unknown["error"].(map[string]any); e == nil || e["code"] != float64(-32602) {
		t.Fatalf("unknown tool answered %v, want -32602", unknown)
	}

	res := call(t, srv, "base64", map[string]any{})
	if res["isError"] != true || structured(t, res)["error"].(map[string]any)["code"] != toolkit.CodeBrowserOnly {
		t.Fatalf("a browser tool answered %v, want browser_only", res)
	}
}

func TestImageAreaThroughMCP(t *testing.T) {
	t.Parallel()

	env := toolkittest.Start(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "sprite.svg")

	res := call(t, newServer(env, env.Key), "pixels-to-svg", map[string]any{
		"input": toolkittest.WritePNG(t, dir, "sprite.png"), "output": out, "options": map[string]any{"mode": "rect"},
	})
	if res["isError"] == true {
		t.Fatalf("pixels-to-svg failed: %v", res)
	}

	got := structured(t, res)

	outputs := got["outputs"].([]any)
	if len(outputs) != 1 || got["expires_at"] == nil {
		t.Fatalf("result = %v, want one output and an expiry", got)
	}

	raw, err := os.ReadFile(out)
	if err != nil || !strings.HasPrefix(strings.TrimSpace(string(raw)), "<svg") {
		t.Fatalf("output %q: %v", raw, err)
	}

	if size := outputs[0].(map[string]any)["size_bytes"]; size != float64(len(raw)) {
		t.Fatalf("reported %v bytes, file has %d", size, len(raw))
	}
}

func TestArchiveAreaThroughMCP(t *testing.T) {
	t.Parallel()

	env := toolkittest.Start(t)
	srv := newServer(env, env.Key)
	dir := t.TempDir()
	zip := filepath.Join(dir, "pack.zip")

	made := call(t, srv, "archive-create", map[string]any{
		"inputs": []string{toolkittest.WritePNG(t, dir, "a.png"), toolkittest.WritePNG(t, dir, "b.png")},
		"output": zip, "options": map[string]any{"format": "zip"},
	})
	if made["isError"] == true {
		t.Fatalf("archive-create failed: %v", made)
	}

	dest := filepath.Join(dir, "unpacked")

	extracted := call(t, srv, "archive-extract", map[string]any{"input": zip, "output": dest})
	if extracted["isError"] == true {
		t.Fatalf("archive-extract failed: %v", extracted)
	}

	if n := len(structured(t, extracted)["outputs"].([]any)); n != 2 {
		t.Fatalf("extract gave %d files, want 2", n)
	}

	entries, err := os.ReadDir(dest)
	if err != nil || len(entries) != 2 {
		t.Fatalf("%s holds %d entries (%v), want 2", dest, len(entries), err)
	}
}

func TestPDFAreaThroughMCP(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("qpdf"); err != nil {
		testenv.MissingEngine(t, "qpdf is not installed")
	}

	if runtime.GOOS == "windows" {
		testenv.MissingEngine(t, "the server cannot run engine subprocesses on Windows (prlimit)")
	}

	env := toolkittest.Start(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "cropped.pdf")

	res := call(t, newServer(env, env.Key), "pdf-crop", map[string]any{
		"input": toolkittest.WritePDF(t, dir, "in.pdf", 2), "output": out,
		"options": map[string]any{"left": 36, "bottom": 36, "right": 36, "top": 36},
	})
	if res["isError"] == true {
		t.Fatalf("pdf-crop failed: %v", res)
	}

	head, _ := os.ReadFile(out)
	if !strings.HasPrefix(string(head), "%PDF") {
		t.Fatalf("output starts with %.10q", head)
	}
}

func TestErrorsAreToolResultsWithCodesAndNoKey(t *testing.T) {
	t.Parallel()

	env := toolkittest.Start(t)
	dir := t.TempDir()
	png := toolkittest.WritePNG(t, dir, "a.png")
	secret := "vk_cafe.0123456789-secret"

	cases := []struct {
		name string
		key  string
		args map[string]any
		want string
	}{
		{
			"a bad key",
			secret,
			map[string]any{"input": png, "output": filepath.Join(dir, "o")},
			toolkit.CodeUnauthorized,
		},
		{"no key", "", map[string]any{"input": png, "output": filepath.Join(dir, "o")}, toolkit.CodeMissingKey},
		{"an unknown option", env.Key, map[string]any{
			"input": png, "output": filepath.Join(dir, "o"), "options": map[string]any{"colour": 1},
		}, toolkit.CodeInvalidInput},
		{
			"an unknown argument",
			env.Key,
			map[string]any{"input": png, "output": filepath.Join(dir, "o"), "path": "x"},
			toolkit.CodeInvalidInput,
		},
		{
			"a missing file",
			env.Key,
			map[string]any{"input": filepath.Join(dir, "nope.png"), "output": filepath.Join(dir, "o")},
			toolkit.CodeInvalidInput,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			res := call(t, newServer(env, tc.key), "pixels-to-svg", tc.args)
			if res["isError"] != true {
				t.Fatalf("result = %v, want isError", res)
			}

			if code := structured(t, res)["error"].(map[string]any)["code"]; code != tc.want {
				t.Fatalf("code = %v, want %s: %v", code, tc.want, res)
			}

			text, _ := json.Marshal(res)
			for _, k := range []string{secret, env.Key} {
				if strings.Contains(string(text), k) {
					t.Fatalf("the answer shows the key: %s", text)
				}
			}
		})
	}
}

func TestTheLocalAdapterAnswersOnlyLocalPrograms(t *testing.T) {
	t.Parallel()

	srv := mcp.New(toolkit.NewRunner(toolkit.Config{BaseURL: "http://127.0.0.1:1"}, nil), "x")
	h := srv.Handler()

	do := func(host, origin, contentType string) int {
		req := httptest.NewRequest(http.MethodPost, "http://"+host+"/mcp",
			strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
		req.Host = host

		if origin != "" {
			req.Header.Set("Origin", origin)
		}

		req.Header.Set("Content-Type", contentType)

		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		return rec.Code
	}

	if code := do("127.0.0.1:8787", "", "application/json"); code != http.StatusOK {
		t.Errorf("a local program got %d", code)
	}

	if code := do("127.0.0.1:8787", "https://evil.example", "application/json"); code != http.StatusForbidden {
		t.Errorf("a browser page got %d, want 403", code)
	}

	if code := do("evil.example:8787", "", "application/json"); code != http.StatusForbidden {
		t.Errorf("a rebound host got %d, want 403", code)
	}

	if code := do("127.0.0.1:8787", "", "text/plain"); code != http.StatusUnsupportedMediaType {
		t.Errorf("a non-JSON body got %d, want 415", code)
	}
}
