package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/AlexGalhardo/vukem-tools/app/catalog"
	"github.com/AlexGalhardo/vukem-tools/app/tools"
	"github.com/AlexGalhardo/vukem-tools/internal/cli"
	"github.com/AlexGalhardo/vukem-tools/internal/mcp"
	"github.com/AlexGalhardo/vukem-tools/internal/pdf"
	"github.com/AlexGalhardo/vukem-tools/internal/testenv"
)

func TestToolsListsTheRegisteredCatalog(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	if code := cli.Run(context.Background(), []string{"tools"}, &stdout, &stderr); code != 0 {
		t.Fatalf("tools exit %d: %s", code, stderr.String())
	}

	var body struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &body); err != nil {
		t.Fatalf("decode tools: %v\n%s", err, stdout.String())
	}

	registered := tools.All(nil, tools.Engines{}, nil)
	if len(body.Tools) != len(registered) {
		t.Fatalf("CLI listed %d tools, the pipeline registers %d", len(body.Tools), len(registered))
	}

	seen := map[string]bool{}
	for _, tool := range body.Tools {
		seen[tool.Name] = true
		entry, ok := catalog.Find(tool.Name)
		if !ok || entry.Client {
			t.Errorf("tool %s is not a user-facing conversion in the catalog", tool.Name)
		}
	}

	if !seen["pdf-crop"] {
		t.Fatal("pdf-crop is missing from the CLI catalog")
	}

	for _, tool := range registered {
		if !seen[tool.Name()] {
			t.Errorf("CLI omitted registered tool %s", tool.Name())
		}
	}
}

func TestCropEveryPageOnCLIHTTPAndMCP(t *testing.T) {
	t.Parallel()

	if os.Getenv("QPDF_PATH") == "" {
		if _, err := exec.LookPath("qpdf"); err != nil {
			testenv.MissingEngine(t, "qpdf is not installed")
		}
	}

	dir := t.TempDir()
	input := writeFixture(t, dir, 2)

	cliOut := filepath.Join(dir, "cli.pdf")
	httpOut := filepath.Join(dir, "http.pdf")
	mcpOut := filepath.Join(dir, "mcp.pdf")

	cropCLI(t, input, cliOut)
	cropHTTP(t, input, httpOut)
	cropMCP(t, input, mcpOut)

	for _, path := range []string{cliOut, httpOut, mcpOut} {
		assertPDF(t, path)
	}
}

func cropCLI(t *testing.T, input, output string) {
	t.Helper()

	var stdout, stderr bytes.Buffer
	code := cli.Run(context.Background(), []string{
		"pdf", "crop", "--input", input, "--output", output,
		"--left", "36", "--bottom", "36", "--right", "36", "--top", "36",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("cli crop exit %d: %s", code, stderr.String())
	}

	assertCroppedPages(t, stdout.Bytes(), 2)
}

func cropHTTP(t *testing.T, input, output string) {
	t.Helper()

	body := `{"input":` + strconv.Quote(input) + `,"output":` + strconv.Quote(output) +
		`,"left":36,"bottom":36,"right":36,"top":36}`
	req := httptest.NewRequest(http.MethodPost, "/pdf/crop", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mcp.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("HTTP crop status %d: %s", rec.Code, rec.Body.String())
	}

	assertCroppedPages(t, rec.Body.Bytes(), 2)
}

func cropMCP(t *testing.T, input, output string) {
	t.Helper()

	params, err := json.Marshal(map[string]any{
		"name": "pdf-crop",
		"arguments": map[string]any{
			"input": input, "output": output,
			"left": 36, "bottom": 36, "right": 36, "top": 36,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	var stdin bytes.Buffer
	req, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params":  json.RawMessage(params),
	})
	if err != nil {
		t.Fatal(err)
	}

	stdin.Write(req)
	stdin.WriteByte('\n')

	var stdout, stderr bytes.Buffer
	if code := mcp.Serve(context.Background(), &stdin, &stdout, &stderr); code != 0 {
		t.Fatalf("mcp serve exit %d: %s", code, stderr.String())
	}

	var resp struct {
		Result struct {
			Cropped int `json:"cropped"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		t.Fatalf("decode mcp: %v\n%s", err, stdout.String())
	}

	if resp.Error != nil {
		t.Fatalf("mcp error: %s", resp.Error.Message)
	}

	if resp.Result.Cropped != 2 {
		t.Fatalf("mcp cropped %d pages, want 2", resp.Result.Cropped)
	}
}

func assertCroppedPages(t *testing.T, raw []byte, want int) {
	t.Helper()

	var outcome struct {
		Cropped int `json:"cropped"`
	}
	if err := json.Unmarshal(raw, &outcome); err != nil {
		t.Fatalf("decode crop outcome: %v\n%s", err, raw)
	}

	if outcome.Cropped != want {
		t.Fatalf("cropped %d pages, want %d", outcome.Cropped, want)
	}
}

func assertPDF(t *testing.T, path string) {
	t.Helper()

	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}

	if info.Size() == 0 {
		t.Fatalf("%s is empty", path)
	}

	magic := make([]byte, 4)
	if _, err := file.Read(magic); err != nil {
		t.Fatal(err)
	}

	if string(magic) != "%PDF" {
		t.Fatalf("%s starts with %q, want %%PDF", path, magic)
	}
}

func writeFixture(t *testing.T, dir string, pages int) string {
	t.Helper()

	a4, ok := pdf.PageSize("a4")
	if !ok {
		t.Fatal("a4 page size missing")
	}

	specs := make([]pdf.PageSpec, 0, pages)
	for i := 1; i <= pages; i++ {
		specs = append(specs, pdf.PageSpec{
			Box: a4,
			Text: []pdf.TextStamp{{
				Text: "Fixture page " + strconv.Itoa(i),
				Font: pdf.Helvetica,
				Size: 24,
				X:    72,
				Y:    720,
			}},
		})
	}

	path := filepath.Join(dir, "in.pdf")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := pdf.Write(file, specs); err != nil {
		file.Close()
		t.Fatal(err)
	}

	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	return path
}
