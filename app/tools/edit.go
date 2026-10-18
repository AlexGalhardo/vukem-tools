package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/AlexGalhardo/vukem/z"

	"github.com/AlexGalhardo/vukem-tools/app/abilities"
	"github.com/AlexGalhardo/vukem-tools/app/models"
	"github.com/AlexGalhardo/vukem-tools/internal/compressor"
	"github.com/AlexGalhardo/vukem-tools/internal/detect"
	"github.com/AlexGalhardo/vukem-tools/internal/editor"
	"github.com/AlexGalhardo/vukem-tools/internal/files"
	"github.com/AlexGalhardo/vukem-tools/internal/jobs"
)

// editTools is the cutting, cropping and compression domain (S3.2 and S3.3).
//
// Both tools need ffmpeg and ffprobe, so neither is registered without them:
// a tool whose engine is missing would accept submissions and fail every one
// of them, and the catalogue showing it as planned is the honest answer for
// an image that cannot run it.
func editTools(f *files.Store, e Engines) []jobs.Tool {
	edit := newEditor(e)
	if edit == nil {
		return nil
	}

	registered := []jobs.Tool{NewVideoClip(f, edit)}

	if comp := newCompressor(e); comp != nil {
		registered = append(registered, NewVideoCompress(f, edit, comp))
	}

	return registered
}

// newEditor builds the editor, or nil when either engine is missing.
func newEditor(e Engines) *editor.Editor {
	if !e.available("ffmpeg", "ffprobe") {
		return nil
	}

	built, err := editor.New(editor.Options{
		Runner:            e.Runner,
		FFmpeg:            e.path("ffmpeg"),
		FFprobe:           e.path("ffprobe"),
		MaxOutputDuration: e.minutes(settingMaxEditMinutes, defaultMaxEditMinutes),
	})
	if err != nil {
		return nil
	}

	return built
}

// newCompressor builds the compressor, or nil when ffmpeg is missing.
//
// Ghostscript is optional here: without it the PDF path reports itself
// unavailable and the video and audio paths still work.
func newCompressor(e Engines) *compressor.Compressor {
	if !e.available("ffmpeg") {
		return nil
	}

	built, err := compressor.New(compressor.Options{
		Runner:      e.Runner,
		FFmpeg:      e.path("ffmpeg"),
		Ghostscript: e.path("gs"),
		Prober:      newEditor(e),
	})
	if err != nil {
		return nil
	}

	return built
}

// editedVideoTypes are the containers the cut and compress tools read.
//
// It is the one list, shared by both tools, because a file this service can
// cut is a file it can compress: both are ffmpeg reading a container, and two
// lists would drift.
func editedVideoTypes() []string {
	return []string{
		"video/mp4",
		"video/webm",
		"video/x-matroska",
		"video/quicktime",
		"video/x-msvideo",
	}
}

// VideoClip trims and crops a video, or splits it into clips.
type VideoClip struct {
	files *files.Store
	edit  *editor.Editor
}

// NewVideoClip returns the tool.
func NewVideoClip(f *files.Store, e *editor.Editor) *VideoClip {
	return &VideoClip{files: f, edit: e}
}

// Name implements jobs.Tool.
func (*VideoClip) Name() string { return "video-clip" }

// Ability implements jobs.Tool.
func (*VideoClip) Ability() string { return abilities.VideoEdit }

// Timeout implements jobs.Tool.
//
// Ten minutes. A copied cut is seconds and a re-encode of the longest output
// this tool allows is minutes, so anything approaching this is a stuck
// worker rather than slow work.
func (*VideoClip) Timeout() time.Duration { return 10 * time.Minute }

// Accepted implements jobs.Accepter.
func (*VideoClip) Accepted() []string { return editedVideoTypes() }

// MaxOutputDuration is the longest output a cut may produce, zero for no
// cap. The capabilities endpoint publishes it so a client can refuse a range
// before submitting it.
func (t *VideoClip) MaxOutputDuration() time.Duration { return t.edit.MaxOutputDuration() }

// The modes video-clip offers.
const (
	// ClipModeTrim keeps one range, optionally cropped.
	ClipModeTrim = "trim"
	// ClipModeSplit cuts the whole video into consecutive clips.
	ClipModeSplit = "split"
)

var clipModes = []string{ClipModeTrim, ClipModeSplit}

// ClipModes returns the accepted modes, for the documentation and the
// interface.
func ClipModes() []string { return slices.Clone(clipModes) }

// ClipOptions is the validated input of a video-clip conversion.
type ClipOptions struct {
	// Mode is trim or split.
	Mode string `json:"mode" validate:"trim,max=16" doc:"trim keeps one range, split cuts the whole video into clips. Default trim."`
	// StartSeconds is where a trim begins.
	StartSeconds float64 `json:"start_seconds" validate:"min=0" doc:"Where the cut starts, in seconds. Default 0."`
	// DurationSeconds is how much a trim keeps. 0 means to the end.
	DurationSeconds float64 `json:"duration_seconds" validate:"min=0" doc:"How much to keep, in seconds. 0 means to the end."`
	// FrameAccurate forces a re-encode so the cut lands exactly on
	// start_seconds rather than on the keyframe before it.
	FrameAccurate bool `json:"frame_accurate" doc:"Cut exactly, by re-encoding. Slower; off by default, which cuts at the nearest keyframe."`
	// Crop is the area to keep. All four zero means no crop.
	Crop editor.Crop `json:"crop" doc:"The area to keep, in pixels. Leave it out to keep the whole frame."`
	// ClipSeconds is the clip length for split mode, at most 60.
	ClipSeconds int `json:"clip_seconds" validate:"min=0,max=60" doc:"Clip length in seconds for split mode, 1 to 60. Default 60."`
}

// Normalise fills in every omitted field and refuses a value that is not one
// of the documented ones.
//
// What it cannot check is the crop against the frame and the start against
// the duration: both need the real stream, which only ffprobe knows. Those
// are checked in internal/editor, at the point where the numbers are real.
func (in ClipOptions) Normalise() (ClipOptions, error) {
	if in.Mode = strings.TrimSpace(in.Mode); in.Mode == "" {
		in.Mode = ClipModeTrim
	}

	if !slices.Contains(clipModes, in.Mode) {
		return ClipOptions{}, z.FieldError("options.mode",
			"Unknown mode. Use one of: "+strings.Join(clipModes, ", ")+".")
	}

	if in.StartSeconds < 0 {
		return ClipOptions{}, z.FieldError("options.start_seconds", "The start cannot be negative.")
	}

	if in.DurationSeconds < 0 {
		return ClipOptions{}, z.FieldError("options.duration_seconds", "The duration cannot be negative.")
	}

	if in.ClipSeconds < 0 || time.Duration(in.ClipSeconds)*time.Second > editor.MaxClip {
		return ClipOptions{}, z.FieldError("options.clip_seconds",
			fmt.Sprintf("A clip is between 1 and %d seconds.", int(editor.MaxClip.Seconds())))
	}

	if err := validateCropShape(in.Crop); err != nil {
		return ClipOptions{}, err
	}

	return in, nil
}

// validateCropShape refuses a crop that cannot be valid for any video, which
// is the part that does not need the stream.
func validateCropShape(crop editor.Crop) error {
	if crop.Zero() {
		return nil
	}

	if crop.X < 0 || crop.Y < 0 {
		return z.FieldError("options.crop", "The crop origin cannot be negative.")
	}

	if crop.Width <= 0 || crop.Height <= 0 {
		return z.FieldError("options.crop", "The crop needs a width and a height above zero.")
	}

	return nil
}

// start and duration render the options as durations.
func (in ClipOptions) start() time.Duration {
	return time.Duration(in.StartSeconds * float64(time.Second))
}

func (in ClipOptions) duration() time.Duration {
	return time.Duration(in.DurationSeconds * float64(time.Second))
}

func (in ClipOptions) clipLength() time.Duration {
	if in.ClipSeconds <= 0 {
		return editor.MaxClip
	}

	return time.Duration(in.ClipSeconds) * time.Second
}

// ParseClipOptions validates and decodes the options stored on a conversion.
func ParseClipOptions(raw json.RawMessage) (ClipOptions, error) {
	var in ClipOptions

	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &in); err != nil {
			return ClipOptions{}, z.FieldError("options", "The options are not a JSON object.")
		}
	}

	return in.Normalise()
}

// ValidateOptions implements jobs.OptionsValidator.
func (*VideoClip) ValidateOptions(raw json.RawMessage) (json.RawMessage, error) {
	opts, err := ParseClipOptions(raw)
	if err != nil {
		return nil, err
	}

	out, err := json.Marshal(opts)
	if err != nil {
		return nil, fmt.Errorf("tools: encode the normalised options: %w", err)
	}

	return out, nil
}

// Run implements jobs.Tool.
func (t *VideoClip) Run(ctx context.Context, w jobs.Work) (jobs.Outcome, error) {
	opts, err := ParseClipOptions(w.Options)
	if err != nil {
		return jobs.Outcome{}, jobs.Fail(jobs.UnsupportedInput, err, "the stored options are not valid for this tool")
	}

	input, local, err := fetchInput(ctx, t.files, w, t.Accepted(), t.Name())
	if err != nil {
		return jobs.Outcome{}, err
	}

	if opts.Mode == ClipModeSplit {
		return t.split(ctx, w, opts, input, local)
	}

	return t.trim(ctx, w, opts, input, local)
}

// trim cuts one range and stores it.
func (t *VideoClip) trim(
	ctx context.Context,
	w jobs.Work,
	opts ClipOptions,
	input models.File,
	local string,
) (jobs.Outcome, error) {
	var reportErr error

	result, err := t.edit.Trim(ctx, editor.TrimRequest{
		Dir:           w.Dir,
		Input:         local,
		Output:        "output.mp4",
		Start:         opts.start(),
		Duration:      opts.duration(),
		FrameAccurate: opts.FrameAccurate,
		Crop:          opts.Crop,
		Progress: func(p editor.Progress) {
			if reportErr != nil {
				return
			}

			reportErr = w.Report(jobs.Step{
				Percent: scale(p.Percent, phaseProcessingAt, phaseStoringAt),
				Phase:   "cutting",
				Message: "Cutting the video.",
			})
		},
	})
	if err != nil {
		return jobs.Outcome{}, editFailure(err)
	}

	if reportErr != nil {
		return jobs.Outcome{}, reportErr
	}

	if err := w.Report(jobs.Step{Percent: phaseStoringAt, Phase: "storing"}); err != nil {
		return jobs.Outcome{}, err
	}

	stored, err := storeNamedResult(ctx, t.files, w, result.Path, stemOf(input.Name)+"-cut.mp4")
	if err != nil {
		return jobs.Outcome{}, err
	}

	return jobs.Outcome{BytesOut: stored, CPUMs: result.CPUTime.Milliseconds()}, nil
}

// split cuts the whole video into clips and stores each one.
//
// Unlike the download tool's clips mode this does not also build a zip: the
// input is already on the server, so somebody who wants one file can ask for
// the download tool's clips instead, and storing every clip twice for a file
// the user already has would be waste.
func (t *VideoClip) split(
	ctx context.Context,
	w jobs.Work,
	opts ClipOptions,
	input models.File,
	local string,
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
		Input:   local,
		Segment: opts.clipLength(),
	})
	if err != nil {
		return jobs.Outcome{}, editFailure(err)
	}

	if len(clips) > MaxClips {
		return jobs.Outcome{}, jobs.Refuse(jobs.InputTooLarge, errTooManyClips,
			fmt.Sprintf("That video splits into %d clips, and this tool delivers at most %d. "+
				"Ask for longer clips, or trim it first.", len(clips), MaxClips),
			fmt.Sprintf("%d clips exceed the %d cap", len(clips), MaxClips))
	}

	stem := stemOf(input.Name)

	var total int64

	for i := range clips {
		if err := w.Report(jobs.Step{
			Percent: scale(i*100/len(clips), phaseStoringClipAt, 99),
			Phase:   "storing",
			Message: fmt.Sprintf("Storing clip %d of %d.", i+1, len(clips)),
		}); err != nil {
			return jobs.Outcome{}, err
		}

		stored, err := storeNamedResult(ctx, t.files, w, clips[i].Path,
			fmt.Sprintf("%s-clip-%02d.mp4", stem, i+1))
		if err != nil {
			return jobs.Outcome{}, err
		}

		total += stored
	}

	return jobs.Outcome{BytesOut: total}, nil
}

// VideoCompress makes a video or an audio file smaller, and says so when it
// could not.
type VideoCompress struct {
	files *files.Store
	edit  *editor.Editor
	comp  *compressor.Compressor
}

// NewVideoCompress returns the tool.
func NewVideoCompress(f *files.Store, e *editor.Editor, c *compressor.Compressor) *VideoCompress {
	return &VideoCompress{files: f, edit: e, comp: c}
}

// Name implements jobs.Tool.
func (*VideoCompress) Name() string { return "video-compress" }

// Ability implements jobs.Tool.
//
// video:edit rather than an ability of its own: compressing is an edit of a
// video, and a key that may cut a video has no reason to be refused a
// compression of the same file. Adding video:compress would also mean
// changing the closed ability set, which every domain shares.
func (*VideoCompress) Ability() string { return abilities.VideoEdit }

// Timeout implements jobs.Tool.
//
// Fifteen minutes: a compression re-encodes the whole file rather than a
// range of it, so it is the slowest thing this domain does.
func (*VideoCompress) Timeout() time.Duration { return 15 * time.Minute }

// Accepted implements jobs.Accepter. Audio is accepted as well as video: the
// same tool answers both, with the engine profile chosen from what ffprobe
// found rather than from what the client said.
func (*VideoCompress) Accepted() []string {
	return append(editedVideoTypes(), "audio/mpeg", "audio/wav", "audio/flac", "audio/ogg", "audio/aac")
}

// Run implements jobs.Tool.
func (t *VideoCompress) Run(ctx context.Context, w jobs.Work) (jobs.Outcome, error) {
	input, local, err := fetchInput(ctx, t.files, w, t.Accepted(), t.Name())
	if err != nil {
		return jobs.Outcome{}, err
	}

	// The profile is chosen from the streams ffprobe found, not from the
	// upload's media type: a .mp4 holding nothing but an AAC track is an
	// audio file whatever its container says, and re-encoding a video
	// profile onto it would produce a file with a blank picture.
	source, err := t.edit.Probe(ctx, w.Dir, local)
	if err != nil {
		return jobs.Outcome{}, editFailure(err)
	}

	if err := w.Report(jobs.Step{
		Percent: phaseProcessingAt,
		Phase:   "compressing",
		Message: "Trying to make it smaller.",
	}); err != nil {
		return jobs.Outcome{}, err
	}

	result, suffix, err := t.run(ctx, w, local, source)
	if err != nil {
		return jobs.Outcome{}, err
	}

	if err := w.Report(jobs.Step{
		Percent: phaseStoringAt,
		Phase:   "storing",
		Message: result.Summary(),
	}); err != nil {
		return jobs.Outcome{}, err
	}

	stored, err := storeNamedResult(ctx, t.files, w, result.Path, stemOf(input.Name)+suffix)
	if err != nil {
		return jobs.Outcome{}, err
	}

	return jobs.Outcome{BytesOut: stored, CPUMs: result.CPUTime.Milliseconds()}, nil
}

// run picks the profile from what the file actually holds.
func (t *VideoCompress) run(
	ctx context.Context,
	w jobs.Work,
	local string,
	source editor.Media,
) (compressor.Result, string, error) {
	if source.HasVideo() {
		result, err := t.comp.Video(ctx, compressor.Request{
			Dir:    w.Dir,
			Input:  local,
			Output: "compressed.mp4",
		})
		if err != nil {
			return compressor.Result{}, "", compressFailure(err)
		}

		return result, "-smaller.mp4", nil
	}

	result, err := t.comp.Audio(ctx, compressor.Request{
		Dir:    w.Dir,
		Input:  local,
		Output: "compressed.mp3",
	})
	if err != nil {
		return compressor.Result{}, "", compressFailure(err)
	}

	return result, "-smaller.mp3", nil
}

// ValidateOptions implements jobs.OptionsValidator.
//
// There are no options: a compression is "make this smaller", and every knob
// that could be exposed — the CRF, the bitrate — is a number nobody outside
// this file should have to choose. It is implemented so a client sending an
// object is told it is ignored rather than having it silently stored.
func (*VideoCompress) ValidateOptions(json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage("{}"), nil
}

// errTooManyClips is the cause recorded when a split produces more clips than
// the cap allows.
var errTooManyClips = fmt.Errorf("tools: more than %d clips", MaxClips)

// fetchInput reads the conversion's input out of the bucket into the job's own
// directory, refusing a type the tool cannot read.
//
// It is copied rather than streamed into the engine because ffmpeg and
// ffprobe both seek: ffprobe reads the header, the moov atom and then
// packets at an interval, which over a bucket means paying for the object
// several times and failing halfway when the connection drops.
func fetchInput(
	ctx context.Context,
	store *files.Store,
	w jobs.Work,
	accepted []string,
	tool string,
) (models.File, string, error) {
	inputs, err := store.ForConversion(ctx, w.ConversionID, models.FileInput)
	if err != nil {
		return models.File{}, "", jobs.Fail(jobs.Internal, err, "look up the conversion's input")
	}

	if len(inputs) == 0 {
		return models.File{}, "", jobs.Failf(jobs.Internal, "conversion %s has no input file", w.ConversionID)
	}

	input := inputs[0]

	if !slices.Contains(accepted, input.MIME) {
		kind, _ := detect.KindOf(input.MIME)
		if kind.Label == "" {
			kind.Label = input.MIME
		}

		return models.File{}, "", jobs.Failf(jobs.UnsupportedInput, "%s is not a type %s reads", kind.Label, tool)
	}

	if err := w.Report(jobs.Step{Percent: phaseReadingAt, Phase: "reading"}); err != nil {
		return models.File{}, "", err
	}

	local, err := copyToDir(ctx, store, w.Dir, input)
	if err != nil {
		return models.File{}, "", err
	}

	return input, local, nil
}

// copyToDir streams the input out of the bucket onto the job's disk.
func copyToDir(ctx context.Context, store *files.Store, dir string, input models.File) (string, error) {
	body, err := store.Open(ctx, input)
	if err != nil {
		return "", jobs.Fail(jobs.Internal, err, "open the input")
	}

	defer func() { _ = body.Close() }()

	// A fixed name, not the upload's: the upload's name came from the client
	// and the extension is only a hint. The real type was settled from the
	// magic bytes at upload and is on the row.
	kind, _ := detect.KindOf(input.MIME)

	path := filepath.Join(dir, "input"+kind.Extension)

	dst, err := os.Create(path)
	if err != nil {
		return "", jobs.Fail(jobs.DiskFull, err, "create the local input file")
	}

	if _, err := io.Copy(dst, body); err != nil {
		_ = dst.Close()

		return "", jobs.Fail(jobs.DiskFull, err, "write the local input file")
	}

	if err := dst.Close(); err != nil {
		return "", jobs.Fail(jobs.DiskFull, err, "close the local input file")
	}

	return path, nil
}

// storeNamedResult puts a produced file in the bucket against the conversion
// under a name the caller chose, with its type settled from the bytes rather
// than from the name the engine wrote.
//
// It differs from storeResult, which names the result after the input and
// reads the type from the engine's declared MIME. The two should become one
// function that detects from the bytes and derives the name: see status.md.
func storeNamedResult(
	ctx context.Context,
	store *files.Store,
	w jobs.Work,
	path, name string,
) (int64, error) {
	body, err := os.Open(path)
	if err != nil {
		return 0, jobs.Fail(jobs.Internal, err, "open the result to store it")
	}

	defer func() { _ = body.Close() }()

	kind, err := detect.Of(body)
	if err != nil {
		return 0, jobs.Fail(jobs.UnsupportedInput, err, "the engine produced a type this service does not recognise")
	}

	if _, err := body.Seek(0, io.SeekStart); err != nil {
		return 0, jobs.Fail(jobs.Internal, err, "rewind the result")
	}

	stored, err := store.Put(ctx, files.Incoming{
		UserID:       w.UserID,
		ConversionID: w.ConversionID,
		Role:         models.FileOutput,
		Name:         name,
		Kind:         kind,
	}, body)
	if err != nil {
		return 0, jobs.Fail(jobs.Internal, err, "store the result")
	}

	return stored.SizeBytes, nil
}

// stemOf is a file name without its extension, reduced to something safe to
// build another name from.
func stemOf(name string) string {
	stem := safeStem(strings.TrimSuffix(name, filepath.Ext(name)))
	if stem == "" {
		return "video"
	}

	return stem
}

// editFailure maps an editor failure onto the job error taxonomy, keeping the
// sentence the editor chose — which for a crop or a duration refusal is the
// one that names the real numbers.
func editFailure(err error) error {
	f := editor.Classify(err)
	if f == nil {
		return nil
	}

	return jobs.Refuse(editErrorType(f.Reason), f, f.Public, f.Detail)
}

// editErrorType is the taxonomy code for an edit failure.
func editErrorType(reason editor.Reason) jobs.ErrorType {
	switch reason {
	case editor.ReasonUnsupportedInput:
		return jobs.UnsupportedInput
	case editor.ReasonOutOfRange:
		// unsupported_input, not input_too_large: the file is fine and the
		// request is not, and the sentence names the real dimensions so the
		// user can fix it.
		return jobs.UnsupportedInput
	case editor.ReasonTooLong:
		return jobs.InputTooLarge
	case editor.ReasonDiskFull:
		return jobs.DiskFull
	case editor.ReasonOutOfMemory:
		return jobs.OutOfMemory
	case editor.ReasonTimeout:
		return jobs.Timeout
	case editor.ReasonCancelled:
		return jobs.CancelledByUser
	case editor.ReasonEngine:
		return jobs.EngineFailure
	default:
		return jobs.EngineFailure
	}
}

// compressFailure maps a compressor failure onto the job error taxonomy.
func compressFailure(err error) error {
	f := compressor.Classify(err)
	if f == nil {
		return nil
	}

	return jobs.Refuse(compressErrorType(f.Reason), f, f.Public, f.Detail)
}

// compressErrorType is the taxonomy code for a compression failure.
func compressErrorType(reason compressor.Reason) jobs.ErrorType {
	switch reason {
	case compressor.ReasonUnsupportedInput, compressor.ReasonEncrypted:
		return jobs.UnsupportedInput
	case compressor.ReasonDiskFull:
		return jobs.DiskFull
	case compressor.ReasonOutOfMemory:
		return jobs.OutOfMemory
	case compressor.ReasonTimeout:
		return jobs.Timeout
	case compressor.ReasonCancelled:
		return jobs.CancelledByUser
	case compressor.ReasonEngine:
		return jobs.EngineFailure
	default:
		return jobs.EngineFailure
	}
}
