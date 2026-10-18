package apiv1

import (
	"encoding/json"
	"slices"

	"github.com/AlexGalhardo/vukem/web"

	"github.com/AlexGalhardo/vukem-tools/app/tools"
	"github.com/AlexGalhardo/vukem-tools/internal/editor"
	"github.com/AlexGalhardo/vukem-tools/internal/engine"
	"github.com/AlexGalhardo/vukem-tools/internal/jobs"
)

// SubmitVideoClip is the body of POST /api/v1/tools/video-clip.
type SubmitVideoClip struct {
	// FileID is an upload of this caller's that is not already attached to
	// a conversion.
	FileID string `json:"file_id" validate:"required,trim" doc:"The id of an upload of yours, from the resumable upload endpoint."`
	// IdempotencyKey makes a repeated submission return the first
	// conversion instead of starting a second one.
	IdempotencyKey string `json:"idempotency_key" validate:"trim,max=128" doc:"Repeat a submission safely: the same key returns the same conversion."`
	// Options are the tool's own options. Every field is optional and has a
	// documented default.
	Options tools.ClipOptions `json:"options" doc:"How to cut. Every field is optional."`
}

// VideoClip submits a cut or a split: POST /api/v1/tools/video-clip.
//
// The crop is not validated against the video here, only its shape is. The
// real frame size is ffprobe's to report, and a crop checked against
// anything the client said would not be checked at all — so the refusal that
// names the real dimensions arrives on the conversion rather than in this
// response.
func (h *Conversions) VideoClip(c *web.Ctx, in SubmitVideoClip) (ConversionView, error) {
	opts, err := in.Options.Normalise()
	if err != nil {
		return ConversionView{}, err
	}

	encoded, err := json.Marshal(opts)
	if err != nil {
		return ConversionView{}, err
	}

	return h.submit(c, submission{
		Tool:           "video-clip",
		FileID:         in.FileID,
		IdempotencyKey: in.IdempotencyKey,
		Options:        encoded,
	})
}

// SubmitVideoCompress is the body of POST /api/v1/tools/video-compress.
//
// It carries no options: a compression is "make this smaller", and the knobs
// that could be exposed — the CRF, the bitrate — are numbers nobody outside
// internal/compressor should have to choose. The profile is picked from the
// streams ffprobe found, not from what the upload claims to be.
type SubmitVideoCompress struct {
	FileID         string `json:"file_id"         validate:"required,trim" doc:"The id of an upload of yours, from the resumable upload endpoint."`
	IdempotencyKey string `json:"idempotency_key" validate:"trim,max=128"  doc:"Repeat a submission safely: the same key returns the same conversion."`
}

// VideoCompress submits a compression: POST /api/v1/tools/video-compress.
func (h *Conversions) VideoCompress(c *web.Ctx, in SubmitVideoCompress) (ConversionView, error) {
	return h.submit(c, submission{
		Tool:           "video-compress",
		FileID:         in.FileID,
		IdempotencyKey: in.IdempotencyKey,
		Options:        json.RawMessage("{}"),
	})
}

// EngineView is one external program and whether this server actually has it.
type EngineView struct {
	Name    string `json:"name"    doc:"The engine, as the configuration names it."`
	Present bool   `json:"present" doc:"Whether it was resolved at startup."`
	// Version is what the program reported when it was resolved, empty when
	// it is absent. It is the figure to check a deploy against what the
	// Dockerfile pinned.
	Version string `json:"version" doc:"The version it reported, empty when absent."`
}

// LimitsView is what this server will and will not do, in numbers.
type LimitsView struct {
	MaxDownloadBytes int64 `json:"max_download_bytes" doc:"The largest download allowed, 0 for no cap."`
	MaxClipSeconds   int   `json:"max_clip_seconds"   doc:"The longest clip a split produces."`
	MaxClips         int   `json:"max_clips"          doc:"The most clips one conversion delivers."`
	MaxOutputSeconds int   `json:"max_output_seconds" doc:"The longest output a cut produces, 0 for no cap."`
}

// CapabilitiesView is the answer to GET /api/v1/capabilities.
type CapabilitiesView struct {
	// Engines is every external program this service can use, present or
	// not. It is the honest answer to "why can I not see that tool": an
	// engine the image does not carry hides its tools.
	Engines []EngineView `json:"engines" doc:"Every external program, and whether this server has it."`
	// Tools names the conversions that are actually registered, which is
	// the same list GET /api/v1/tools returns, repeated here so one request
	// answers the whole question.
	Tools  []string   `json:"tools"  doc:"Every tool that can be submitted."`
	Limits LimitsView `json:"limits" doc:"The numbers this server enforces."`
}

// Capabilities reports which engines are present and what the limits are.
//
// It is a handler of its own rather than a method on Conversions because it
// is the one endpoint that needs the engine registry: everything else works
// from the pipeline, and a conversion handler that could read the registry
// would be a conversion handler that could leak a path.
type Capabilities struct {
	engines *engine.Registry
	pipe    *jobs.Pipeline
}

// NewCapabilities returns the capabilities handler.
func NewCapabilities(engines *engine.Registry, pipe *jobs.Pipeline) *Capabilities {
	return &Capabilities{engines: engines, pipe: pipe}
}

// declaredEngines is the order the endpoint lists engines in. It is written
// out rather than read from the registry so an engine that is missing is
// still *listed* as missing: the registry only knows what it found.
var declaredEngines = []string{"yt-dlp", "ffmpeg", "ffprobe", "gs"}

// Show answers GET /api/v1/capabilities.
func (h *Capabilities) Show(_ *web.Ctx, _ struct{}) (CapabilitiesView, error) {
	out := CapabilitiesView{
		Engines: make([]EngineView, 0, len(declaredEngines)),
		Tools:   h.pipe.Tools(),
		Limits: LimitsView{
			MaxClipSeconds: int(editor.MaxClip.Seconds()),
			MaxClips:       tools.MaxClips,
		},
	}

	for _, name := range declaredEngines {
		out.Engines = append(out.Engines, EngineView{
			Name:    name,
			Present: h.engines != nil && h.engines.Has(name),
			Version: h.version(name),
		})
	}

	if download, ok := h.pipe.Tool("video-download"); ok {
		if tool, ok := download.(*tools.VideoDownload); ok {
			out.Limits.MaxDownloadBytes = tool.MaxBytes()
		}
	}

	if clip, ok := h.pipe.Tool("video-clip"); ok {
		if tool, ok := clip.(*tools.VideoClip); ok {
			out.Limits.MaxOutputSeconds = int(tool.MaxOutputDuration().Seconds())
		}
	}

	return out, nil
}

// version is what an engine reported, or "" when it is absent.
func (h *Capabilities) version(name string) string {
	if h.engines == nil || !h.engines.Has(name) {
		return ""
	}

	return h.engines.Version(name)
}

// ClipModes lists the cut modes: GET /api/v1/clip-modes.
//
// Published so the interface and any client read the same list, rather than
// each repeating it and drifting when one is added.
func (*Conversions) ClipModes(_ *web.Ctx, _ struct{}) (NameList, error) {
	return NameList{Data: slices.Clone(tools.ClipModes())}, nil
}

// NameList is a plain list of names, for the small catalogue endpoints.
type NameList struct {
	Data []string `json:"data" doc:"The accepted values, in the order to show them."`
}
