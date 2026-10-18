package tools

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AlexGalhardo/vukem-tools/internal/compressor"
	"github.com/AlexGalhardo/vukem-tools/internal/downloader"
	"github.com/AlexGalhardo/vukem-tools/internal/editor"
	"github.com/AlexGalhardo/vukem-tools/internal/engine"
	"github.com/AlexGalhardo/vukem-tools/internal/jobs"
)

// This is an internal test on purpose: the mapping from an engine package's
// own vocabulary onto the job error taxonomy is unexported, and it is the
// part most worth pinning down. A client branches on the code, so a reason
// that silently started mapping to `internal` would turn a refusal the user
// could act on into "something went wrong on our side".

func TestDownloadOptionsRefuseAURLOutsideTheFourPlatforms(t *testing.T) {
	t.Parallel()

	cases := []string{
		"https://vimeo.com/1",
		"https://youtube.com.attacker.example/watch?v=a",
		"http://169.254.169.254/latest/meta-data/",
		"file:///etc/passwd",
		"",
		"   ",
	}

	for _, url := range cases {
		if _, err := (DownloadOptions{URL: url}).Normalise(); err == nil {
			t.Errorf("%q was accepted", url)
		}
	}
}

func TestDownloadOptionsFillInTheDocumentedDefaults(t *testing.T) {
	t.Parallel()

	got, err := (DownloadOptions{URL: "https://youtu.be/abc"}).Normalise()
	if err != nil {
		t.Fatalf("normalise: %v", err)
	}

	if got.Kind != string(downloader.KindVideo) {
		t.Errorf("kind %q, want video", got.Kind)
	}

	if got.Quality != string(downloader.QualityBest) {
		t.Errorf("quality %q, want best", got.Quality)
	}

	if got.clipLength() != editor.MaxClip {
		t.Errorf("clip length %s, want %s", got.clipLength(), editor.MaxClip)
	}
}

func TestDownloadOptionsRefuseUndocumentedValues(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   DownloadOptions
	}{
		{name: "an unknown kind", in: DownloadOptions{URL: "https://youtu.be/a", Kind: "subtitles"}},
		{name: "an unknown quality", in: DownloadOptions{URL: "https://youtu.be/a", Quality: "4k"}},
		{name: "a clip longer than the bound", in: DownloadOptions{URL: "https://youtu.be/a", ClipSeconds: 90}},
		{name: "a negative clip", in: DownloadOptions{URL: "https://youtu.be/a", ClipSeconds: -1}},
		{
			// Clips of an MP3 are not a thing anybody asked for, and the
			// segment muxer would produce files no player shows.
			name: "clips of audio",
			in:   DownloadOptions{URL: "https://youtu.be/a", Kind: "audio", Clips: true},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			if _, err := c.in.Normalise(); err == nil {
				t.Errorf("%+v was accepted", c.in)
			}
		})
	}
}

func TestParseDownloadOptionsRefusesSomethingThatIsNotAnObject(t *testing.T) {
	t.Parallel()

	if _, err := ParseDownloadOptions(json.RawMessage(`"a string"`)); err == nil {
		t.Error("a JSON string was accepted as options")
	}
}

func TestValidateOptionsStoresWhatWasValidated(t *testing.T) {
	t.Parallel()

	raw, err := (&VideoDownload{}).ValidateOptions(json.RawMessage(`{"url":"https://youtu.be/abc"}`))
	if err != nil {
		t.Fatalf("validate: %v", err)
	}

	stored, err := ParseDownloadOptions(raw)
	if err != nil {
		t.Fatalf("the stored options do not validate: %v", err)
	}

	if stored.Kind != string(downloader.KindVideo) || stored.Quality != string(downloader.QualityBest) {
		t.Errorf("the defaults were not stored: %+v", stored)
	}
}

func TestEveryDownloadReasonMapsOntoTheTaxonomy(t *testing.T) {
	t.Parallel()

	cases := []struct {
		reason downloader.Reason
		want   jobs.ErrorType
	}{
		{downloader.ReasonUnsupportedURL, jobs.UnsupportedInput},
		{downloader.ReasonPrivate, jobs.UnsupportedInput},
		{downloader.ReasonAgeRestricted, jobs.UnsupportedInput},
		{downloader.ReasonUnavailable, jobs.UnsupportedInput},
		{downloader.ReasonTooLarge, jobs.InputTooLarge},
		{downloader.ReasonDiskFull, jobs.DiskFull},
		{downloader.ReasonOutOfMemory, jobs.OutOfMemory},
		{downloader.ReasonTimeout, jobs.Timeout},
		{downloader.ReasonCancelled, jobs.CancelledByUser},
		{downloader.ReasonPlatformBlock, jobs.EngineFailure},
		{downloader.ReasonEngine, jobs.EngineFailure},
	}

	for _, c := range cases {
		t.Run(string(c.reason), func(t *testing.T) {
			t.Parallel()

			if got := downloadErrorType(c.reason); got != c.want {
				t.Fatalf("%s maps to %s, want %s", c.reason, got, c.want)
			}

			if !jobs.Known(c.want) {
				t.Fatalf("%s is not in the taxonomy", c.want)
			}
		})
	}
}

// downloadFixtures are yt-dlp's own messages for every failure the ROADMAP
// names, so the whole path — stderr to reason to taxonomy code to the
// sentence a user reads — is exercised end to end rather than in halves.
var downloadFixtures = []struct {
	name   string
	stderr string
	want   jobs.ErrorType
	says   string
	// generic marks a fixture whose own sentence is deliberately the same as
	// the taxonomy's, because the taxonomy already words it correctly. Only
	// the others prove that Public survives the mapping.
	generic bool
}{
	{
		name:   "age restricted",
		stderr: "ERROR: [youtube] a: Sign in to confirm your age",
		want:   jobs.UnsupportedInput,
		says:   "age-restricted",
	},
	{
		name:   "private",
		stderr: "ERROR: [youtube] a: Private video",
		want:   jobs.UnsupportedInput,
		says:   "private",
	},
	{
		name:   "platform block",
		stderr: "ERROR: unable to download video data: HTTP Error 429: Too Many Requests",
		want:   jobs.EngineFailure,
		says:   "refused this server",
	},
	{
		name:   "unsupported url",
		stderr: "ERROR: Unsupported URL: https://example.com/x",
		want:   jobs.UnsupportedInput,
		says:   "not one this service can download from",
	},
	{
		name:   "too large",
		stderr: "ERROR: File is larger than max-filesize (999 bytes > 10 bytes). Aborting.",
		want:   jobs.InputTooLarge,
		says:   "larger than",
	},
	{
		name:    "disk full",
		stderr:  "ERROR: unable to write data: [Errno 28] No space left on device",
		want:    jobs.DiskFull,
		says:    "working space",
		generic: true,
	},
	{
		name:   "out of memory",
		stderr: "MemoryError",
		want:   jobs.OutOfMemory,
		says:   "more memory",
	},
}

func TestEveryDownloadFailureReachesTheUserAsItsOwnSentence(t *testing.T) {
	t.Parallel()

	for _, f := range downloadFixtures {
		t.Run(f.name, func(t *testing.T) {
			t.Parallel()

			err := downloadFailure(&engine.ExitError{Code: 1, Stderr: f.stderr})

			if got := jobs.Classify(err); got != f.want {
				t.Fatalf("classified as %s, want %s", got, f.want)
			}

			message := jobs.MessageOf(err)

			if !strings.Contains(strings.ToLower(message), f.says) {
				t.Errorf("the message does not say %q: %q", f.says, message)
			}

			// The taxonomy's generic sentence would lose the reason, which is
			// the whole point of carrying Public through.
			if !f.generic && message == jobs.Describe(f.want).Message {
				t.Errorf("the specific sentence was replaced by the taxonomy's: %q", message)
			}

			// The owner-only detail may hold the engine's text; the message
			// must not.
			if strings.Contains(message, "ERROR:") || strings.Contains(message, "http") {
				t.Errorf("the message leaks engine output: %q", message)
			}

			if jobs.DetailOf(err) == "" {
				t.Error("no owner-only detail was recorded")
			}
		})
	}
}

func TestDownloadFailureOfNothingIsNothing(t *testing.T) {
	t.Parallel()

	if err := downloadFailure(nil); err != nil {
		t.Errorf("a success was turned into %v", err)
	}
}

func TestEveryEditReasonMapsOntoTheTaxonomy(t *testing.T) {
	t.Parallel()

	cases := []struct {
		reason editor.Reason
		want   jobs.ErrorType
	}{
		{editor.ReasonUnsupportedInput, jobs.UnsupportedInput},
		{editor.ReasonOutOfRange, jobs.UnsupportedInput},
		{editor.ReasonTooLong, jobs.InputTooLarge},
		{editor.ReasonDiskFull, jobs.DiskFull},
		{editor.ReasonOutOfMemory, jobs.OutOfMemory},
		{editor.ReasonTimeout, jobs.Timeout},
		{editor.ReasonCancelled, jobs.CancelledByUser},
		{editor.ReasonEngine, jobs.EngineFailure},
	}

	for _, c := range cases {
		if got := editErrorType(c.reason); got != c.want {
			t.Errorf("%s maps to %s, want %s", c.reason, got, c.want)
		}
	}

	if err := editFailure(nil); err != nil {
		t.Errorf("a success was turned into %v", err)
	}
}

func TestEveryCompressReasonMapsOntoTheTaxonomy(t *testing.T) {
	t.Parallel()

	cases := []struct {
		reason compressor.Reason
		want   jobs.ErrorType
	}{
		{compressor.ReasonUnsupportedInput, jobs.UnsupportedInput},
		{compressor.ReasonEncrypted, jobs.UnsupportedInput},
		{compressor.ReasonDiskFull, jobs.DiskFull},
		{compressor.ReasonOutOfMemory, jobs.OutOfMemory},
		{compressor.ReasonTimeout, jobs.Timeout},
		{compressor.ReasonCancelled, jobs.CancelledByUser},
		{compressor.ReasonEngine, jobs.EngineFailure},
	}

	for _, c := range cases {
		if got := compressErrorType(c.reason); got != c.want {
			t.Errorf("%s maps to %s, want %s", c.reason, got, c.want)
		}
	}

	if err := compressFailure(nil); err != nil {
		t.Errorf("a success was turned into %v", err)
	}
}

func TestACropRefusalReachesTheUserWithTheRealDimensions(t *testing.T) {
	t.Parallel()

	// The sentence internal/editor builds for a crop outside the frame has
	// to survive the taxonomy, or the user is told "that file is not a
	// format this tool can read" about a file that is fine.
	refusal := editor.Classify(&editor.EditError{
		Reason: editor.ReasonOutOfRange,
		Public: "That area reaches 1920x1080, but the video is only 1280x720. Pick an area inside it.",
		Detail: "crop exceeds 1280x720",
	})

	err := editFailure(refusal)

	if !strings.Contains(jobs.MessageOf(err), "1280x720") {
		t.Errorf("the message lost the real dimensions: %q", jobs.MessageOf(err))
	}
}

func TestTheZipHoldsOneEntryPerClip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	clips := make([]editor.Clip, 0, 5)

	for i := range 5 {
		name := "clip00" + string(rune('0'+i)) + ".mp4"
		path := filepath.Join(dir, name)

		if err := os.WriteFile(path, make([]byte, 128+i), 0o600); err != nil {
			t.Fatal(err)
		}

		clips = append(clips, editor.Clip{Index: i, Path: path, Name: name, Bytes: int64(128 + i)})
	}

	tool := &VideoDownload{}

	archive, err := tool.zipClips(dir, clips)
	if err != nil {
		t.Fatalf("zip: %v", err)
	}

	reader, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatalf("open the archive: %v", err)
	}

	defer func() { _ = reader.Close() }()

	// The ROADMAP's assert: the zip entry count matches the reported clip
	// count.
	if len(reader.File) != len(clips) {
		t.Fatalf("%d entries, want %d", len(reader.File), len(clips))
	}

	for i, entry := range reader.File {
		if entry.Name != clips[i].Name {
			t.Errorf("entry %d is %q, want %q", i, entry.Name, clips[i].Name)
		}

		if entry.UncompressedSize64 != uint64(clips[i].Bytes) {
			t.Errorf("entry %q is %d bytes, want %d", entry.Name, entry.UncompressedSize64, clips[i].Bytes)
		}

		// Stored, not deflated: an H.264 stream is already compressed, and
		// deflating it spends CPU to add bytes.
		if entry.Method != zip.Store {
			t.Errorf("entry %q is compressed with method %d", entry.Name, entry.Method)
		}
	}
}

func TestAMissingEngineIsRefusedBeforeAnyBandwidthIsSpent(t *testing.T) {
	t.Parallel()

	// A conversion queued while ffmpeg was present and run after it went
	// missing: it will never work, and failing before the download is what
	// keeps it from costing the bandwidth first.
	tool := &VideoDownload{}

	if err := tool.refuseUnavailableOptions(DownloadOptions{Clips: true}); err == nil {
		t.Error("clips were accepted without ffmpeg")
	}

	if err := tool.refuseUnavailableOptions(DownloadOptions{Compress: true}); err == nil {
		t.Error("compression was accepted without ffmpeg")
	}

	if err := tool.refuseUnavailableOptions(DownloadOptions{}); err != nil {
		t.Errorf("a plain download was refused: %v", err)
	}
}

func TestATitleFromAPlatformCannotEscapeIntoAFileName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		title string
		want  string
	}{
		{title: "A normal title", want: "A-normal-title"},
		// "/" is dropped rather than folded into a separator, so the stem
		// cannot carry a path component at all.
		{title: "../../etc/passwd", want: "etcpasswd"},
		{title: `C:\Windows\System32`, want: "CWindowsSystem32"},
		{title: "a\x00b", want: "ab"},
		{title: "  spaced  out  ", want: "spaced-out"},
		{title: "🎬🎬🎬", want: ""},
		{title: "", want: ""},
		{title: strings.Repeat("x", 500), want: strings.Repeat("x", maxStem)},
	}

	for _, c := range cases {
		if got := safeStem(c.title); got != c.want {
			t.Errorf("safeStem(%q) = %q, want %q", c.title, got, c.want)
		}
	}

	// Whatever the title was, the stem never holds a separator, so it cannot
	// reach outside the directory it is joined to.
	for _, c := range cases {
		stem := safeStem(c.title)
		if strings.ContainsAny(stem, `/\:`) || strings.Contains(stem, "..") {
			t.Errorf("safeStem(%q) = %q, which is not a safe file name", c.title, stem)
		}
	}
}

func TestStemOfFallsBackRatherThanProducingAnEmptyName(t *testing.T) {
	t.Parallel()

	if got := stemOf("🎬.mp4"); got != "video" {
		t.Errorf("stemOf = %q, want the fallback", got)
	}

	if got := stemOf("holiday.mp4"); got != "holiday" {
		t.Errorf("stemOf = %q, want holiday", got)
	}
}

func TestScaleMapsAnEnginePercentageOntoAWindow(t *testing.T) {
	t.Parallel()

	cases := []struct {
		percent int
		want    int
	}{
		{percent: -1, want: 5},
		{percent: 0, want: 5},
		{percent: 50, want: 35},
		{percent: 100, want: 65},
	}

	for _, c := range cases {
		if got := scale(c.percent, phaseDownloadFrom, phaseDownloadTo); got != c.want {
			t.Errorf("scale(%d) = %d, want %d", c.percent, got, c.want)
		}
	}
}

func TestClipOptionsRefuseACropThatCannotBeValidForAnyVideo(t *testing.T) {
	t.Parallel()

	cases := []ClipOptions{
		{Crop: editor.Crop{X: -1, Y: 0, Width: 10, Height: 10}},
		{Crop: editor.Crop{X: 0, Y: 0, Width: 0, Height: 10}},
		{Crop: editor.Crop{X: 0, Y: 0, Width: 10, Height: -5}},
		{StartSeconds: -1},
		{DurationSeconds: -1},
		{Mode: "reverse"},
		{ClipSeconds: 61},
	}

	for _, in := range cases {
		if _, err := in.Normalise(); err == nil {
			t.Errorf("%+v was accepted", in)
		}
	}
}

func TestClipOptionsAcceptAWholeFrameCropAndTheDefaults(t *testing.T) {
	t.Parallel()

	got, err := (ClipOptions{}).Normalise()
	if err != nil {
		t.Fatalf("normalise: %v", err)
	}

	if got.Mode != ClipModeTrim {
		t.Errorf("mode %q, want trim", got.Mode)
	}

	if !got.Crop.Zero() {
		t.Errorf("crop %+v, want no crop", got.Crop)
	}

	if got.clipLength() != editor.MaxClip {
		t.Errorf("clip length %s, want %s", got.clipLength(), editor.MaxClip)
	}
}

func TestTheToolsReportTheirOwnIdentity(t *testing.T) {
	t.Parallel()

	// The names are the public API: they are stored on every conversion,
	// published by GET /api/v1/tools and used as the URL segment of the
	// tool's page, so renaming one is a breaking change.
	cases := []struct {
		tool    jobs.Tool
		name    string
		ability string
	}{
		{tool: &VideoDownload{}, name: "video-download", ability: "video:download"},
		{tool: &VideoClip{}, name: "video-clip", ability: "video:edit"},
		{tool: &VideoCompress{}, name: "video-compress", ability: "video:edit"},
	}

	for _, c := range cases {
		if got := c.tool.Name(); got != c.name {
			t.Errorf("name %q, want %q", got, c.name)
		}

		if got := c.tool.Ability(); got != c.ability {
			t.Errorf("%s ability %q, want %q", c.name, got, c.ability)
		}

		if c.tool.Timeout() <= 0 {
			t.Errorf("%s has no timeout", c.name)
		}
	}
}

func TestAnUnavailableEngineRegistersNoTool(t *testing.T) {
	t.Parallel()

	// An empty Engines has no registry, so nothing is available and no tool
	// is registered. That is what makes the catalogue honest on an image
	// without the engines.
	if got := downloadTools(nil, Engines{}); got != nil {
		t.Errorf("%d download tools were registered without yt-dlp", len(got))
	}

	if got := editTools(nil, Engines{}); got != nil {
		t.Errorf("%d edit tools were registered without ffmpeg", len(got))
	}
}

func TestSettingsFallBackRatherThanFailing(t *testing.T) {
	t.Parallel()

	e := Engines{Config: mapLookup{
		"NUMBER":   "7",
		"NONSENSE": "not a number",
		"NEGATIVE": "-3",
		"FRACTION": "1.5",
	}}

	if got := e.number("NUMBER", 1); got != 7 {
		t.Errorf("number = %d, want 7", got)
	}

	for _, key := range []string{"NONSENSE", "NEGATIVE", "MISSING"} {
		if got := e.number(key, 42); got != 42 {
			t.Errorf("number(%s) = %d, want the default 42", key, got)
		}
	}

	if got := e.seconds("FRACTION"); got.Seconds() != 1.5 {
		t.Errorf("seconds = %s, want 1.5s", got)
	}

	if got := e.seconds("NONSENSE"); got != 0 {
		t.Errorf("seconds of nonsense = %s, want 0", got)
	}

	if got := e.megabytes("NUMBER", 1); got != 7<<20 {
		t.Errorf("megabytes = %d, want %d", got, 7<<20)
	}

	// No configuration at all must not panic: a test and a one-off command
	// both build tools without one.
	if got := (Engines{}).setting("ANYTHING"); got != "" {
		t.Errorf("setting without a config = %q", got)
	}
}

// mapLookup is an engine.Lookup backed by a map.
type mapLookup map[string]string

func (m mapLookup) Get(key string) string { return m[key] }

func TestErrTooManyClipsIsAnErrorRatherThanAString(t *testing.T) {
	t.Parallel()

	// It is wrapped into the refusal, so it has to be a real error for
	// errors.Is to reach it from a log.
	if !errors.Is(errTooManyClips, errTooManyClips) {
		t.Error("errTooManyClips is not usable with errors.Is")
	}
}

// recordingRunner records every engine invocation and never succeeds, so a
// test can assert that an engine was *not* started.
type recordingRunner struct{ calls int }

func (r *recordingRunner) Run(context.Context, engine.Command) (engine.Result, error) {
	r.calls++

	return engine.Result{}, errors.New("the engine should not have been started")
}

func TestTheToolRefusesANonPlatformURLWithoutStartingAnEngine(t *testing.T) {
	t.Parallel()

	// The guarantee has to hold at the tool, not only in the handler: an
	// unvalidated URL reaching yt-dlp is a request forger with this
	// service's egress, and the next endpoint somebody adds will call the
	// tool rather than repeat the handler's check.
	runner := &recordingRunner{}

	down, err := downloader.New(downloader.Options{Runner: runner, Binary: "yt-dlp"})
	if err != nil {
		t.Fatalf("new downloader: %v", err)
	}

	tool := NewVideoDownload(nil, down, nil, nil)

	for _, target := range []string{
		"http://169.254.169.254/latest/meta-data/",
		"http://localhost:5432",
		"http://127.0.0.1:6379/",
		"https://youtube.com.attacker.example/watch?v=a",
		"file:///etc/passwd",
		"https://www.youtube.com@attacker.example/watch?v=a",
	} {
		media, err := tool.Metadata(t.Context(), target)
		if err == nil {
			t.Errorf("%s was probed and returned %+v", target, media)
		}

		if got := jobs.Classify(err); got != jobs.UnsupportedInput {
			t.Errorf("%s classified as %s, want unsupported_input", target, got)
		}
	}

	if runner.calls != 0 {
		t.Fatalf("an engine was started %d times for addresses outside the platform list", runner.calls)
	}
}

func TestNoCallerValueCanReachAnFFmpegFiltergraph(t *testing.T) {
	t.Parallel()

	// A filtergraph is its own language: `,` and `;` and `[` separate
	// filters, so a caller-chosen *string* spliced into -vf adds filters
	// rather than being quoted away. The crop is four ints, which is what
	// makes that impossible rather than merely unlikely — these are the
	// payloads that would work if any of them were a string.
	for _, payload := range []string{
		`{"crop":{"x":0,"y":0,"width":"640,drawtext=text=pwned","height":360}}`,
		`{"crop":{"x":0,"y":0,"width":"640[a];[a]movie=/etc/passwd","height":360}}`,
		`{"crop":{"x":"0;exec","y":0,"width":640,"height":360}}`,
		`{"clip_seconds":"60,drawtext=x"}`,
		`{"start_seconds":"0;x"}`,
		`{"mode":"trim,drawtext=text=x"}`,
	} {
		if _, err := ParseClipOptions(json.RawMessage(payload)); err == nil {
			t.Errorf("%s was accepted", payload)
		}
	}

	// And the values that do get through are numbers, so the only thing that
	// can appear in the filtergraph is a digit.
	opts, err := ParseClipOptions(json.RawMessage(`{"crop":{"x":1,"y":2,"width":3,"height":4}}`))
	if err != nil {
		t.Fatalf("a valid crop was refused: %v", err)
	}

	if opts.Crop != (editor.Crop{X: 1, Y: 2, Width: 3, Height: 4}) {
		t.Errorf("crop %+v", opts.Crop)
	}
}
