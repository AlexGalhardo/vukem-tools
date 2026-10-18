package downloader_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/AlexGalhardo/vukem-tools/internal/downloader"
	"github.com/AlexGalhardo/vukem-tools/internal/engine"
)

// fakeRunner stands in for engine.Runner. The real one is tested in
// internal/engine; what matters here is the arguments this package builds,
// the output it reads back and the way it classifies a fetchErr, none of which
// needs a subprocess or a network.
type fakeRunner struct {
	// calls records every command, so a test can assert on what yt-dlp was
	// actually told.
	calls []engine.Command
	// stdout is written to the command's Stdout, for the metadata probe.
	stdout string
	// lines are delivered to the command's Progress handler.
	lines []string
	// files are created in the command's Dir before it returns, which is how
	// a fake download produces a result.
	files map[string]int
	// err is returned instead of success.
	err error
	// cpu is reported back.
	cpu time.Duration
}

func (f *fakeRunner) Run(_ context.Context, cmd engine.Command) (engine.Result, error) {
	f.calls = append(f.calls, cmd)

	if cmd.Stdout != nil && f.stdout != "" {
		if _, err := cmd.Stdout.Write([]byte(f.stdout)); err != nil {
			return engine.Result{}, err
		}
	}

	if cmd.Progress != nil {
		for _, line := range f.lines {
			cmd.Progress(line)
		}
	}

	for name, size := range f.files {
		if err := os.WriteFile(filepath.Join(cmd.Dir, name), make([]byte, size), 0o600); err != nil {
			return engine.Result{}, err
		}
	}

	if f.err != nil {
		return engine.Result{}, f.err
	}

	return engine.Result{CPUTime: f.cpu}, nil
}

// last returns the arguments of the most recent call.
func (f *fakeRunner) last(t *testing.T) []string {
	t.Helper()

	if len(f.calls) == 0 {
		t.Fatal("the runner was never called")
	}

	return f.calls[len(f.calls)-1].Args
}

// newDownloader builds a downloader on a fake runner.
func newDownloader(t *testing.T, r downloader.Runner, o downloader.Options) *downloader.Downloader {
	t.Helper()

	o.Runner = r

	if o.Binary == "" {
		o.Binary = "/usr/local/bin/yt-dlp"
	}

	d, err := downloader.New(o)
	if err != nil {
		t.Fatalf("new downloader: %v", err)
	}

	return d
}

// argValue returns the value that follows flag, and whether it was present.
func argValue(args []string, flag string) (string, bool) {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1], true
		}
	}

	return "", false
}

func TestPlatformDetection(t *testing.T) {
	t.Parallel()

	cases := []struct {
		url  string
		want string
	}{
		{"https://www.youtube.com/watch?v=abc", "youtube"},
		{"https://youtu.be/abc", "youtube"},
		{"https://m.youtube.com/watch?v=abc", "youtube"},
		{"https://music.youtube.com/watch?v=abc", "youtube"},
		{"https://www.instagram.com/reel/abc/", "instagram"},
		{"https://x.com/someone/status/1", "x"},
		{"https://twitter.com/someone/status/1", "x"},
		{"https://www.tiktok.com/@someone/video/1", "tiktok"},
		{"https://vm.tiktok.com/abc/", "tiktok"},
	}

	for _, c := range cases {
		platform, err := downloader.PlatformOf(c.url)
		if err != nil {
			t.Errorf("%s: %v", c.url, err)
			continue
		}

		if platform.Name != c.want {
			t.Errorf("%s: platform %q, want %q", c.url, platform.Name, c.want)
		}
	}
}

func TestAnUnsupportedURLIsRefused(t *testing.T) {
	t.Parallel()

	cases := []string{
		"https://vimeo.com/1",
		// A lookalike host: the suffix match must require the dot, or this
		// would be read as YouTube.
		"https://youtube.com.attacker.example/watch?v=abc",
		// Not http, so yt-dlp never sees it.
		"file:///etc/passwd",
		"ftp://example.com/video.mp4",
		// No host at all.
		"https://",
		"not a url at all",
		// An internal address, which is the proxy case the platform list
		// exists to prevent.
		"http://169.254.169.254/latest/meta-data/",
		"http://localhost:3000/admin",
	}

	for _, raw := range cases {
		if _, err := downloader.PlatformOf(raw); !errors.Is(err, downloader.ErrUnsupportedURL) {
			t.Errorf("%s: error %v, want ErrUnsupportedURL", raw, err)
		}

		if downloader.Supported(raw) {
			t.Errorf("%s: reported as supported", raw)
		}
	}
}

func TestTheGenericExtractorIsNeverEnabled(t *testing.T) {
	t.Parallel()

	for _, platform := range downloader.Platforms() {
		if len(platform.Extractors) == 0 {
			t.Errorf("%s: no extractor set, which would let yt-dlp pick any extractor", platform.Name)
		}

		for _, pattern := range platform.Extractors {
			if strings.Contains(pattern, "generic") || pattern == "all" || pattern == "default" {
				t.Errorf("%s: extractor pattern %q can enable the generic extractor", platform.Name, pattern)
			}
		}
	}
}

func TestEveryInvocationRestrictsTheExtractorsAndIgnoresHostConfig(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{stdout: `{"title":"t","duration":1}`}
	d := newDownloader(t, runner, downloader.Options{})

	if _, err := d.Metadata(t.Context(), t.TempDir(), "https://youtu.be/abc"); err != nil {
		t.Fatalf("metadata: %v", err)
	}

	args := runner.last(t)

	if !slices.Contains(args, "--ignore-config") {
		t.Error("the host's yt-dlp configuration is not ignored")
	}

	if !slices.Contains(args, "--no-playlist") {
		t.Error("a playlist URL would download the whole playlist")
	}

	extractors, ok := argValue(args, "--use-extractors")
	if !ok {
		t.Fatalf("--use-extractors is absent from %v", args)
	}

	if extractors != "youtube.*" {
		t.Errorf("--use-extractors %q, want youtube.*", extractors)
	}
}

func TestMetadataReportsWhatThePlatformSaid(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{stdout: `{
		"title": "A video",
		"uploader": "Someone",
		"thumbnail": "https://example.com/t.jpg",
		"extractor_key": "Youtube",
		"duration": 125.5,
		"filesize_approx": 10485760,
		"formats": [
			{"format_id":"18","ext":"mp4","resolution":"640x360","vcodec":"avc1","acodec":"mp4a","filesize":5242880,"tbr":500},
			{"format_id":"22","ext":"mp4","resolution":"1280x720","vcodec":"avc1","acodec":"mp4a","filesize_approx":10485760,"tbr":1200}
		]
	}`}

	d := newDownloader(t, runner, downloader.Options{})

	media, err := d.Metadata(t.Context(), t.TempDir(), "https://youtu.be/abc")
	if err != nil {
		t.Fatalf("metadata: %v", err)
	}

	if media.Title != "A video" || media.Uploader != "Someone" {
		t.Errorf("title %q uploader %q", media.Title, media.Uploader)
	}

	if media.Platform != "youtube" || media.Extractor != "Youtube" {
		t.Errorf("platform %q extractor %q", media.Platform, media.Extractor)
	}

	if media.Duration != 125.5 {
		t.Errorf("duration %v, want 125.5", media.Duration)
	}

	if media.Thumbnail == "" {
		t.Error("no thumbnail")
	}

	if media.EstimatedBytes != 10485760 {
		t.Errorf("estimate %d, want 10485760", media.EstimatedBytes)
	}

	if len(media.Formats) != 2 {
		t.Fatalf("%d formats, want 2", len(media.Formats))
	}

	if media.Formats[1].Bytes != 10485760 || media.Formats[1].Resolution != "1280x720" {
		t.Errorf("second format %+v", media.Formats[1])
	}
}

func TestTheEstimateFallsBackToDurationTimesBitrate(t *testing.T) {
	t.Parallel()

	// YouTube reports no size at all for the adaptive formats that give the
	// best quality, so without the arithmetic fallback the size cap would
	// never fire before a download.
	runner := &fakeRunner{stdout: `{
		"title": "No sizes anywhere",
		"duration": 100,
		"tbr": 800,
		"formats": [{"format_id":"137","ext":"mp4","tbr":800}]
	}`}

	d := newDownloader(t, runner, downloader.Options{})

	media, err := d.Metadata(t.Context(), t.TempDir(), "https://youtu.be/abc")
	if err != nil {
		t.Fatalf("metadata: %v", err)
	}

	// 800 kbit/s for 100 s is 80000 kbit, and a kilobit is 125 bytes.
	if want := int64(800 * 100 * 125); media.EstimatedBytes != want {
		t.Errorf("estimate %d, want %d", media.EstimatedBytes, want)
	}
}

func TestALiveStreamIsRefused(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{stdout: `{"title":"live now","is_live":true}`}
	d := newDownloader(t, runner, downloader.Options{})

	_, err := d.Metadata(t.Context(), t.TempDir(), "https://youtu.be/abc")

	var fetchErr *downloader.FetchError
	if !errors.As(err, &fetchErr) || fetchErr.Reason != downloader.ReasonUnsupportedURL {
		t.Fatalf("error %v, want an unsupported_url FetchError", err)
	}
}

func TestTheSizeCapIsEnforcedFromTheEstimateBeforeAnythingIsFetched(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{}
	d := newDownloader(t, runner, downloader.Options{MaxBytes: 1 << 20})

	_, err := d.Download(t.Context(), downloader.Request{
		URL:      "https://youtu.be/abc",
		Dir:      t.TempDir(),
		Kind:     downloader.KindVideo,
		Estimate: 10 << 20,
	})

	var fetchErr *downloader.FetchError
	if !errors.As(err, &fetchErr) || fetchErr.Reason != downloader.ReasonTooLarge {
		t.Fatalf("error %v, want a too_large FetchError", err)
	}

	if len(runner.calls) != 0 {
		t.Fatalf("yt-dlp was started anyway: %v", runner.calls)
	}

	if !strings.Contains(fetchErr.Public, "10.0 MB") || !strings.Contains(fetchErr.Public, "1.0 MB") {
		t.Errorf("the refusal does not name both sizes: %q", fetchErr.Public)
	}
}

func TestTheSizeCapIsEnforcedAgainstTheFinishedFile(t *testing.T) {
	t.Parallel()

	// The estimate was within the cap and the real file is not, which is what
	// happens whenever the platform declares no size.
	runner := &fakeRunner{files: map[string]int{"download.mp4": 4 << 20}}
	d := newDownloader(t, runner, downloader.Options{MaxBytes: 1 << 20})

	_, err := d.Download(t.Context(), downloader.Request{
		URL:  "https://youtu.be/abc",
		Dir:  t.TempDir(),
		Kind: downloader.KindVideo,
	})

	var fetchErr *downloader.FetchError
	if !errors.As(err, &fetchErr) || fetchErr.Reason != downloader.ReasonTooLarge {
		t.Fatalf("error %v, want a too_large FetchError", err)
	}

	// The cap is also handed to yt-dlp, so a declared size is refused before
	// the bytes rather than after.
	if got, ok := argValue(runner.last(t), "--max-filesize"); !ok || got != "1048576" {
		t.Errorf("--max-filesize %q present=%v", got, ok)
	}
}

func TestADownloadReturnsTheFileItProduced(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{
		files: map[string]int{
			"download.mp4":      2048,
			"download.mp4.part": 4096, // a partial file must never win
			"download.webp":     64,   // nor a thumbnail
		},
		cpu: 1500 * time.Millisecond,
	}

	d := newDownloader(t, runner, downloader.Options{FFmpeg: "/usr/bin/ffmpeg"})

	result, err := d.Download(t.Context(), downloader.Request{
		URL:     "https://youtu.be/abc",
		Dir:     t.TempDir(),
		Kind:    downloader.KindVideo,
		Quality: downloader.QualityBest,
	})
	if err != nil {
		t.Fatalf("download: %v", err)
	}

	if filepath.Base(result.Path) != "download.mp4" {
		t.Errorf("path %q, want the muxed file", result.Path)
	}

	if result.Bytes != 2048 {
		t.Errorf("bytes %d, want 2048", result.Bytes)
	}

	if result.CPUTime != 1500*time.Millisecond {
		t.Errorf("cpu %s", result.CPUTime)
	}
}

func TestADownloadThatWroteNothingIsAFailureRatherThanAnEmptyResult(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{files: map[string]int{"download.mp4.part": 512}}
	d := newDownloader(t, runner, downloader.Options{})

	_, err := d.Download(t.Context(), downloader.Request{
		URL:  "https://youtu.be/abc",
		Dir:  t.TempDir(),
		Kind: downloader.KindVideo,
	})

	var fetchErr *downloader.FetchError
	if !errors.As(err, &fetchErr) || fetchErr.Reason != downloader.ReasonEngine {
		t.Fatalf("error %v, want an engine FetchError", err)
	}
}

func TestAudioAsksForMP3AndVideoAsksForAMuxedMP4(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{files: map[string]int{"download.mp3": 128}}
	d := newDownloader(t, runner, downloader.Options{FFmpeg: "/usr/bin/ffmpeg"})

	if _, err := d.Download(t.Context(), downloader.Request{
		URL:  "https://youtu.be/abc",
		Dir:  t.TempDir(),
		Kind: downloader.KindAudio,
	}); err != nil {
		t.Fatalf("download: %v", err)
	}

	args := runner.last(t)

	if !slices.Contains(args, "--extract-audio") {
		t.Errorf("audio did not ask for extraction: %v", args)
	}

	if got, _ := argValue(args, "--audio-format"); got != "mp3" {
		t.Errorf("--audio-format %q, want mp3", got)
	}

	if got, _ := argValue(args, "--ffmpeg-location"); got != "/usr/bin/ffmpeg" {
		t.Errorf("--ffmpeg-location %q", got)
	}

	runner.files = map[string]int{"download.mp4": 256}

	if _, err := d.Download(t.Context(), downloader.Request{
		URL:     "https://youtu.be/abc",
		Dir:     t.TempDir(),
		Kind:    downloader.KindVideo,
		Quality: downloader.QualitySmallest,
	}); err != nil {
		t.Fatalf("download: %v", err)
	}

	args = runner.last(t)

	if got, _ := argValue(args, "--merge-output-format"); got != "mp4" {
		t.Errorf("--merge-output-format %q, want mp4", got)
	}

	format, _ := argValue(args, "-f")
	if !strings.HasPrefix(format, "wv*") {
		t.Errorf("smallest asked for %q, want the worst-quality selector", format)
	}
}

func TestWithoutFFmpegOnlyPreMuxedFormatsAreAsked(t *testing.T) {
	t.Parallel()

	// A merge needs ffmpeg. Asking for bestvideo+bestaudio without it fails
	// every download, so the selector has to fall back instead.
	runner := &fakeRunner{files: map[string]int{"download.mp4": 256}}
	d := newDownloader(t, runner, downloader.Options{})

	if _, err := d.Download(t.Context(), downloader.Request{
		URL:  "https://youtu.be/abc",
		Dir:  t.TempDir(),
		Kind: downloader.KindVideo,
	}); err != nil {
		t.Fatalf("download: %v", err)
	}

	args := runner.last(t)

	if slices.Contains(args, "--merge-output-format") {
		t.Errorf("a merge was requested without ffmpeg: %v", args)
	}

	if format, _ := argValue(args, "-f"); strings.Contains(format, "+") {
		t.Errorf("selector %q needs a merge", format)
	}
}

func TestTheSleepRetryCookieAndPOTokenSettingsReachTheEngine(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	jar := filepath.Join(dir, "cookies.txt")

	if err := os.WriteFile(jar, []byte("# Netscape HTTP Cookie File\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	runner := &fakeRunner{stdout: `{"title":"t"}`}
	d := newDownloader(t, runner, downloader.Options{
		CookieFile:       jar,
		POTokenBaseURL:   "http://pot:4416",
		SleepRequests:    1500 * time.Millisecond,
		SleepInterval:    2 * time.Second,
		MaxSleepInterval: 5 * time.Second,
		Retries:          4,
	})

	if _, err := d.Metadata(t.Context(), dir, "https://youtu.be/abc"); err != nil {
		t.Fatalf("metadata: %v", err)
	}

	args := runner.last(t)

	for flag, want := range map[string]string{
		"--cookies":            jar,
		"--extractor-args":     "youtube:getpot_bgutil_baseurl=http://pot:4416",
		"--sleep-requests":     "1.5",
		"--sleep-interval":     "2",
		"--max-sleep-interval": "5",
		"--retries":            "4",
		"--fragment-retries":   "4",
	} {
		if got, ok := argValue(args, flag); !ok || got != want {
			t.Errorf("%s %q present=%v, want %q", flag, got, ok, want)
		}
	}
}

func TestAMissingCookieFileDegradesRatherThanFailing(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{stdout: `{"title":"t"}`}
	d := newDownloader(t, runner, downloader.Options{
		CookieFile: filepath.Join(t.TempDir(), "does-not-exist.txt"),
	})

	if _, err := d.Metadata(t.Context(), t.TempDir(), "https://youtu.be/abc"); err != nil {
		t.Fatalf("metadata: %v", err)
	}

	if slices.Contains(runner.last(t), "--cookies") {
		t.Error("a cookie file that does not exist was passed to yt-dlp anyway")
	}
}

func TestADownloaderNeedsARunnerAndABinary(t *testing.T) {
	t.Parallel()

	if _, err := downloader.New(downloader.Options{Binary: "yt-dlp"}); err == nil {
		t.Error("a downloader was built without a runner")
	}

	if _, err := downloader.New(downloader.Options{Runner: &fakeRunner{}}); err == nil {
		t.Error("a downloader was built without yt-dlp")
	}
}

func TestTheEstimateDependsOnTheQualityBeingAskedFor(t *testing.T) {
	t.Parallel()

	// A 4K video: the best stream is three orders of magnitude larger than
	// the smallest pre-muxed one. Checking a `smallest` request against the
	// 4K figure refuses a download that comfortably fits, which users report
	// as "it says the video is too large and it is not".
	runner := &fakeRunner{stdout: `{
		"title": "Big Buck Bunny 4K",
		"duration": 635,
		"formats": [
			{"format_id":"18","ext":"mp4","vcodec":"avc1","acodec":"mp4a","filesize":5000000,"tbr":500},
			{"format_id":"22","ext":"mp4","vcodec":"avc1","acodec":"mp4a","filesize":40000000,"tbr":1200},
			{"format_id":"315","ext":"mp4","vcodec":"vp9","acodec":"none","filesize":700000000,"tbr":20000}
		]
	}`}

	d := newDownloader(t, runner, downloader.Options{})

	media, err := d.Metadata(t.Context(), t.TempDir(), "https://youtu.be/abc")
	if err != nil {
		t.Fatalf("metadata: %v", err)
	}

	if media.EstimatedBytes != 700_000_000 {
		t.Errorf("best estimate %d, want the largest stream", media.EstimatedBytes)
	}

	// The video-only 4K stream is not a candidate for "smallest": the
	// selector wants picture and sound.
	if media.SmallestBytes != 5_000_000 {
		t.Errorf("smallest estimate %d, want the smallest pre-muxed stream", media.SmallestBytes)
	}

	if got := media.EstimateFor(downloader.QualityBest); got != 700_000_000 {
		t.Errorf("EstimateFor(best) = %d", got)
	}

	if got := media.EstimateFor(downloader.QualitySmallest); got != 5_000_000 {
		t.Errorf("EstimateFor(smallest) = %d", got)
	}
}

func TestASmallestDownloadIsNotRefusedBecauseThe4KStreamIsLarge(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{files: map[string]int{"download.mp4": 4 << 20}}
	d := newDownloader(t, runner, downloader.Options{MaxBytes: 100 << 20})

	media := downloader.Media{EstimatedBytes: 700 << 20, SmallestBytes: 5 << 20}

	if _, err := d.Download(t.Context(), downloader.Request{
		URL:      "https://youtu.be/abc",
		Dir:      t.TempDir(),
		Kind:     downloader.KindVideo,
		Quality:  downloader.QualitySmallest,
		Estimate: media.EstimateFor(downloader.QualitySmallest),
	}); err != nil {
		t.Fatalf("a download that fits was refused: %v", err)
	}

	// The best-quality request against the same video is still refused.
	_, err := d.Download(t.Context(), downloader.Request{
		URL:      "https://youtu.be/abc",
		Dir:      t.TempDir(),
		Kind:     downloader.KindVideo,
		Quality:  downloader.QualityBest,
		Estimate: media.EstimateFor(downloader.QualityBest),
	})

	if failure := downloader.Classify(err); failure == nil || failure.Reason != downloader.ReasonTooLarge {
		t.Fatalf("error %v, want too_large for the best-quality request", err)
	}
}

func TestTheSmallestEstimateIgnoresFormatsWithNoDeclaredSize(t *testing.T) {
	t.Parallel()

	// A format with no size would otherwise be estimated from the *highest*
	// bitrate on offer, which would make the "smallest" figure the largest
	// one. Zero is the honest answer, and EstimateFor then falls back to the
	// best-quality figure, which is the safe direction.
	runner := &fakeRunner{stdout: `{
		"title": "No sizes on the muxed formats",
		"duration": 100,
		"tbr": 800,
		"formats": [{"format_id":"18","ext":"mp4","vcodec":"avc1","acodec":"mp4a","tbr":500}]
	}`}

	d := newDownloader(t, runner, downloader.Options{})

	media, err := d.Metadata(t.Context(), t.TempDir(), "https://youtu.be/abc")
	if err != nil {
		t.Fatalf("metadata: %v", err)
	}

	if media.SmallestBytes != 0 {
		t.Errorf("smallest estimate %d, want 0 when nothing declares a size", media.SmallestBytes)
	}

	if media.EstimateFor(downloader.QualitySmallest) != media.EstimatedBytes {
		t.Error("EstimateFor(smallest) did not fall back to the best-quality figure")
	}
}

func TestADownloadAsksYtDlpToPrintProgressDespiteQuietMode(t *testing.T) {
	t.Parallel()

	// yt-dlp's --quiet suppresses the progress template, and --progress is
	// documented as "show progress bar, even if in quiet mode". Without both
	// the download still succeeds and no progress line is ever emitted, so
	// the user watches a bar that never moves — a silent failure a fake
	// runner cannot catch, which is why it is pinned here.
	runner := &fakeRunner{files: map[string]int{"download.mp4": 1024}}
	d := newDownloader(t, runner, downloader.Options{})

	if _, err := d.Download(t.Context(), downloader.Request{
		URL:  "https://youtu.be/abc",
		Dir:  t.TempDir(),
		Kind: downloader.KindVideo,
	}); err != nil {
		t.Fatalf("download: %v", err)
	}

	args := runner.last(t)

	for _, want := range []string{"--progress", "--newline", "--progress-template"} {
		if !slices.Contains(args, want) {
			t.Errorf("%s is missing, so progress would be silent: %v", want, args)
		}
	}

	// The template has to be the one the parser understands.
	template, ok := argValue(args, "--progress-template")
	if !ok {
		t.Fatal("no progress template")
	}

	if !strings.HasPrefix(template, "download:{") {
		t.Errorf("the template is not a download-scoped JSON object: %q", template)
	}

	// Round-trip: a line rendered from this template must parse. The field
	// names in the template and the ones ParseProgress reads are the one
	// thing that can silently disagree.
	rendered := strings.Replace(template, "download:", "", 1)
	for placeholder, value := range map[string]string{
		"%(progress.status)s":               "downloading",
		"%(progress.downloaded_bytes)s":     "512",
		"%(progress.total_bytes)s":          "1024",
		"%(progress.total_bytes_estimate)s": "NA",
	} {
		rendered = strings.ReplaceAll(rendered, placeholder, value)
	}

	if strings.Contains(rendered, "%(") {
		t.Fatalf("the template holds a field this test does not substitute: %q", rendered)
	}

	progress, parsed := downloader.ParseProgress(rendered)
	if !parsed {
		t.Fatalf("a line rendered from the template does not parse: %q", rendered)
	}

	if progress.Percent() != 50 {
		t.Errorf("the round-tripped line reports %d%%, want 50%%", progress.Percent())
	}
}
