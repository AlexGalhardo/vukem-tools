package apiv1

import (
	"encoding/json"
	"net/http"

	"github.com/AlexGalhardo/vukem/errs"
	"github.com/AlexGalhardo/vukem/web"

	"github.com/AlexGalhardo/vukem-tools/app/tools"
	"github.com/AlexGalhardo/vukem-tools/internal/downloader"
	"github.com/AlexGalhardo/vukem-tools/internal/jobs"
)

// The download routes are their own file because a download has no upload.
//
// Every other conversion starts from a file the caller already put on the
// server, which is what Conversions.submit is built around: it finds the
// upload, checks its type against the tool and attaches it inside the
// transaction that creates the conversion. A download starts from a URL, so
// there is nothing to find, nothing to type-check and nothing to attach —
// and bytes_in is zero until yt-dlp has been somewhere. Reusing submit would
// have meant threading "no file" through every one of those steps.

// SubmitVideoDownload is the body of POST /api/v1/tools/video-download.
//
// One submit route per tool rather than a generic one carrying opaque JSON:
// the OpenAPI document then describes the options exactly, and the typed
// handler derives its schema from the Go type, which cannot be done for
// json.RawMessage or map[string]any.
type SubmitVideoDownload struct {
	// IdempotencyKey makes a repeated submission return the first
	// conversion instead of starting a second one. Scoped to the caller.
	IdempotencyKey string `json:"idempotency_key" validate:"trim,max=128" doc:"Repeat a submission safely: the same key returns the same conversion."`
	// Options are the tool's own options. Only the URL is required.
	Options tools.DownloadOptions `json:"options" doc:"What to download and how."`
}

// VideoDownload submits a download: POST /api/v1/tools/video-download.
func (h *Conversions) VideoDownload(c *web.Ctx, in SubmitVideoDownload) (ConversionView, error) {
	opts, err := in.Options.Normalise()
	if err != nil {
		return ConversionView{}, err
	}

	encoded, err := json.Marshal(opts)
	if err != nil {
		return ConversionView{}, err
	}

	return h.submitWithoutUpload(c, "video-download", encoded, in.IdempotencyKey)
}

// submitWithoutUpload creates a conversion that has no input file.
//
// It is the same path as submit — caller, ability, budget, pipeline — with
// the upload steps removed, and deliberately not a flag on submit: a
// conversion with no input and a conversion whose input failed to attach
// have to be told apart, and a shared function with a "maybe there is a
// file" branch is where that distinction goes to die.
func (h *Conversions) submitWithoutUpload(
	c *web.Ctx,
	name string,
	options json.RawMessage,
	idempotencyKey string,
) (ConversionView, error) {
	userID, err := callerID(c)
	if err != nil {
		return ConversionView{}, err
	}

	tool, ok := h.pipe.Tool(name)
	if !ok {
		// The route exists and the tool does not, which happens on an image
		// whose engine is missing: yt-dlp absent means the tool is never
		// registered. That is a server condition, not the caller's mistake.
		return ConversionView{}, errs.NewKey(http.StatusServiceUnavailable, "conversion.tool_unavailable",
			"That tool is not available right now.", nil)
	}

	if err := requireAbility(c, tool.Ability()); err != nil {
		return ConversionView{}, err
	}

	if err := h.withinBudget(c, userID); err != nil {
		return ConversionView{}, err
	}

	created, _, err := h.pipe.Submit(c.Context(), jobs.Submission{
		UserID:         userID,
		Tool:           name,
		Options:        options,
		IdempotencyKey: idempotencyKey,
		// Nothing has been read yet, so there is nothing to charge the
		// quota for on the way in. The download's real size is recorded as
		// bytes_out when it finishes.
		BytesIn: 0,
	})
	if err != nil {
		return ConversionView{}, err
	}

	return viewOf(created), nil
}

// MediaProbe is the body of POST /api/v1/tools/video-download/metadata.
type MediaProbe struct {
	URL string `json:"url" validate:"required,trim,max=2048" doc:"A YouTube, Instagram, X or TikTok URL."`
}

// MediaFormat is one of the streams a platform offers.
type MediaFormat struct {
	ID         string  `json:"id"           doc:"The platform's own format id."`
	Ext        string  `json:"ext"          doc:"Its container extension."`
	Resolution string  `json:"resolution"   doc:"Its frame size, empty for an audio-only stream."`
	VideoCodec string  `json:"vcodec"       doc:"Its video codec, or none."`
	AudioCodec string  `json:"acodec"       doc:"Its audio codec, or none."`
	Bytes      int64   `json:"bytes"        doc:"Its size, 0 when the platform does not say."`
	Bitrate    float64 `json:"bitrate_kbps" doc:"Its total bitrate in kilobits per second."`
}

// MediaView is what a platform says about a video before any of it is
// fetched.
type MediaView struct {
	Platform  string  `json:"platform"         doc:"youtube, instagram, x or tiktok."`
	Title     string  `json:"title"            doc:"The video's title, as the platform gives it."`
	Uploader  string  `json:"uploader"         doc:"Who posted it."`
	Thumbnail string  `json:"thumbnail"        doc:"A link to the platform's own thumbnail."`
	Duration  float64 `json:"duration_seconds" doc:"How long it is."`
	// EstimatedBytes is what the size cap is checked against before the
	// download starts, so a client can show the refusal before submitting.
	EstimatedBytes int64         `json:"estimated_bytes" doc:"The expected download size. The cap is checked against this."`
	MaxBytes       int64         `json:"max_bytes"       doc:"The largest download this server allows."`
	TooLarge       bool          `json:"too_large"       doc:"Whether the estimate is already past the cap."`
	Formats        []MediaFormat `json:"formats"         doc:"Every stream the platform offers."`
}

// DownloadMetadata reports what a platform says about a URL:
// POST /api/v1/tools/video-download/metadata.
//
// It is a POST rather than a GET even though it changes nothing. A URL in a
// query string ends up in every access log, in the browser's history and in
// any proxy in between, and the URL is the one piece of this request that is
// the caller's private business. It is also rate-limited like a submission,
// because each call is a real request to somebody else's server.
func (h *Conversions) DownloadMetadata(c *web.Ctx, in MediaProbe) (MediaView, error) {
	if _, err := callerID(c); err != nil {
		return MediaView{}, err
	}

	tool, err := h.downloadTool(c)
	if err != nil {
		return MediaView{}, err
	}

	if err := requireAbility(c, tool.Ability()); err != nil {
		return MediaView{}, err
	}

	media, err := tool.Metadata(c.Context(), in.URL)
	if err != nil {
		return MediaView{}, probeFailure(err)
	}

	view := MediaView{
		Platform:       media.Platform,
		Title:          media.Title,
		Uploader:       media.Uploader,
		Thumbnail:      media.Thumbnail,
		Duration:       media.Duration,
		EstimatedBytes: media.EstimatedBytes,
		MaxBytes:       tool.MaxBytes(),
		Formats:        make([]MediaFormat, 0, len(media.Formats)),
	}

	view.TooLarge = view.MaxBytes > 0 && view.EstimatedBytes > view.MaxBytes

	for i := range media.Formats {
		f := media.Formats[i]

		view.Formats = append(view.Formats, MediaFormat{
			ID:         f.ID,
			Ext:        f.Ext,
			Resolution: f.Resolution,
			VideoCodec: f.VideoCodec,
			AudioCodec: f.AudioCodec,
			Bytes:      f.Bytes,
			Bitrate:    f.Bitrate,
		})
	}

	return view, nil
}

// downloadTool returns the registered download tool, or the 503 that says
// the engine is absent.
func (h *Conversions) downloadTool(*web.Ctx) (*tools.VideoDownload, error) {
	registered, ok := h.pipe.Tool("video-download")
	if !ok {
		return nil, errs.NewKey(http.StatusServiceUnavailable, "conversion.tool_unavailable",
			"Downloading is not available right now.", nil)
	}

	tool, ok := registered.(*tools.VideoDownload)
	if !ok {
		// Another tool is registered under this name, which is a wiring
		// mistake rather than anything a caller did.
		return nil, errs.NewKey(http.StatusInternalServerError, "conversion.tool_mismatch",
			"That tool is not available right now.", nil)
	}

	return tool, nil
}

// probeFailure turns a metadata failure into the HTTP answer for it.
//
// The downloader's own sentence is used rather than the taxonomy's generic
// one: "that video is private" is something the caller can act on, and
// "unsupported media type" is not. The status comes from the taxonomy, so a
// client branching on the code still sees the same shape it does for an
// asynchronous failure.
func probeFailure(err error) error {
	f := downloader.Classify(err)
	if f == nil {
		return nil
	}

	info := jobs.Describe(downloadProbeErrorType(f.Reason))

	return errs.NewKey(info.Status, "download."+string(f.Reason), f.Public, nil)
}

// downloadProbeErrorType maps a probe failure onto the taxonomy, so the HTTP
// status of a synchronous refusal matches the error_type of the asynchronous
// one for the same cause.
func downloadProbeErrorType(reason downloader.Reason) jobs.ErrorType {
	switch reason {
	case downloader.ReasonUnsupportedURL,
		downloader.ReasonPrivate,
		downloader.ReasonAgeRestricted,
		downloader.ReasonUnavailable:
		return jobs.UnsupportedInput
	case downloader.ReasonTooLarge:
		return jobs.InputTooLarge
	case downloader.ReasonTimeout:
		return jobs.Timeout
	case downloader.ReasonCancelled:
		return jobs.CancelledByUser
	case downloader.ReasonDiskFull:
		return jobs.DiskFull
	case downloader.ReasonOutOfMemory:
		return jobs.OutOfMemory
	case downloader.ReasonPlatformBlock, downloader.ReasonEngine:
		return jobs.EngineFailure
	default:
		return jobs.EngineFailure
	}
}

// PlatformView is one platform the downloader accepts.
type PlatformView struct {
	Name  string   `json:"name"  doc:"The stable id, as a conversion reports it."`
	Label string   `json:"label" doc:"What to call it in an interface."`
	Hosts []string `json:"hosts" doc:"The domains a URL may be on."`
}

// PlatformList is the answer to GET /api/v1/download-platforms.
type PlatformList struct {
	Data []PlatformView `json:"data" doc:"Every platform a URL may belong to."`
}

// DownloadPlatforms lists the platforms downloading is allowed from:
// GET /api/v1/download-platforms.
//
// Published so a client can refuse a URL before sending it, and so the list
// is the server's to state rather than the interface's to repeat.
func (*Conversions) DownloadPlatforms(_ *web.Ctx, _ struct{}) (PlatformList, error) {
	platforms := tools.DownloadPlatforms()

	out := PlatformList{Data: make([]PlatformView, 0, len(platforms))}
	for i := range platforms {
		out.Data = append(out.Data, PlatformView{
			Name:  platforms[i].Name,
			Label: platforms[i].Label,
			Hosts: platforms[i].Hosts,
		})
	}

	return out, nil
}
