package tests

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/AlexGalhardo/vukem/vukemtest"

	"github.com/AlexGalhardo/vukem-tools/bootstrap"
)

// The page used to hand Inertia the domain struct. That struct has no JSON
// tags, so the browser received Label and Extractors and the notice read
// ", , , ." instead of the four platform names.
func TestVideoDownloadPageNamesThePlatforms(t *testing.T) {
	t.Parallel()

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
