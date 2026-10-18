// Package downloader fetches a video from one of four platforms by driving
// yt-dlp.
//
// It is the only part of this service whose subprocess is allowed to reach
// the network, which is why so much of this file is about narrowing what that
// subprocess may do. The URL is matched against a closed list of platforms
// before yt-dlp is told about it, yt-dlp is restricted to that platform's own
// extractors so the `generic` extractor can never run, and the host's yt-dlp
// configuration files are ignored. Without those three, a download endpoint
// is an open proxy with a nice interface.
//
// Nothing here knows about jobs, conversions, HTTP or storage. It takes a
// directory to work in, runs an engine through internal/engine and reports
// what it produced, so it can be tested with a fake runner and no network.
package downloader

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/AlexGalhardo/vukem-tools/internal/engine"
)

// Runner runs one engine under its limits. *engine.Runner satisfies it; a
// test passes a fake.
type Runner interface {
	Run(ctx context.Context, cmd engine.Command) (engine.Result, error)
}

// Options configures a Downloader. Runner and Binary are required.
type Options struct {
	// Runner applies the timeout, the resource limits and the process-group
	// kill. Every yt-dlp invocation goes through it.
	Runner Runner
	// Binary is the absolute path of yt-dlp, from engine.Registry.
	Binary string
	// FFmpeg is the absolute path of ffmpeg. yt-dlp needs it to merge a
	// separate video and audio stream and to produce MP3. Empty means it is
	// unavailable, and the downloader then asks only for formats that need
	// no merging — a smaller, lower-quality result rather than a fetchErr.
	FFmpeg string
	// CookieFile is a Netscape cookie jar for the platforms that want a
	// signed-in session. A path that does not exist is logged and ignored
	// rather than failing every download.
	CookieFile string
	// POTokenBaseURL is the base URL of a bgutil PO-token provider. Empty
	// means none, which is normal and degrades to whatever the platform
	// allows an anonymous client.
	POTokenBaseURL string
	// SleepRequests, SleepInterval and MaxSleepInterval slow the downloader
	// down to look less like a scraper. Zero means yt-dlp's default, which
	// is no sleeping.
	SleepRequests    time.Duration
	SleepInterval    time.Duration
	MaxSleepInterval time.Duration
	// Retries is how many times yt-dlp retries a request and a fragment.
	// Zero means its default.
	Retries int
	// MaxBytes is the largest download allowed. It is checked against the
	// estimate before the download starts, handed to yt-dlp as
	// --max-filesize, and checked again on the finished file. Zero means no
	// cap, which is only right in a test.
	MaxBytes int64
	Log      *slog.Logger
}

// Downloader drives yt-dlp.
type Downloader struct {
	run     Runner
	binary  string
	ffmpeg  string
	cookies string
	potoken string

	sleepRequests time.Duration
	sleepInterval time.Duration
	maxSleep      time.Duration
	retries       int
	maxBytes      int64

	log *slog.Logger
}

// New returns a downloader.
//
// A missing cookie file is the one misconfiguration that is tolerated: the
// ROADMAP asks for its absence to degrade gracefully, because a cookie jar
// expires and a download without cookies still works for most public videos.
// Everything else that is wrong here is a construction error.
func New(o Options) (*Downloader, error) {
	switch {
	case o.Runner == nil:
		return nil, errors.New("downloader: a runner is required")
	case o.Binary == "":
		return nil, errors.New("downloader: the path of yt-dlp is required")
	}

	if o.Log == nil {
		o.Log = slog.Default()
	}

	cookies := strings.TrimSpace(o.CookieFile)
	if cookies != "" {
		if _, err := os.Stat(cookies); err != nil {
			o.Log.Warn("downloader: the cookie file is unreadable, continuing without it",
				"path", cookies, "err", err)

			cookies = ""
		}
	}

	return &Downloader{
		run:           o.Runner,
		binary:        o.Binary,
		ffmpeg:        strings.TrimSpace(o.FFmpeg),
		cookies:       cookies,
		potoken:       strings.TrimSpace(o.POTokenBaseURL),
		sleepRequests: o.SleepRequests,
		sleepInterval: o.SleepInterval,
		maxSleep:      o.MaxSleepInterval,
		retries:       o.Retries,
		maxBytes:      o.MaxBytes,
		log:           o.Log,
	}, nil
}

// MaxBytes is the size cap this downloader enforces, zero for none.
func (d *Downloader) MaxBytes() int64 { return d.maxBytes }

// Format is one of the streams a platform offers.
type Format struct {
	ID         string  `json:"id"`
	Ext        string  `json:"ext"`
	Resolution string  `json:"resolution"`
	VideoCodec string  `json:"vcodec"`
	AudioCodec string  `json:"acodec"`
	Bytes      int64   `json:"bytes"`
	Bitrate    float64 `json:"bitrate_kbps"`
}

// Media is what a platform says about a video, before any of it is fetched.
type Media struct {
	Platform  string  `json:"platform"`
	Extractor string  `json:"extractor"`
	Title     string  `json:"title"`
	Uploader  string  `json:"uploader"`
	Thumbnail string  `json:"thumbnail"`
	Duration  float64 `json:"duration_seconds"`
	// EstimatedBytes is the best available guess at the size of a
	// best-quality download: the platform's own figure when it gives one,
	// and duration times bitrate when it does not.
	EstimatedBytes int64 `json:"estimated_bytes"`
	// SmallestBytes is the same guess for the smallest stream that still
	// carries picture and sound.
	//
	// It exists because the two differ by more than an order of magnitude —
	// Big Buck Bunny is 690 MB at 4K and a few megabytes at its lowest
	// format — so checking a `smallest` request against the 4K figure would
	// refuse a download that comfortably fits. The cap has to be accurate,
	// not merely safe: refusing work that would have succeeded is a bug
	// users report as "it says the video is too large and it is not".
	SmallestBytes int64    `json:"smallest_bytes"`
	Formats       []Format `json:"formats"`
}

// EstimateFor is the size to check the cap against for a given quality.
func (m Media) EstimateFor(q Quality) int64 {
	if q == QualitySmallest && m.SmallestBytes > 0 {
		return m.SmallestBytes
	}

	return m.EstimatedBytes
}

// probePayload is the part of yt-dlp's -J output this service reads.
//
// It is a narrow view on purpose: the real document has well over a hundred
// fields, several of which are free text from the platform, and decoding only
// what is used means a platform adding a field cannot change anything here.
type probePayload struct {
	Title           string  `json:"title"`
	Uploader        string  `json:"uploader"`
	Channel         string  `json:"channel"`
	Thumbnail       string  `json:"thumbnail"`
	Extractor       string  `json:"extractor_key"`
	Duration        float64 `json:"duration"`
	FileSize        int64   `json:"filesize"`
	FileSizeApprox  int64   `json:"filesize_approx"`
	TotalBitrateKbs float64 `json:"tbr"`
	IsLive          bool    `json:"is_live"`
	Formats         []struct {
		ID             string  `json:"format_id"`
		Ext            string  `json:"ext"`
		Resolution     string  `json:"resolution"`
		VideoCodec     string  `json:"vcodec"`
		AudioCodec     string  `json:"acodec"`
		FileSize       int64   `json:"filesize"`
		FileSizeApprox int64   `json:"filesize_approx"`
		Bitrate        float64 `json:"tbr"`
	} `json:"formats"`
}

// Metadata asks the platform what it has, without downloading anything.
//
// dir is the working directory the engine runs in: yt-dlp writes nothing
// here, but engine.Runner requires one and a program that inherits the
// server's working directory is a program that can write next to the binary.
func (d *Downloader) Metadata(ctx context.Context, dir, rawURL string) (Media, error) {
	platform, err := PlatformOf(rawURL)
	if err != nil {
		return Media{}, fail(ReasonUnsupportedURL,
			"That URL is not one this service can download from. YouTube, Instagram, X and TikTok are supported.",
			err.Error(), err)
	}

	var out strings.Builder

	args := append(d.commonArgs(platform), "--dump-single-json", "--skip-download", "--", rawURL)

	if _, err := d.run.Run(ctx, engine.Command{
		Path:   d.binary,
		Args:   args,
		Dir:    dir,
		Stdout: &out,
	}); err != nil {
		return Media{}, Classify(err)
	}

	var payload probePayload
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &payload); err != nil {
		return Media{}, fail(ReasonEngine, "That video's details could not be read.",
			"decode yt-dlp's JSON: "+err.Error(), err)
	}

	if payload.IsLive {
		return Media{}, fail(ReasonUnsupportedURL, "That is a live stream, which cannot be downloaded.",
			"the platform reports a live stream", nil)
	}

	return mediaOf(platform, payload), nil
}

// mediaOf turns yt-dlp's document into the view this service publishes.
func mediaOf(platform Platform, p probePayload) Media {
	uploader := p.Uploader
	if uploader == "" {
		uploader = p.Channel
	}

	m := Media{
		Platform:       platform.Name,
		Extractor:      p.Extractor,
		Title:          p.Title,
		Uploader:       uploader,
		Thumbnail:      p.Thumbnail,
		Duration:       p.Duration,
		EstimatedBytes: estimateBytes(p),
		SmallestBytes:  smallestBytes(p),
		Formats:        make([]Format, 0, len(p.Formats)),
	}

	for i := range p.Formats {
		f := p.Formats[i]

		m.Formats = append(m.Formats, Format{
			ID:         f.ID,
			Ext:        f.Ext,
			Resolution: f.Resolution,
			VideoCodec: f.VideoCodec,
			AudioCodec: f.AudioCodec,
			Bytes:      max(f.FileSize, f.FileSizeApprox),
			Bitrate:    f.Bitrate,
		})
	}

	return m
}

// estimateBytes is the size the download is expected to be.
//
// The platform's own figure is preferred, then the largest figure any single
// format reported, and only then duration times bitrate. The arithmetic
// fallback matters more than it looks: YouTube reports no size at all for the
// adaptive formats that produce the best quality, so without it the size cap
// would never fire before the download rather than always.
func estimateBytes(p probePayload) int64 {
	if n := max(p.FileSize, p.FileSizeApprox); n > 0 {
		return n
	}

	var largest int64
	for i := range p.Formats {
		largest = max(largest, p.Formats[i].FileSize, p.Formats[i].FileSizeApprox)
	}

	if largest > 0 {
		return largest
	}

	bitrate := p.TotalBitrateKbs
	for i := range p.Formats {
		bitrate = max(bitrate, p.Formats[i].Bitrate)
	}

	if bitrate <= 0 || p.Duration <= 0 {
		return 0
	}

	// tbr is in kilobits per second, so a kilobit is 125 bytes.
	return int64(bitrate * p.Duration * 125)
}

// smallestBytes is the size of the smallest stream that carries both picture
// and sound, or the smallest of anything when none does.
//
// Only declared sizes are considered: a format with no size is skipped rather
// than estimated, because the arithmetic fallback uses the *highest* bitrate
// on offer and would therefore make the "smallest" figure the largest one.
// Zero means nothing could be measured, and EstimateFor then falls back to
// the best-quality figure, which is the safe direction.
func smallestBytes(p probePayload) int64 {
	var (
		smallest int64
		found    bool
	)

	for i := range p.Formats {
		f := p.Formats[i]

		size := max(f.FileSize, f.FileSizeApprox)
		if size <= 0 {
			continue
		}

		// A pre-muxed format, which is what the `smallest` selector prefers:
		// "none" is what yt-dlp reports for an absent stream.
		muxed := f.VideoCodec != "" && f.VideoCodec != "none" &&
			f.AudioCodec != "" && f.AudioCodec != "none"
		if !muxed {
			continue
		}

		if !found || size < smallest {
			smallest, found = size, true
		}
	}

	return smallest
}

// Kind is what to keep from a video.
type Kind string

// The kinds a request may ask for.
const (
	// KindVideo keeps the picture and the sound, muxed into MP4.
	KindVideo Kind = "video"
	// KindAudio keeps only the sound, as MP3.
	KindAudio Kind = "audio"
)

// Valid reports whether k is one of the two kinds.
func (k Kind) Valid() bool { return k == KindVideo || k == KindAudio }

// Quality is which of a platform's streams to take.
type Quality string

// The qualities a request may ask for.
const (
	// QualityBest is the best the platform offers within the size cap.
	QualityBest Quality = "best"
	// QualitySmallest is the smallest stream that still has both picture and
	// sound, for somebody on a phone connection.
	QualitySmallest Quality = "smallest"
)

// Valid reports whether q is one of the two qualities.
func (q Quality) Valid() bool { return q == QualityBest || q == QualitySmallest }

// Request is one download.
type Request struct {
	// URL is the page to download from.
	URL string
	// Dir is the working directory. The result is written inside it and
	// nothing else is touched.
	Dir string
	// Kind and Quality say what to fetch.
	Kind    Kind
	Quality Quality
	// Estimate, when positive, is the size Metadata predicted. It is
	// checked against the cap before yt-dlp is started, which is the only
	// way to refuse a download without paying for it first.
	Estimate int64
	// Progress receives each structured progress update. Optional.
	Progress func(Progress)
}

// Result is a finished download.
type Result struct {
	// Path is the absolute path of the file, inside Request.Dir.
	Path string
	// Bytes is its size.
	Bytes int64
	// CPUTime is what yt-dlp and the ffmpeg it drove actually used.
	CPUTime time.Duration
}

// Download fetches the video and returns the file it produced.
func (d *Downloader) Download(ctx context.Context, in Request) (Result, error) {
	platform, err := PlatformOf(in.URL)
	if err != nil {
		return Result{}, fail(ReasonUnsupportedURL,
			"That URL is not one this service can download from. YouTube, Instagram, X and TikTok are supported.",
			err.Error(), err)
	}

	if in.Dir == "" {
		return Result{}, fail(ReasonEngine, "That video could not be downloaded.",
			"a download needs its own working directory", nil)
	}

	if !in.Kind.Valid() {
		in.Kind = KindVideo
	}

	if !in.Quality.Valid() {
		in.Quality = QualityBest
	}

	// Refused from the estimate, before a byte is fetched. Doing it after
	// would mean paying for the bandwidth to learn what the metadata already
	// said.
	if d.maxBytes > 0 && in.Estimate > d.maxBytes {
		return Result{}, fail(ReasonTooLarge, sizeRefusal(in.Estimate, d.maxBytes),
			fmt.Sprintf("estimate %d bytes is past the %d byte cap", in.Estimate, d.maxBytes), nil)
	}

	args := append(d.commonArgs(platform), d.formatArgs(in.Kind, in.Quality)...)
	args = append(args,
		// --newline makes each update its own line instead of a redraw, and
		// --progress is not optional next to the --quiet in commonArgs:
		// yt-dlp documents it as "show progress bar, even if in quiet mode",
		// and without it --quiet suppresses the progress template entirely.
		// That failure is silent — the download succeeds and the user watches
		// a bar that never moves — so it is worth the one argument and the
		// test that pins it.
		"--newline",
		"--progress",
		"--progress-template", progressTemplate,
		"-o", outputTemplate,
		"--", in.URL,
	)

	result, err := d.run.Run(ctx, engine.Command{
		Path:     d.binary,
		Args:     args,
		Dir:      in.Dir,
		Progress: progressReader(in.Progress),
	})
	if err != nil {
		return Result{}, Classify(err)
	}

	path, size, err := locateOutput(in.Dir)
	if err != nil {
		return Result{}, err
	}

	// Checked again on the finished file: --max-filesize only fires when the
	// platform declared a size, and the estimate is an estimate.
	if d.maxBytes > 0 && size > d.maxBytes {
		return Result{}, fail(ReasonTooLarge, sizeRefusal(size, d.maxBytes),
			fmt.Sprintf("the finished file is %d bytes, past the %d byte cap", size, d.maxBytes), nil)
	}

	return Result{Path: path, Bytes: size, CPUTime: result.CPUTime}, nil
}

// outputTemplate names the file yt-dlp writes. A fixed stem keeps the result
// findable whatever the platform called the video, and keeps a title full of
// slashes, quotes and right-to-left overrides from reaching the filesystem.
const outputTemplate = "download.%(ext)s"

// sizeRefusal is the sentence a user reads when a download is too large.
func sizeRefusal(size, limit int64) string {
	return fmt.Sprintf("That video is about %s, and this service downloads at most %s.",
		humanBytes(size), humanBytes(limit))
}

// humanBytes renders a size the way a person reads it.
func humanBytes(n int64) string {
	const unit = 1 << 20

	if n < unit {
		return strconv.FormatInt(max(n, 0), 10) + " bytes"
	}

	return strconv.FormatFloat(float64(n)/float64(unit), 'f', 1, 64) + " MB"
}

// commonArgs are the arguments every invocation gets, in a stable order so a
// test can assert on them.
//
// --ignore-config is not decoration: yt-dlp reads /etc/yt-dlp.conf and
// ~/.config/yt-dlp/config unless told not to, and a configuration file on the
// host could re-enable the generic extractor that the whole platform list
// exists to keep switched off.
func (d *Downloader) commonArgs(platform Platform) []string {
	args := []string{
		"--ignore-config",
		"--no-playlist",
		"--no-warnings",
		"--no-mtime",
		"--no-color",
		"--quiet",
		"--use-extractors", strings.Join(platform.Extractors, ","),
	}

	if d.retries > 0 {
		args = append(args,
			"--retries", strconv.Itoa(d.retries),
			"--fragment-retries", strconv.Itoa(d.retries),
		)
	}

	if d.sleepRequests > 0 {
		args = append(args, "--sleep-requests", seconds(d.sleepRequests))
	}

	if d.sleepInterval > 0 {
		args = append(args, "--sleep-interval", seconds(d.sleepInterval))
	}

	if d.maxSleep > 0 {
		args = append(args, "--max-sleep-interval", seconds(d.maxSleep))
	}

	if d.maxBytes > 0 {
		args = append(args, "--max-filesize", strconv.FormatInt(d.maxBytes, 10))
	}

	if d.cookies != "" {
		args = append(args, "--cookies", d.cookies)
	}

	if d.potoken != "" {
		// The provider is a sidecar HTTP service; yt-dlp reaches it through
		// the YouTube extractor's own argument.
		args = append(args, "--extractor-args", "youtube:getpot_bgutil_baseurl="+d.potoken)
	}

	if d.ffmpeg != "" {
		args = append(args, "--ffmpeg-location", d.ffmpeg)
	}

	return args
}

// seconds renders a duration the way yt-dlp's sleep options want it.
func seconds(d time.Duration) string {
	return strconv.FormatFloat(d.Seconds(), 'f', -1, 64)
}

// formatArgs chooses the streams to fetch.
//
// Without ffmpeg there is nothing to merge a separate video and audio stream
// with, so the selectors fall back to the pre-muxed formats a platform
// offers. That is a worse result, not a fetchErr: a 720p file beats an error.
func (d *Downloader) formatArgs(kind Kind, quality Quality) []string {
	if kind == KindAudio {
		if d.ffmpeg == "" {
			// No ffmpeg means no transcode, so take the smallest audio-only
			// stream as it comes and let the caller see its real type.
			return []string{"-f", "ba/b"}
		}

		return []string{
			"-f", "ba/b",
			"--extract-audio",
			"--audio-format", "mp3",
			// 2 is libmp3lame's VBR quality, around 190 kbps: audibly
			// transparent for speech and music at a fraction of 320.
			"--audio-quality", "2",
		}
	}

	if d.ffmpeg == "" {
		if quality == QualitySmallest {
			return []string{"-f", "w[ext=mp4]/w"}
		}

		return []string{"-f", "b[ext=mp4]/b"}
	}

	if quality == QualitySmallest {
		return []string{
			"-f", "wv*[ext=mp4]+wa[ext=m4a]/w[ext=mp4]/wv*+wa/w",
			"--merge-output-format", "mp4",
		}
	}

	return []string{
		"-f", "bv*[ext=mp4]+ba[ext=m4a]/b[ext=mp4]/bv*+ba/b",
		"--merge-output-format", "mp4",
	}
}

// locateOutput finds the file yt-dlp wrote.
//
// The extension is yt-dlp's to choose — the output template says so — and the
// partial files it leaves behind must not be mistaken for a result, which is
// what the suffix skips are for.
func locateOutput(dir string) (string, int64, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", 0, fail(ReasonEngine, "That video could not be downloaded.",
			"read the working directory: "+err.Error(), err)
	}

	var (
		best     string
		bestSize int64
	)

	for _, e := range entries {
		name := e.Name()

		if e.IsDir() || !strings.HasPrefix(name, "download.") || partial(name) {
			continue
		}

		info, err := e.Info()
		if err != nil {
			continue
		}

		// The largest match, so a leftover thumbnail or subtitle file cannot
		// be returned as the video.
		if info.Size() > bestSize {
			best, bestSize = filepath.Join(dir, name), info.Size()
		}
	}

	if best == "" {
		return "", 0, fail(ReasonEngine, "That video could not be downloaded.",
			"yt-dlp exited 0 but wrote no file", nil)
	}

	return best, bestSize, nil
}

// partial reports whether a name is one of yt-dlp's intermediate files.
func partial(name string) bool {
	for _, suffix := range []string{".part", ".ytdl", ".temp", ".tmp"} {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}

	return false
}
