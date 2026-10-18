package downloader_test

import (
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/AlexGalhardo/vukem-tools/internal/downloader"
	"github.com/AlexGalhardo/vukem-tools/internal/engine"
)

// The test in this file downloads a real video from a real platform.
//
// It is gated behind VUKEM_TOOLS_NETWORK_TESTS=1 and is not part of the
// default suite, for two reasons. It depends on a third party that
// deliberately tries to stop it — docs/web-research.md section 4.3 records
// that a bot check, a PO token or a cookie jar can block any given attempt —
// so in CI it would be a test that fails for reasons nobody can fix. And it
// fetches somebody's copyrighted video, which is not something a test suite
// should do on every run.
//
// It exists because the ROADMAP asks for it, and because it is the only
// assertion that covers the one thing a fake runner cannot: that the
// arguments this package builds actually produce a playable file from a real
// platform.
//
//	VUKEM_TOOLS_NETWORK_TESTS=1 go test -run TestARealDownload -v ./internal/downloader/

// networkTestURL is what gets downloaded. It is overridable, because the
// right fixture is "a short video that is still up", and no single URL stays
// true forever.
const networkTestURL = "https://www.youtube.com/watch?v=aqz-KE-bpKQ"

func TestARealDownloadProducesAPlayableFileWhoseDurationMatchesTheMetadata(t *testing.T) {
	if os.Getenv("VUKEM_TOOLS_NETWORK_TESTS") != "1" {
		t.Skip("set VUKEM_TOOLS_NETWORK_TESTS=1 to download a real video from a real platform")
	}

	ytdlp, err := exec.LookPath("yt-dlp")
	if err != nil {
		t.Skip("yt-dlp is not installed")
	}

	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not installed")
	}

	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe is not installed")
	}

	runner, err := engine.NewRunner(engine.RunnerOptions{Limits: engine.Limits{Timeout: 10 * time.Minute}})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	url := networkTestURL
	if override := os.Getenv("VUKEM_TOOLS_NETWORK_TEST_URL"); override != "" {
		url = override
	}

	d, err := downloader.New(downloader.Options{
		Runner:  runner,
		Binary:  ytdlp,
		FFmpeg:  ffmpeg,
		Retries: 3,
		// Above the best-quality estimate on purpose. YouTube declares no
		// size at all for the formats that matter — SmallestBytes comes back
		// 0 for this video — so EstimateFor falls back to the 4K figure,
		// which is the safe direction and is what the cap is checked
		// against. The file actually fetched at QualitySmallest is a few
		// megabytes.
		MaxBytes: 1 << 30,
	})
	if err != nil {
		t.Fatalf("new downloader: %v", err)
	}

	dir := t.TempDir()

	media, err := d.Metadata(t.Context(), dir, url)
	if err != nil {
		// A platform block is the documented reality, not a defect in this
		// package: it means today's attempt needs a cookie jar or a PO
		// token. Reported rather than failed, so the gate stays useful.
		reportPlatformRefusal(t, err)

		return
	}

	t.Logf("metadata: %q by %q, %.1fs, about %d bytes at best and %d at smallest",
		media.Title, media.Uploader, media.Duration, media.EstimatedBytes, media.SmallestBytes)

	if media.Duration <= 0 {
		t.Fatal("the platform reported no duration, so there is nothing to compare the file against")
	}

	var (
		lastPercent = -1
		updates     int
		sawFinished bool
	)

	result, err := d.Download(t.Context(), downloader.Request{
		URL:      url,
		Dir:      dir,
		Kind:     downloader.KindVideo,
		Quality:  downloader.QualitySmallest,
		Estimate: media.EstimateFor(downloader.QualitySmallest),
		Progress: func(p downloader.Progress) {
			updates++
			lastPercent = p.Percent()
			sawFinished = sawFinished || p.Done()
		},
	})
	if err != nil {
		reportPlatformRefusal(t, err)

		return
	}

	t.Logf("downloaded %s (%d bytes), %d progress updates, last %d%%",
		filepath.Base(result.Path), result.Bytes, updates, lastPercent)

	if result.Bytes == 0 {
		t.Fatal("the download is empty")
	}

	// yt-dlp's --quiet suppresses the progress template unless --progress is
	// also passed, and that failure is silent: the download succeeds and the
	// bar never moves. This is the only test that can catch it, because a
	// fake runner emits whatever lines it is told to.
	if updates == 0 {
		t.Error("not one progress update arrived, so the progress template is not reaching stdout")
	}

	if !sawFinished {
		t.Error("no update reported status=finished, so the stream ended without its last line")
	}

	// Playable, asserted by ffprobe rather than by the file existing: a
	// truncated download is still a file.
	duration, width, height := probeRealFile(t, ffprobe, dir, result.Path)

	t.Logf("the file is %.1fs at %dx%d", duration, width, height)

	if width <= 0 || height <= 0 {
		t.Errorf("the file has no video stream: %dx%d", width, height)
	}

	// The platform's figure is rounded to the second and a container's
	// duration carries its own rounding, so a couple of seconds of
	// difference is the format, not a wrong file. More than that means
	// something else was downloaded.
	if diff := math.Abs(duration - media.Duration); diff > 3 {
		t.Errorf("the file is %.1fs and the metadata said %.1fs, off by %.1fs", duration, media.Duration, diff)
	}
}

// reportPlatformRefusal fails or skips depending on whose fault it is.
//
// A platform block is the platform's; anything else is this package's, and
// has to fail.
func reportPlatformRefusal(t *testing.T, err error) {
	t.Helper()

	failure := downloader.Classify(err)
	if failure == nil {
		t.Fatalf("no error and no result: %v", err)
	}

	switch failure.Reason {
	case downloader.ReasonPlatformBlock, downloader.ReasonAgeRestricted, downloader.ReasonUnavailable:
		t.Skipf("the platform refused this attempt (%s): %s — see docs/web-research.md section 4.3",
			failure.Reason, failure.Detail)
	default:
		t.Fatalf("the download failed for a reason this package owns (%s): %s", failure.Reason, failure.Detail)
	}
}

// probeRealFile reads a file's duration and frame size with the real ffprobe.
func probeRealFile(t *testing.T, ffprobe, dir, path string) (duration float64, width, height int) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), ffprobe,
		"-v", "error", "-print_format", "json", "-show_format", "-show_streams", "-i", path)
	cmd.Dir = dir

	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("ffprobe refused the downloaded file, so it is not playable: %v", err)
	}

	var doc struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			CodecType string `json:"codec_type"`
			Width     int    `json:"width"`
			Height    int    `json:"height"`
		} `json:"streams"`
	}

	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("decode ffprobe's answer: %v", err)
	}

	duration, _ = strconv.ParseFloat(strings.TrimSpace(doc.Format.Duration), 64)

	for _, s := range doc.Streams {
		if s.CodecType == "video" {
			return duration, s.Width, s.Height
		}
	}

	return duration, 0, 0
}
