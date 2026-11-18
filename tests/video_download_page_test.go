package tests

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/AlexGalhardo/vukem/vukemtest"

	"github.com/AlexGalhardo/vukem-tools/bootstrap"
)

// The page used to hand Inertia the domain struct. That struct has no JSON
// tags, so the browser received Label and Extractors and the notice read
// ", , , ." instead of the four platform names.
//
// The page is registered only when yt-dlp resolves. CI does not install it,
// and t.Setenv cannot run beside t.Parallel.
func TestVideoDownloadPageNamesThePlatforms(t *testing.T) {
	stubYtDlp(t)

	app := vukemtest.New(t, bootstrap.New)
	client := app.Client(t).Inertia("")
	register(t, app, client, "vt-002-platforms@example.com")

	page := client.Get("/tools/video-download").
		AssertOK().AssertInertia("Tools/VideoDownload").
		PropEquals("platforms.0.label", "YouTube").
		PropEquals("platforms.0.name", "youtube").
		PropEquals("platforms.1.label", "Instagram").
		PropEquals("platforms.2.label", "X").
		PropEquals("platforms.3.label", "TikTok")

	raw, ok := page.Prop("platforms")
	if !ok {
		t.Fatal("platforms prop missing")
	}

	encoded, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}

	if bytes.Contains(bytes.ToLower(encoded), []byte(`"extractors"`)) {
		t.Fatalf("platforms prop leaks yt-dlp extractor patterns: %s", encoded)
	}
}

// stubYtDlp points YTDLP_PATH at a program that only prints a version.
// The download page stays unregistered without one, and the request is
// redirected home.
func stubYtDlp(t *testing.T) {
	t.Helper()

	dir := t.TempDir()
	name := "yt-dlp"
	body := "#!/bin/sh\necho 2026.08.19\n"

	if runtime.GOOS == "windows" {
		name = "yt-dlp.bat"
		body = "@echo 2026.08.19\r\n"
	}

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("YTDLP_PATH", path)
}
