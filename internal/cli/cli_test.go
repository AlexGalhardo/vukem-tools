package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/AlexGalhardo/vukem-tools/internal/cli"
	"github.com/AlexGalhardo/vukem-tools/internal/mcp"
	"github.com/AlexGalhardo/vukem-tools/internal/testenv"
	"github.com/AlexGalhardo/vukem-tools/internal/toolkit/toolkittest"
)

type run struct {
	code           int
	stdout, stderr string
}

func execute(t *testing.T, env map[string]string, args ...string) run {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()

	var out, errOut bytes.Buffer

	code := cli.Execute(ctx, args, &out, &errOut, cli.Options{
		Getenv:  func(k string) string { return env[k] },
		Version: "1.2.3",
		Poll:    50 * time.Millisecond,
	})

	return run{code, out.String(), errOut.String()}
}

func TestVersionIsOneBareLine(t *testing.T) {
	t.Parallel()

	secrets := map[string]string{"VUKEM_TOOLS_API_KEY": "vk_x.secret", "VUKEM_TOOLS_URL": "http://example.test"}

	for _, arg := range []string{"version", "--version"} {
		r := execute(t, secrets, arg)
		if r.code != 0 || r.stdout != "1.2.3\n" || r.stderr != "" {
			t.Errorf("%s: exit %d, stdout %q, stderr %q, want exactly 1.2.3", arg, r.code, r.stdout, r.stderr)
		}
	}
}

func TestToolsAndMCPListTheSameNames(t *testing.T) {
	t.Parallel()

	r := execute(t, nil, "tools")
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}

	var listed struct {
		Tools []struct {
			Name string `json:"name"`
			Kind string `json:"kind"`
		} `json:"tools"`
	}

	if err := json.Unmarshal([]byte(r.stdout), &listed); err != nil {
		t.Fatal(err)
	}

	var cliNames, mcpNames []string

	for _, tl := range listed.Tools {
		cliNames = append(cliNames, tl.Name)
	}

	for _, tl := range mcp.Tools() {
		mcpNames = append(mcpNames, tl.Name)
	}

	slices.Sort(cliNames)
	slices.Sort(mcpNames)

	if len(cliNames) != 54 || !slices.Equal(cliNames, mcpNames) {
		t.Fatalf("cli lists %d names, mcp %d; want the same 54", len(cliNames), len(mcpNames))
	}
}

func TestEveryPipelineToolIsReachableAsAreaAndTool(t *testing.T) {
	t.Parallel()

	var listed struct {
		Tools []struct {
			Name    string `json:"name"`
			Kind    string `json:"kind"`
			Area    string `json:"area"`
			Command string `json:"command"`
		} `json:"tools"`
	}

	if err := json.Unmarshal([]byte(execute(t, nil, "tools").stdout), &listed); err != nil {
		t.Fatal(err)
	}

	pipeline := 0

	for _, tl := range listed.Tools {
		if tl.Kind != "pipeline" {
			continue
		}

		pipeline++

		// --help runs no call, so it proves the subcommand is wired.
		r := execute(t, nil, tl.Area, tl.Command, "--help")
		if r.code != 0 || !strings.Contains(r.stdout, "-output") {
			t.Errorf("%s %s --help: exit %d, %q", tl.Area, tl.Command, r.code, r.stderr)
		}

		if again := execute(t, nil, "run", tl.Name, "--help"); again.stdout != r.stdout {
			t.Errorf("run %s and %s %s differ", tl.Name, tl.Area, tl.Command)
		}
	}

	if pipeline != 37 {
		t.Fatalf("%d pipeline tools reachable, want 37", pipeline)
	}
}

func TestOptionsBecomeFlags(t *testing.T) {
	t.Parallel()

	help := execute(t, nil, "pdf", "crop", "--help").stdout
	for _, want := range []string{"-left", "-bottom", "-pages", "-input", "-timeout"} {
		if !strings.Contains(help, want) {
			t.Errorf("pdf crop --help lacks %s:\n%s", want, help)
		}
	}
}

func TestUsageErrors(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		nil,
		{"frobnicate"},
		{"pdf", "nope"},
		{"run", "nope"},
		{"image", "convert", "--input", "a.png", "--output", "o", "--colour", "red"},
		{"image", "convert", "--input", "a.png", "--output", "o", "stray"},
	} {
		r := execute(t, map[string]string{"VUKEM_TOOLS_API_KEY": "k"}, args...)
		if r.code != 2 || r.stderr == "" {
			t.Errorf("%v: exit %d, stderr %q, want exit 2 and a message", args, r.code, r.stderr)
		}
	}
}

func TestImageAreaThroughTheCLI(t *testing.T) {
	t.Parallel()

	srv := toolkittest.Start(t)
	env := map[string]string{"VUKEM_TOOLS_URL": srv.URL, "VUKEM_TOOLS_API_KEY": srv.Key}
	dir := t.TempDir()
	out := filepath.Join(dir, "sprite.svg")

	r := execute(t, env, "image", "to-svg", "--input", toolkittest.WritePNG(t, dir, "sprite.png"),
		"--output", out, "--mode", "rect", "--json")
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}

	var res struct {
		Status  string `json:"status"`
		Outputs []struct {
			Path      string `json:"path"`
			SizeBytes int64  `json:"size_bytes"`
		} `json:"outputs"`
		ExpiresAt string `json:"expires_at"`
	}

	if err := json.Unmarshal([]byte(r.stdout), &res); err != nil {
		t.Fatalf("--json printed %q: %v", r.stdout, err)
	}

	raw, err := os.ReadFile(out)
	if err != nil || !strings.HasPrefix(strings.TrimSpace(string(raw)), "<svg") ||
		!strings.Contains(string(raw), "<rect") {
		t.Fatalf("output %.60q: %v", raw, err)
	}

	if res.Status != "finished" || len(res.Outputs) != 1 || res.Outputs[0].SizeBytes != int64(len(raw)) ||
		res.ExpiresAt == "" {
		t.Fatalf("result = %+v", res)
	}

	plain := execute(t, env, "image", "to-svg", "--input", filepath.Join(dir, "sprite.png"), "--output", out)
	if plain.code != 0 || !strings.Contains(plain.stdout, out) {
		t.Fatalf("plain output = %q (exit %d)", plain.stdout, plain.code)
	}
}

func TestArchiveAreaThroughTheCLI(t *testing.T) {
	t.Parallel()

	srv := toolkittest.Start(t)
	env := map[string]string{"VUKEM_TOOLS_URL": srv.URL, "VUKEM_TOOLS_API_KEY": srv.Key}
	dir := t.TempDir()
	zip := filepath.Join(dir, "pack.zip")

	if r := execute(t, env, "archive", "create", "--input", toolkittest.WritePNG(t, dir, "a.png"),
		"--input", toolkittest.WritePNG(t, dir, "b.png"), "--output", zip, "--format", "zip"); r.code != 0 {
		t.Fatalf("create: exit %d: %s", r.code, r.stderr)
	}

	dest := filepath.Join(dir, "out")
	if r := execute(t, env, "archive", "extract", "--input", zip, "--output", dest); r.code != 0 {
		t.Fatalf("extract: exit %d: %s", r.code, r.stderr)
	}

	if entries, err := os.ReadDir(dest); err != nil || len(entries) != 2 {
		t.Fatalf("%s holds %d entries (%v), want 2", dest, len(entries), err)
	}
}

func TestPDFAreaThroughTheCLI(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("qpdf"); err != nil {
		testenv.MissingEngine(t, "qpdf is not installed")
	}

	if runtime.GOOS == "windows" {
		testenv.MissingEngine(t, "the server cannot run engine subprocesses on Windows (prlimit)")
	}

	srv := toolkittest.Start(t)
	env := map[string]string{"VUKEM_TOOLS_URL": srv.URL, "VUKEM_TOOLS_API_KEY": srv.Key}
	dir := t.TempDir()
	out := filepath.Join(dir, "cropped.pdf")

	r := execute(t, env, "pdf", "crop", "--input", toolkittest.WritePDF(t, dir, "in.pdf", 2), "--output", out,
		"--left", "36", "--bottom", "36", "--right", "36", "--top", "36")
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}

	if head, _ := os.ReadFile(out); !strings.HasPrefix(string(head), "%PDF") {
		t.Fatalf("output starts with %.10q", head)
	}
}

func TestFailuresPrintAStableCodeAndNeverTheKey(t *testing.T) {
	t.Parallel()

	srv := toolkittest.Start(t)
	dir := t.TempDir()
	png := toolkittest.WritePNG(t, dir, "a.png")
	secret := "vk_cafe.0123456789-secret"

	cases := []struct {
		name string
		key  string
		want string
	}{
		{"a bad key", secret, "unauthorized"},
		{"no key", "", "missing_key"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			env := map[string]string{"VUKEM_TOOLS_URL": srv.URL, "VUKEM_TOOLS_API_KEY": tc.key}
			r := execute(t, env, "image", "to-svg", "--input", png, "--output", filepath.Join(dir, "o.svg"), "--json")

			if r.code != 1 {
				t.Fatalf("exit %d, want 1", r.code)
			}

			var e struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}

			if err := json.Unmarshal([]byte(r.stderr), &e); err != nil || e.Error.Code != tc.want {
				t.Fatalf("stderr = %q (%v), want code %s", r.stderr, err, tc.want)
			}

			if r.stdout != "" || strings.Contains(r.stderr+r.stdout, secret) || strings.Contains(r.stderr, srv.Key) {
				t.Fatalf("stdout %q / stderr %q leak or mix output", r.stdout, r.stderr)
			}
		})
	}
}
