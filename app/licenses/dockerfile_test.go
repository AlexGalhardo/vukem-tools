package licenses_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/AlexGalhardo/vukem-tools/app/licenses"
)

// engineOf maps what the Dockerfile installs to the component the /licenses
// page must list for it. A package missing here fails the test, so a new
// engine cannot reach the image without a decision about its licence entry.
var engineOf = map[string]string{
	"ffmpeg":              "FFmpeg",
	"libvips-tools":       "libvips",
	"ghostscript":         "Ghostscript",
	"poppler-utils":       "Poppler",
	"qpdf":                "qpdf",
	"libreoffice-writer":  "LibreOffice",
	"libreoffice-calc":    "LibreOffice",
	"libreoffice-impress": "LibreOffice",
	"ocrmypdf":            "OCRmyPDF",
	"tesseract-ocr":       "Tesseract",
	"tesseract-ocr-eng":   "Tesseract",
	"tesseract-ocr-por":   "Tesseract",
	"clamav":              "ClamAV",
	"util-linux":          "util-linux",
	"yt-dlp":              "yt-dlp",
}

// notEngines are installed but run nothing on a user's file: trust roots,
// timezone data, typefaces for rendering, and a library pinned only to carry a
// security fix.
var notEngines = map[string]string{
	"ca-certificates":   "trust roots",
	"tzdata":            "timezone data",
	"fonts-liberation":  "typeface",
	"fonts-dejavu-core": "typeface",
	"liblzma5":          "library pinned for a security fix",
}

var (
	pinned   = regexp.MustCompile(`"([a-z0-9][a-z0-9.+-]*)=\$\{([A-Z0-9_]+)\}"`)
	argValue = regexp.MustCompile(`(?m)^ARG ([A-Z0-9_]+)=(\S+)`)
)

// dockerfile is the production image's recipe, read from the repository root.
func dockerfile(t *testing.T) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("..", "..", "Dockerfile"))
	if err != nil {
		t.Fatalf("read the Dockerfile: %v", err)
	}

	return string(data)
}

// installed is every package of the apt-get install step, pinned or not, plus
// yt-dlp, which the image copies in from its own stage.
func installed(t *testing.T, text string) (packages []string, unpinned []string) {
	t.Helper()

	// The last install step is the runtime image's; the earlier ones belong to
	// build stages that ship nothing.
	index := strings.LastIndex(text, "apt-get install")
	if index < 0 {
		t.Fatal("the Dockerfile has no apt-get install step")
	}

	after := text[index:]

	block, _, _ := strings.Cut(after, "&& rm -rf")

	for line := range strings.SplitSeq(block, "\n") {
		token := strings.Trim(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line), `\`)), `"`)
		if token == "" || strings.HasPrefix(token, "-") || strings.HasPrefix(token, "&&") ||
			strings.HasPrefix(token, "apt-get") {
			continue
		}

		if m := pinned.FindStringSubmatch(`"` + token + `"`); m != nil {
			packages = append(packages, m[1])

			continue
		}

		unpinned = append(unpinned, token)
	}

	if strings.Contains(text, "ARG YTDLP_VERSION=") {
		packages = append(packages, "yt-dlp")
	}

	return packages, unpinned
}

// Every engine in the image is on the licences page. The AGPL and GPL engines
// are the reason: the page is part of complying with them.
func TestEveryEngineInTheImageIsOnTheLicencesPage(t *testing.T) {
	t.Parallel()

	listed := map[string]bool{}
	for _, c := range licenses.Components() {
		listed[c.Name] = true
	}

	text := dockerfile(t)
	packages, unpinned := installed(t, text)

	if len(packages) < 10 {
		t.Fatalf(
			"parsed only %d packages from the Dockerfile; the parser is out of step with it: %v",
			len(packages),
			packages,
		)
	}

	for _, pkg := range packages {
		if _, ok := notEngines[pkg]; ok {
			continue
		}

		name, ok := engineOf[pkg]
		if !ok {
			t.Errorf("the Dockerfile installs %q and this test does not know what it is: map it to a "+
				"/licenses component in engineOf, or to a reason in notEngines", pkg)

			continue
		}

		if !listed[name] {
			t.Errorf("the Dockerfile installs %q but /licenses has no %q entry", pkg, name)
		}
	}

	for _, pkg := range unpinned {
		if _, ok := notEngines[pkg]; !ok {
			t.Errorf("the Dockerfile installs %q without a pinned version", pkg)
		}
	}
}

// Every engine is pinned to an exact version, never latest and never the
// distro default (.claude/rules/engines.md).
func TestEveryEngineInTheImageIsPinned(t *testing.T) {
	t.Parallel()

	text := dockerfile(t)
	args := map[string]string{}

	for _, m := range argValue.FindAllStringSubmatch(text, -1) {
		args[m[1]] = m[2]
	}

	found := 0

	for _, m := range pinned.FindAllStringSubmatch(text, -1) {
		found++

		value, ok := args[m[2]]
		if !ok {
			t.Errorf("%s is pinned to ${%s}, which no ARG defines", m[1], m[2])

			continue
		}

		if value == "" || strings.Contains(strings.ToLower(value), "latest") || strings.ContainsAny(value, "*^<>=") {
			t.Errorf("%s is pinned to %q, which is not an exact version", m[1], value)
		}
	}

	if found == 0 {
		t.Fatal("no pinned package found; the parser is out of step with the Dockerfile")
	}

	if args["YTDLP_VERSION"] == "" || len(args["YTDLP_SHA256"]) != 64 {
		t.Errorf(
			"yt-dlp needs an exact version and a SHA-256 (got %q, %q)",
			args["YTDLP_VERSION"],
			args["YTDLP_SHA256"],
		)
	}

	if strings.Contains(text, ":latest") {
		t.Error("the Dockerfile uses a latest tag")
	}
}
