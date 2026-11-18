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
// The page is registered only when yt-dlp resolves. The test app reads a
// config map, not the process environment, and CI does not install yt-dlp.
func TestVideoDownloadPageNamesThePlatforms(t *testing.T) {
	app := vukemtest.New(t, bootstrap.New, vukemtest.Options{
		Env: map[string]string{"YTDLP_PATH": stubYtDlp(t)},
	})
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

// stubYtDlp returns a program that only prints a version. Pass it as
// YTDLP_PATH in vukemtest.Options.Env: a path left only in the process
// environment is invisible to the test app, the tool stays unregistered
// and the request is redirected home.
func stubYtDlp(t *testing.T) string {
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

	return path
}
