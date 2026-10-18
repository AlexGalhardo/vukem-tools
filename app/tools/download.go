package tools

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/AlexGalhardo/vukem/z"

	"github.com/AlexGalhardo/vukem-tools/app/abilities"
	"github.com/AlexGalhardo/vukem-tools/internal/compressor"
	"github.com/AlexGalhardo/vukem-tools/internal/downloader"
	"github.com/AlexGalhardo/vukem-tools/internal/editor"
	"github.com/AlexGalhardo/vukem-tools/internal/engine"
	"github.com/AlexGalhardo/vukem-tools/internal/files"
	"github.com/AlexGalhardo/vukem-tools/internal/jobs"
)

// Engines is what a tool needs to run an external program: the resolved
// binaries, the runner that applies their limits, and the configuration the
// engines themselves are tuned from.
//
// It is a struct rather than three parameters because the number of engines
// a tool needs only grows, and because a tool that takes "the engines"
// reads better than one that takes six paths.
//
// Config is engine.Lookup, which *config.Source satisfies. It is here rather
// than read at boot and passed as a settled struct so that each domain owns
// the names of its own settings: the downloader's cookie jar is no business
// of the sprite tools.
type Engines struct {
	Registry *engine.Registry
	Runner   *engine.Runner
	Config   engine.Lookup
}

// available reports whether every named engine was resolved, which is what a
// tool asks before offering itself.
func (e Engines) available(names ...string) bool {
	if e.Registry == nil || e.Runner == nil {
		return false
	}

	for _, name := range names {
		if !e.Registry.Has(name) {
			return false
		}
	}

	return true
}

// path returns an engine's absolute path, or "" when it was not resolved.
func (e Engines) path(name string) string {
	if e.Registry == nil {
		return ""
	}

	found, _ := e.Registry.Path(name)

	return found
}

// setting reads a configuration value.
func (e Engines) setting(key string) string {
	if e.Config == nil {
		return ""
	}

	return strings.TrimSpace(e.Config.Get(key))
}

// minutes reads a setting that is a whole number of minutes, falling back to
// def for an unset or unusable value.
func (e Engines) minutes(key string, def time.Duration) time.Duration {
	return time.Duration(e.number(key, int(def/time.Minute))) * time.Minute
}

// megabytes reads a setting that is a whole number of megabytes.
func (e Engines) megabytes(key string, def int) int64 {
	return int64(e.number(key, def)) << 20
}

// seconds reads a setting that may be fractional seconds, which is what
// yt-dlp's sleep options take.
func (e Engines) seconds(key string) time.Duration {
	raw := e.setting(key)
	if raw == "" {
		return 0
	}

	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || value <= 0 {
		return 0
	}

	return time.Duration(value * float64(time.Second))
}

// number reads a positive integer setting.
//
// A nonsense value falls back to the default rather than failing the boot:
// every setting here has a safe default, and a service that will not start
// is worse than one running on it. The same rule services.intFrom follows.
func (e Engines) number(key string, def int) int {
	raw := e.setting(key)
	if raw == "" {
		return def
	}

	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return def
	}

	return value
}

// Settings the download and edit tools read, with their defaults.
const (
	// settingCookieFile is a Netscape cookie jar for the platforms that
	// want a signed-in session. Its absence degrades rather than fails.
	settingCookieFile = "YOUTUBE_COOKIES_FILE"
	// settingPOTokenBaseURL is a bgutil PO-token provider sidecar.
	//nolint:gosec // G101: this is the name of a setting whose value is a base URL, not a token. The PO token itself is fetched from the provider at run time and never stored here.
	settingPOTokenBaseURL = "YTDLP_POT_PROVIDER_BASE_URL"
	// The sleep settings slow yt-dlp down so it looks less like a scraper.
	settingSleepRequests = "YTDLP_SLEEP_REQUESTS_SECONDS"
	settingSleepInterval = "YTDLP_SLEEP_INTERVAL_SECONDS"
	settingMaxSleep      = "YTDLP_MAX_SLEEP_INTERVAL_SECONDS"
	settingRetries       = "YTDLP_RETRIES"
	// settingMaxDownloadMB caps a download. It matches the upload limit by
	// default, so the two halves of the service promise the same size.
	settingMaxDownloadMB = "MAX_DOWNLOAD_SIZE_MB"
	// settingMaxEditMinutes caps the output of a cut.
	settingMaxEditMinutes = "MAX_EDIT_DURATION_MINUTES"

	defaultRetries        = 3
	defaultMaxDownloadMB  = 500
	defaultMaxEditMinutes = 30 * time.Minute
)

// downloadTools is the downloads driving yt-dlp domain (S3.1).
//
// The tool is registered only when yt-dlp was resolved. A tool whose engine
// is absent would be advertised on the index, accept a submission and fail
// every one of them with "no such file or directory", so it is better not to
// exist: the catalogue then shows it as planned, which is the honest status
// for an image that cannot run it.
func downloadTools(f *files.Store, e Engines) []jobs.Tool {
	if !e.available("yt-dlp") {
		return nil
	}

	down, err := downloader.New(downloader.Options{
		Runner:           e.Runner,
		Binary:           e.path("yt-dlp"),
		FFmpeg:           e.path("ffmpeg"),
		CookieFile:       e.setting(settingCookieFile),
		POTokenBaseURL:   e.setting(settingPOTokenBaseURL),
		SleepRequests:    e.seconds(settingSleepRequests),
		SleepInterval:    e.seconds(settingSleepInterval),
		MaxSleepInterval: e.seconds(settingMaxSleep),
		Retries:          e.number(settingRetries, defaultRetries),
		MaxBytes:         e.megabytes(settingMaxDownloadMB, defaultMaxDownloadMB),
	})
	if err != nil {
		// Construction fails only on a missing runner or binary, both of
		// which available() just checked, so this is unreachable rather than
		// tolerated. Returning no tool keeps it that way.
		return nil
	}

	return []jobs.Tool{NewVideoDownload(f, down, newEditor(e), newCompressor(e))}
}

// VideoDownload fetches a video from a URL, and can split it into clips or
// compress it on the way out.
//
// The three post-processing steps are deliberately in the tool rather than in
// internal/downloader: downloading, cutting and compressing are three
// separate packages with three separate engines, and the order they run in is
// a product decision, not a property of yt-dlp.
type VideoDownload struct {
	files *files.Store
	down  *downloader.Downloader
	// edit is nil when ffmpeg or ffprobe is missing, which disables the
	// clips mode rather than the whole tool.
	edit *editor.Editor
	// comp is nil when ffmpeg is missing, which disables the compress
	// option.
	comp *compressor.Compressor
}

// NewVideoDownload returns the tool. edit and comp may be nil, which
// withdraws the options that need them.
func NewVideoDownload(
	f *files.Store,
	down *downloader.Downloader,
	edit *editor.Editor,
	comp *compressor.Compressor,
) *VideoDownload {
	return &VideoDownload{files: f, down: down, edit: edit, comp: comp}
}

// Name implements jobs.Tool. It is the stable id stored on every conversion
// and published by GET /api/v1/tools, so it is part of the public API.
func (*VideoDownload) Name() string { return "video-download" }

// Ability implements jobs.Tool.
func (*VideoDownload) Ability() string { return abilities.VideoDownload }

// Timeout implements jobs.Tool.
//
// Twenty minutes, which is the one tool timeout that is not about compute. A
// download waits on somebody else's server: a platform that throttles to
// 200 kB/s takes forty minutes over a 500 MB file and there is nothing to be
// done about it, so the bound is set by what a user will wait for rather than
// by what the work costs. It is above the engine runner's own 15-minute
// limit on purpose, so yt-dlp is stopped by its limit and the conversion is
// then settled rather than both firing at once.
func (*VideoDownload) Timeout() time.Duration { return 20 * time.Minute }

// CanClip reports whether the clips mode is available, which the API's
// capabilities endpoint and the interface ask before offering it.
func (t *VideoDownload) CanClip() bool { return t.edit != nil }

// CanCompress reports whether the compress option is available.
func (t *VideoDownload) CanCompress() bool { return t.comp != nil }

// MaxBytes is the largest download this server allows, zero for no cap. The
// metadata endpoint publishes it so a client can show the refusal before
// submitting.
func (t *VideoDownload) MaxBytes() int64 { return t.down.MaxBytes() }

// Metadata asks the platform what it has, without downloading anything.
//
// It is on the tool rather than reached through the pipeline because the
// downloader is built here, with this server's cookie jar and sleep settings,
// and a metadata probe that used different settings from the download it
// precedes would report formats the download cannot fetch.
//
// The directory is temporary and its own: yt-dlp writes nothing during a
// --skip-download probe, but engine.Runner requires a working directory, and
// a program that inherits the server's is a program that can write next to
// the binary. It is not the job work root on purpose — there is no job — and
// nothing in it has to survive, so the system temporary directory is right
// even where that is a tmpfs.
func (t *VideoDownload) Metadata(ctx context.Context, url string) (downloader.Media, error) {
	// The platform check is repeated here even though internal/downloader
	// makes it the first statement of both Metadata and Download.
	//
	// It is not redundant, it is where the invariant belongs. An unvalidated
	// URL reaching yt-dlp is a request forger running inside this network
	// with this service's egress: yt-dlp's generic extractor fetches whatever
	// it is given, and http://169.254.169.254/latest/meta-data/ and
	// http://localhost:5432 are both reachable from a conversion container.
	// A check in one caller protects one caller; a check here protects every
	// path into the tool, including the next endpoint somebody adds. The
	// extractor allowlist in internal/downloader is the third layer, and all
	// three fail differently on purpose: a hostname rule can be wrong about a
	// domain, and the extractor list still refuses to fetch it.
	if _, err := downloader.PlatformOf(url); err != nil {
		return downloader.Media{}, jobs.Refuse(jobs.UnsupportedInput, err,
			"That URL is not one this service can download from. "+
				"YouTube, Instagram, X and TikTok are supported.",
			"the URL is not on the platform allowlist")
	}

	dir, err := os.MkdirTemp("", "vukem-probe-")
	if err != nil {
		return downloader.Media{}, jobs.Fail(jobs.DiskFull, err, "create a directory for the probe")
	}

	defer func() { _ = os.RemoveAll(dir) }()

	media, err := t.down.Metadata(ctx, dir, url)
	if err != nil {
		return downloader.Media{}, err
	}

	return media, nil
}

// MaxClips is how many clips one conversion may produce.
//
// Sixty clips at a minute each is an hour of video, which is past anything
// this service accepts. The cap exists because every clip is a separate
// object in the bucket and a separate entry in the zip, so an unbounded
// count turns one conversion into a thousand uploads.
const MaxClips = 60

// DownloadOptions is the validated input of a video-download conversion.
type DownloadOptions struct {
	// URL is the page to download from. It must belong to one of the four
	// platforms; anything else is refused at submission rather than by
	// yt-dlp, so the generic extractor is never reached.
	URL string `json:"url" validate:"required,trim,max=2048" doc:"A YouTube, Instagram, X or TikTok URL."`
	// Kind is video or audio.
	Kind string `json:"kind" validate:"trim,max=8" doc:"video keeps the picture, audio gives MP3. Default video."`
	// Quality is best or smallest.
	Quality string `json:"quality" validate:"trim,max=16" doc:"best or smallest. Default best."`
	// Compress re-encodes the result and delivers whichever file is
	// smaller, reporting the saving.
	Compress bool `json:"compress" doc:"Compress afterwards and keep whichever file is smaller."`
	// Clips splits the result into consecutive segments.
	Clips bool `json:"clips" doc:"Split the result into consecutive clips, delivered individually and as a zip."`
	// ClipSeconds is how long each clip should be, at most 60.
	ClipSeconds int `json:"clip_seconds" validate:"min=0,max=60" doc:"Clip length in seconds, 1 to 60. Default 60."`
}

// The kinds and qualities a client may name, as the API documents them.
var (
	downloadKinds     = []string{string(downloader.KindVideo), string(downloader.KindAudio)}
	downloadQualities = []string{string(downloader.QualityBest), string(downloader.QualitySmallest)}
)

// DownloadKinds returns the accepted kinds, for the documentation and the
// interface.
func DownloadKinds() []string { return slices.Clone(downloadKinds) }

// DownloadQualities returns the accepted qualities.
func DownloadQualities() []string { return slices.Clone(downloadQualities) }

// DownloadPlatforms returns the platforms a URL may belong to, for the
// interface.
func DownloadPlatforms() []downloader.Platform { return downloader.Platforms() }

// Normalise fills in every omitted field and refuses a value that is not one
// of the documented ones.
//
// The URL's platform is checked here, at submission, which is the whole point
// of having a closed list: a URL that is not one of the four is answered 422
// with the field named, before a worker, an engine or a network request is
// spent on it.
func (in DownloadOptions) Normalise() (DownloadOptions, error) {
	in.URL = strings.TrimSpace(in.URL)
	if in.URL == "" {
		return DownloadOptions{}, z.FieldError("options.url", "A URL is required.")
	}

	if _, err := downloader.PlatformOf(in.URL); err != nil {
		return DownloadOptions{}, z.FieldError("options.url",
			"That URL is not one this service can download from. YouTube, Instagram, X and TikTok are supported.")
	}

	if in.Kind = strings.TrimSpace(in.Kind); in.Kind == "" {
		in.Kind = string(downloader.KindVideo)
	}

	if !slices.Contains(downloadKinds, in.Kind) {
		return DownloadOptions{}, z.FieldError("options.kind",
			"Unknown kind. Use one of: "+strings.Join(downloadKinds, ", ")+".")
	}

	if in.Quality = strings.TrimSpace(in.Quality); in.Quality == "" {
		in.Quality = string(downloader.QualityBest)
	}

	if !slices.Contains(downloadQualities, in.Quality) {
		return DownloadOptions{}, z.FieldError("options.quality",
			"Unknown quality. Use one of: "+strings.Join(downloadQualities, ", ")+".")
	}

	if in.ClipSeconds < 0 || time.Duration(in.ClipSeconds)*time.Second > editor.MaxClip {
		return DownloadOptions{}, z.FieldError("options.clip_seconds",
			fmt.Sprintf("A clip is between 1 and %d seconds.", int(editor.MaxClip.Seconds())))
	}

	if in.Clips && in.Kind == string(downloader.KindAudio) {
		return DownloadOptions{}, z.FieldError("options.clips",
			"Clips are for video. Ask for video, or drop the clips option.")
	}

	return in, nil
}

// clipLength is the segment length to use, filling in the default.
func (in DownloadOptions) clipLength() time.Duration {
	if in.ClipSeconds <= 0 {
		return editor.MaxClip
	}

	return time.Duration(in.ClipSeconds) * time.Second
}

// ParseDownloadOptions validates and decodes the options stored on a
// conversion. It is exported so the submit endpoint validates exactly what
// the worker will later read.
func ParseDownloadOptions(raw json.RawMessage) (DownloadOptions, error) {
	var in DownloadOptions

	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &in); err != nil {
			return DownloadOptions{}, z.FieldError("options", "The options are not a JSON object.")
		}
	}

	return in.Normalise()
}

// ValidateOptions implements jobs.OptionsValidator. What it returns is what is
// stored, so the worker reads the same options that were validated.
func (*VideoDownload) ValidateOptions(raw json.RawMessage) (json.RawMessage, error) {
	opts, err := ParseDownloadOptions(raw)
	if err != nil {
		return nil, err
	}

	out, err := json.Marshal(opts)
	if err != nil {
		return nil, fmt.Errorf("tools: encode the normalised options: %w", err)
	}

	return out, nil
}

// Progress milestones, so the percentages a user watches are in one place
// rather than scattered through Run.
const (
	phaseReadingAt     = 2
	phaseDownloadFrom  = 5
	phaseDownloadTo    = 65
	phaseProcessingAt  = 70
	phaseStoringAt     = 88
	phaseStoringClipAt = 90
)

// Run implements jobs.Tool.
func (t *VideoDownload) Run(ctx context.Context, w jobs.Work) (jobs.Outcome, error) {
	opts, err := ParseDownloadOptions(w.Options)
	if err != nil {
		// Stored options that no longer validate mean the conversion was
		// accepted by an older version of this tool, so it will never
		// succeed.
		return jobs.Outcome{}, jobs.Fail(jobs.UnsupportedInput, err, "the stored options are not valid for this tool")
	}

	if err := t.refuseUnavailableOptions(opts); err != nil {
		return jobs.Outcome{}, err
	}

	if err := w.Report(
		jobs.Step{Percent: phaseReadingAt, Phase: "reading", Message: "Asking the platform."},
	); err != nil {
		return jobs.Outcome{}, err
	}

	media, err := t.down.Metadata(ctx, w.Dir, opts.URL)
	if err != nil {
		return jobs.Outcome{}, downloadFailure(err)
	}

	got, err := t.fetch(ctx, w, opts, media)
	if err != nil {
		return jobs.Outcome{}, err
	}

	if opts.Clips {
		return t.deliverClips(ctx, w, opts, got.Path, media)
	}

	path := got.Path
	cpu := got.CPUTime
	note := "Storing the result."

	if opts.Compress {
		compressed, spent, summary, err := t.compress(ctx, w, opts, path)
		if err != nil {
			return jobs.Outcome{}, err
		}

		path, note = compressed, summary
		cpu += spent
	}

	if err := w.Report(jobs.Step{Percent: phaseStoringAt, Phase: "storing", Message: note}); err != nil {
		return jobs.Outcome{}, err
	}

	stored, err := storeResult(ctx, t.files, w, path, resultName(media, opts, filepath.Ext(path)))
	if err != nil {
		return jobs.Outcome{}, err
	}

	return jobs.Outcome{BytesOut: stored, CPUMs: cpu.Milliseconds()}, nil
}

// refuseUnavailableOptions turns a missing engine into a clear refusal rather
// than a failure halfway through.
//
// It cannot happen through the API — the submit endpoint checks the same
// thing — but it can happen to a conversion that was queued before ffmpeg
// disappeared from the image, and a conversion that is going to fail should
// fail before it spends the bandwidth.
func (t *VideoDownload) refuseUnavailableOptions(opts DownloadOptions) error {
	if opts.Clips && t.edit == nil {
		return jobs.Refuse(jobs.EngineFailure, errors.New("ffmpeg is unavailable"),
			"Clips are not available on this server right now.", "the clips mode needs ffmpeg and ffprobe")
	}

	if opts.Compress && t.comp == nil {
		return jobs.Refuse(jobs.EngineFailure, errors.New("ffmpeg is unavailable"),
			"Compression is not available on this server right now.", "compression needs ffmpeg")
	}

	return nil
}

// fetch downloads the video, mapping yt-dlp's progress onto the job's.
func (t *VideoDownload) fetch(
	ctx context.Context,
	w jobs.Work,
	opts DownloadOptions,
	media downloader.Media,
) (downloader.Result, error) {
	// The report error is kept rather than returned from inside the progress
	// callback: the callback cannot fail the download by itself, and the
	// cancellation it signals already cancels the context the engine runs
	// on.
	var reportErr error

	result, err := t.down.Download(ctx, downloader.Request{
		URL:     opts.URL,
		Dir:     w.Dir,
		Kind:    downloader.Kind(opts.Kind),
		Quality: downloader.Quality(opts.Quality),
		// The estimate that matches the quality being asked for, not the
		// best-quality one: the two differ by more than an order of
		// magnitude, and checking a `smallest` request against the 4K figure
		// refuses a download that comfortably fits.
		Estimate: media.EstimateFor(downloader.Quality(opts.Quality)),
		Progress: func(p downloader.Progress) {
			if reportErr != nil {
				return
			}

			reportErr = w.Report(jobs.Step{
				Percent: scale(p.Percent(), phaseDownloadFrom, phaseDownloadTo),
				Phase:   "downloading",
				Message: downloadMessage(p),
			})
		},
	})
	if err != nil {
		return downloader.Result{}, downloadFailure(err)
	}

	if reportErr != nil {
		return downloader.Result{}, reportErr
	}

	return result, nil
}

// downloadMessage is what the user reads while the bytes arrive. It names no
// path and no argument, only sizes.
func downloadMessage(p downloader.Progress) string {
	if p.Total <= 0 {
		return "Downloading."
	}

	return "Downloading " + humanMB(p.Total) + "."
}

// humanMB renders a size in megabytes.
func humanMB(n int64) string {
	return strconv.FormatFloat(float64(n)/float64(1<<20), 'f', 1, 64) + " MB"
}

// scale maps a 0-100 percentage from an engine onto a window of the job's own
// progress. A -1 from the engine — an unknown size — becomes the window's
// start rather than a negative percentage.
func scale(percent, from, to int) int {
	if percent < 0 {
		return from
	}

	return from + percent*(to-from)/100
}

// compress re-encodes the download and keeps whichever file is smaller.
//
// It returns the path to deliver, what the engine spent and the sentence that
// says what happened, which is the ROADMAP's "delivering whichever file is
// smaller and reporting the saving".
func (t *VideoDownload) compress(
	ctx context.Context,
	w jobs.Work,
	opts DownloadOptions,
	input string,
) (string, time.Duration, string, error) {
	if err := w.Report(jobs.Step{
		Percent: phaseProcessingAt,
		Phase:   "compressing",
		Message: "Trying to make it smaller.",
	}); err != nil {
		return "", 0, "", err
	}

	request := compressor.Request{Dir: w.Dir, Input: input}

	var (
		result compressor.Result
		err    error
	)

	if opts.Kind == string(downloader.KindAudio) {
		request.Output = "compressed.mp3"
		result, err = t.comp.Audio(ctx, request)
	} else {
		request.Output = "compressed.mp4"
		result, err = t.comp.Video(ctx, request)
	}

	if err != nil {
		return "", 0, "", compressFailure(err)
	}

	return result.Path, result.CPUTime, result.Summary(), nil
}

// deliverClips splits the download, stores every clip and a zip of all of
// them, and reports the count.
//
// Both forms are stored because the ROADMAP asks for both: somebody posting
// one clip wants that file, and somebody archiving the lot wants one
// download. It costs the clips' bytes twice, which is why MaxClips exists and
// why the result expires in five minutes like everything else.
func (t *VideoDownload) deliverClips(
	ctx context.Context,
	w jobs.Work,
	opts DownloadOptions,
	input string,
	media downloader.Media,
) (jobs.Outcome, error) {
	if err := w.Report(jobs.Step{
		Percent: phaseProcessingAt,
		Phase:   "splitting",
		Message: "Cutting it into clips.",
	}); err != nil {
		return jobs.Outcome{}, err
	}

	clips, err := t.edit.Segment(ctx, editor.SegmentRequest{
		Dir:     w.Dir,
		Input:   input,
		Segment: opts.clipLength(),
	})
	if err != nil {
		return jobs.Outcome{}, editFailure(err)
	}

	if len(clips) > MaxClips {
		return jobs.Outcome{}, jobs.Refuse(jobs.InputTooLarge, errors.New("too many clips"),
			fmt.Sprintf("That video splits into %d clips, and this tool delivers at most %d. "+
				"Ask for longer clips, or trim it first.", len(clips), MaxClips),
			fmt.Sprintf("%d clips exceed the %d cap", len(clips), MaxClips))
	}

	stem := resultStem(media, opts)

	var total int64

	for i := range clips {
		if err := w.Report(jobs.Step{
			Percent: scale(i*100/len(clips), phaseStoringClipAt, 99),
			Phase:   "storing",
			Message: fmt.Sprintf("Storing clip %d of %d.", i+1, len(clips)),
		}); err != nil {
			return jobs.Outcome{}, err
		}

		name := fmt.Sprintf("%s-clip-%02d.mp4", stem, i+1)

		stored, err := storeResult(ctx, t.files, w, clips[i].Path, name)
		if err != nil {
			return jobs.Outcome{}, err
		}

		total += stored
	}

	archive, err := t.zipClips(w.Dir, clips)
	if err != nil {
		return jobs.Outcome{}, err
	}

	stored, err := storeResult(ctx, t.files, w, archive, stem+"-clips.zip")
	if err != nil {
		return jobs.Outcome{}, err
	}

	return jobs.Outcome{BytesOut: total + stored}, nil
}

// zipClips packs the clips into one archive and returns its path.
//
// Each clip is streamed into the archive with io.Copy rather than read into
// memory: sixty clips of a minute each is gigabytes, and this runs on a small
// container. The entries are stored rather than deflated, because an H.264
// stream is already compressed and deflate would spend CPU to add bytes.
func (t *VideoDownload) zipClips(dir string, clips []editor.Clip) (string, error) {
	path := filepath.Join(dir, "clips.zip")

	out, err := os.Create(path)
	if err != nil {
		return "", jobs.Fail(jobs.DiskFull, err, "create the clip archive")
	}

	defer func() { _ = out.Close() }()

	archive := zip.NewWriter(out)

	for i := range clips {
		if err := addToZip(archive, clips[i]); err != nil {
			return "", err
		}
	}

	if err := archive.Close(); err != nil {
		return "", jobs.Fail(jobs.DiskFull, err, "finish the clip archive")
	}

	if err := out.Close(); err != nil {
		return "", jobs.Fail(jobs.DiskFull, err, "close the clip archive")
	}

	return path, nil
}

// addToZip streams one clip into the archive.
func addToZip(archive *zip.Writer, clip editor.Clip) error {
	entry, err := archive.CreateHeader(&zip.FileHeader{Name: clip.Name, Method: zip.Store})
	if err != nil {
		return jobs.Fail(jobs.Internal, err, "add a clip to the archive")
	}

	body, err := os.Open(clip.Path)
	if err != nil {
		return jobs.Fail(jobs.Internal, err, "open a clip to archive it")
	}

	defer func() { _ = body.Close() }()

	if _, err := io.Copy(entry, body); err != nil {
		return jobs.Fail(jobs.DiskFull, err, "write a clip into the archive")
	}

	return nil
}

// resultStem is the file name stem a download is saved under: the video's
// own title, reduced to something a filesystem and a zip entry can hold.
func resultStem(media downloader.Media, opts DownloadOptions) string {
	stem := safeStem(media.Title)
	if stem == "" {
		stem = opts.Kind
	}

	return stem
}

// resultName is the stem plus the extension the engine chose.
func resultName(media downloader.Media, opts DownloadOptions, ext string) string {
	if ext == "" {
		ext = ".mp4"
		if opts.Kind == string(downloader.KindAudio) {
			ext = ".mp3"
		}
	}

	return resultStem(media, opts) + ext
}

// maxStem bounds the name taken from a video's title. Long enough to be
// recognisable, short enough that the stem plus a suffix plus an extension
// stays inside every filesystem's limit.
const maxStem = 60

// safeStem reduces a platform-supplied title to a file name stem.
//
// A title is free text from a stranger: it can hold slashes, quotes, null
// bytes, right-to-left overrides and a thousand characters of emoji. Only
// letters, digits and a few separators survive, which is both safe and what
// anybody would have named the file anyway.
func safeStem(title string) string {
	var b strings.Builder

	lastDash := true

	for _, r := range title {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r == ' ' || r == '-' || r == '_' || r == '.':
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}

		if b.Len() >= maxStem {
			break
		}
	}

	return strings.Trim(b.String(), "-")
}

// downloadFailure maps a downloader failure onto the job error taxonomy,
// keeping the sentence the downloader chose.
//
// The mapping is lossy by design: the taxonomy is the stable public contract
// and has ten codes, while the downloader distinguishes a private video from
// an age-restricted one. The code is what a client branches on; the sentence
// is where the difference survives, which is why every one of these passes
// Public through.
func downloadFailure(err error) error {
	f := downloader.Classify(err)
	if f == nil {
		return nil
	}

	return jobs.Refuse(downloadErrorType(f.Reason), f, f.Public, f.Detail)
}

// downloadErrorType is the taxonomy code for a download failure.
func downloadErrorType(reason downloader.Reason) jobs.ErrorType {
	switch reason {
	case downloader.ReasonUnsupportedURL,
		downloader.ReasonPrivate,
		downloader.ReasonAgeRestricted,
		downloader.ReasonUnavailable:
		return jobs.UnsupportedInput
	case downloader.ReasonTooLarge:
		return jobs.InputTooLarge
	case downloader.ReasonDiskFull:
		return jobs.DiskFull
	case downloader.ReasonOutOfMemory:
		return jobs.OutOfMemory
	case downloader.ReasonTimeout:
		return jobs.Timeout
	case downloader.ReasonCancelled:
		return jobs.CancelledByUser
	case downloader.ReasonPlatformBlock:
		// engine_failure rather than a retryable code on purpose. A platform
		// that just refused us will refuse the next two attempts as well,
		// and hammering it is how a soft block becomes a hard one. The
		// sentence tells the user to try again later, which is the right
		// place for the retry to be decided.
		return jobs.EngineFailure
	case downloader.ReasonEngine:
		return jobs.EngineFailure
	default:
		return jobs.EngineFailure
	}
}
