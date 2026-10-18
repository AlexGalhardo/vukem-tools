package tools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/AlexGalhardo/vukem-tools/app/models"
	"github.com/AlexGalhardo/vukem-tools/internal/audio"
	"github.com/AlexGalhardo/vukem-tools/internal/detect"
	"github.com/AlexGalhardo/vukem-tools/internal/engine"
	"github.com/AlexGalhardo/vukem-tools/internal/ffmpeg"
	"github.com/AlexGalhardo/vukem-tools/internal/files"
	vipsimage "github.com/AlexGalhardo/vukem-tools/internal/image"
	"github.com/AlexGalhardo/vukem-tools/internal/jobs"
	"github.com/AlexGalhardo/vukem-tools/internal/video"
)

// MediaEngines resolves an engine's path. *engine.Registry satisfies it.
//
// An interface rather than the registry itself so a media tool can be built
// in a test with two fake paths, without resolving nine binaries.
type MediaEngines interface {
	// Path returns the absolute path of an engine, and whether it was
	// resolved at boot.
	Path(name string) (string, bool)
	// Has reports whether the engine is available, which is what a tool asks
	// before offering itself.
	Has(name string) bool
}

// MediaRunner runs one engine subprocess under limits. *engine.Runner
// satisfies it.
type MediaRunner interface {
	Run(ctx context.Context, cmd engine.Command) (engine.Result, error)
}

// Media is what the image, video and audio tools need beyond the file store:
// the engines, the runner, and what those engines turned out to be able to do.
//
// The capabilities are a value held here rather than probed per conversion,
// and that is the whole reason this type exists. Finding out whether this
// build can write AVIF costs a process; doing it per request would be a
// process per request for an answer that cannot change while the binary does
// not. Probing once, at boot, also means an unavailable format is invisible
// in the API rather than being offered and then failing.
type Media struct {
	// Engines and Runner come from the resolved registry.
	Engines MediaEngines
	Runner  MediaRunner
	// Image is what libvips can read and write in this build.
	Image []vipsimage.Capability
	// FFmpeg is what this ffmpeg build is and what it can encode, including
	// whether it is the GPL build that has H.264 and H.265 at all.
	FFmpeg ffmpeg.Capabilities
}

// The engine names, as app/services/engines.go registers them.
const (
	engineVips       = "vips"
	engineVipsHeader = "vipsheader"
	engineFFmpeg     = "ffmpeg"
	engineFFprobe    = "ffprobe"
)

// ResolveMedia probes what the media engines can do, once, at boot.
//
// It is tolerant on purpose. An engine that is absent leaves its capabilities
// empty, its tools unavailable and the rest of the service running, which is
// the difference between "this feature is not ready" and "the image is
// broken". A probe that fails for an engine that *is* present is an error,
// because that means the engine is broken rather than missing, and a deploy
// should say so.
//
// dir must be a writable scratch directory: probing an image format means
// trying to encode four pixels, because an operation being compiled in and a
// format being writable are different facts.
func ResolveMedia(
	ctx context.Context,
	engines MediaEngines,
	runner MediaRunner,
	dir string,
) (*Media, error) {
	m := &Media{Engines: engines, Runner: runner}

	if vips, ok := vipsEngine(engines); ok {
		caps, err := vipsimage.Capabilities(ctx, vips, runner, dir)
		if err != nil {
			return nil, fmt.Errorf("tools: probe libvips: %w", err)
		}

		m.Image = caps
	}

	if ff, ok := ffmpegEngine(engines); ok {
		caps, err := ffmpeg.ReadCapabilities(ctx, ff, runner, dir)
		if err != nil {
			return nil, fmt.Errorf("tools: probe ffmpeg: %w", err)
		}

		m.FFmpeg = caps
	}

	return m, nil
}

// vipsEngine returns the libvips programs, and whether both are present.
// Both, because the metadata probe is a separate binary and a conversion
// needs it: half of libvips is not a working image pipeline.
func vipsEngine(engines MediaEngines) (vipsimage.Engine, bool) {
	vips, okVips := engines.Path(engineVips)
	header, okHeader := engines.Path(engineVipsHeader)

	if !okVips || !okHeader {
		return vipsimage.Engine{}, false
	}

	return vipsimage.Engine{Vips: vips, VipsHeader: header}, true
}

// ffmpegEngine returns the ffmpeg programs, and whether both are present.
func ffmpegEngine(engines MediaEngines) (ffmpeg.Engine, bool) {
	binary, okFFmpeg := engines.Path(engineFFmpeg)
	probe, okProbe := engines.Path(engineFFprobe)

	if !okFFmpeg || !okProbe {
		return ffmpeg.Engine{}, false
	}

	return ffmpeg.Engine{FFmpeg: binary, FFprobe: probe}, true
}

// HasImageEngine reports whether the image tools can run.
func (m *Media) HasImageEngine() bool {
	if m == nil {
		return false
	}

	_, ok := vipsEngine(m.Engines)

	return ok
}

// HasFFmpeg reports whether the video and audio tools can run.
func (m *Media) HasFFmpeg() bool {
	if m == nil {
		return false
	}

	_, ok := ffmpegEngine(m.Engines)

	return ok
}

// WritableImageFormats returns the image formats this build can produce, for
// the API to publish. The frontend reads it from there rather than holding a
// list of its own, which is a roadmap requirement and also the only way the
// list can be true.
func (m *Media) WritableImageFormats() []vipsimage.Format {
	if m == nil {
		return nil
	}

	return vipsimage.Writable(m.Image)
}

// ReadableImageTypes returns the MIME types the image tools accept.
func (m *Media) ReadableImageTypes() []string {
	if m == nil {
		return nil
	}

	// Intersected with what internal/detect recognises, because a type the
	// upload endpoint will not store is a type no conversion can ever
	// receive, and advertising it would be a dead end.
	var out []string

	for _, mime := range vipsimage.Readable(m.Image) {
		if detect.Known(mime) {
			out = append(out, mime)
		}
	}

	return out
}

// WritableAudioFormats returns the audio formats this build can produce.
func (m *Media) WritableAudioFormats() []audio.Format {
	if m == nil {
		return nil
	}

	return audio.Available(m.FFmpeg)
}

// WritableVideoCodecs returns the video codecs this build can produce, which
// is where the GPL question surfaces: an LGPL build has no H.264 and no
// H.265, and the interface must not offer them.
func (m *Media) WritableVideoCodecs() []video.Codec {
	if m == nil {
		return nil
	}

	return video.AvailableCodecs(m.FFmpeg)
}

// localInputName is what every tool's local copy of its input is called.
//
// A constant, not a parameter: the whole point is that no caller can choose
// it. See localInput.
const localInputName = "input"

// localInput copies a conversion's input out of the bucket into the job's own
// directory, under a fixed name.
//
// The fixed name is a security property, not tidiness. libvips reads its
// loader options out of the filename — `in.jpg[shrink=8]` is the documented
// interface — so a local file named after the upload would let an uploaded
// name change how the file is decoded. ffmpeg has the same shape in other
// places. The name here is chosen by this code, so neither can happen; the
// extension comes from the *detected* type rather than from the upload's
// name, for the same reason.
//
// It is copied rather than streamed into the engine because every engine here
// reads its input more than once: libvips reads the header and then decodes,
// ffmpeg's two-pass paths read the whole file twice. Two passes over a bucket
// means paying for the object twice and failing halfway if the connection
// drops.
func localInput(
	ctx context.Context,
	store *files.Store,
	w jobs.Work,
	in models.File,
) (string, error) {
	kind, ok := detect.KindOf(in.MIME)
	if !ok {
		return "", jobs.Failf(jobs.UnsupportedInput, "%s is not a type this service recognises", in.MIME)
	}

	body, err := store.Open(ctx, in)
	if err != nil {
		return "", jobs.Fail(jobs.Internal, err, "open the input")
	}

	defer func() { _ = body.Close() }()

	path := filepath.Join(w.Dir, localInputName+kind.Extension)

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

// inputFile returns the one file a conversion is to read, refusing a type the
// tool cannot decode.
func inputFile(
	ctx context.Context,
	store *files.Store,
	w jobs.Work,
	tool string,
	accepted []string,
) (models.File, error) {
	inputs, err := store.ForConversion(ctx, w.ConversionID, models.FileInput)
	if err != nil {
		return models.File{}, jobs.Fail(jobs.Internal, err, "look up the conversion's input")
	}

	if len(inputs) == 0 {
		// Nothing is attached and nothing ever will be: the submit endpoint
		// attaches the input in the same transaction that creates the
		// conversion.
		return models.File{}, jobs.Failf(jobs.Internal, "conversion %s has no input file", w.ConversionID)
	}

	in := inputs[0]

	if slices.Contains(accepted, in.MIME) {
		return in, nil
	}

	kind, _ := detect.KindOf(in.MIME)
	if kind.Label == "" {
		kind.Label = in.MIME
	}

	return models.File{}, jobs.Refuse(jobs.UnsupportedInput, nil,
		"That file is "+kind.Label+", which "+tool+" cannot read.",
		in.MIME+" is not a type "+tool+" reads")
}

// storeResult puts a finished file in the bucket and records it against the
// conversion.
//
// The result is named after the input so somebody downloading several
// conversions can tell them apart, with the input's extension replaced by the
// output's.
func storeResult(
	ctx context.Context,
	store *files.Store,
	w jobs.Work,
	input models.File,
	path, mime string,
) (jobs.Outcome, error) {
	kind, ok := detect.KindOf(mime)
	if !ok {
		return jobs.Outcome{}, jobs.Failf(jobs.Internal, "%s is not a recognised type", mime)
	}

	body, err := os.Open(path)
	if err != nil {
		return jobs.Outcome{}, jobs.Fail(jobs.Internal, err, "open the result to store it")
	}

	defer func() { _ = body.Close() }()

	stored, err := store.Put(ctx, files.Incoming{
		UserID:       w.UserID,
		ConversionID: w.ConversionID,
		Role:         models.FileOutput,
		Name:         strings.TrimSuffix(input.Name, filepath.Ext(input.Name)) + kind.Extension,
		Kind:         kind,
	}, body)
	if err != nil {
		return jobs.Outcome{}, jobs.Fail(jobs.Internal, err, "store the result")
	}

	return jobs.Outcome{BytesOut: stored.SizeBytes}, nil
}

// classifyImage maps internal/image's errors onto the job error taxonomy.
//
// The mapping is where the two vocabularies meet, and it is deliberately
// explicit rather than a default: a failure nobody classified is Internal,
// which is retried, and retrying a file that can never convert wastes a
// worker to reach the same answer. So every error this package can produce
// has a case, and only something genuinely unrecognised falls through.
func classifyImage(err error) error {
	if err == nil {
		return nil
	}

	if dimension, ok := errors.AsType[*vipsimage.DimensionError](err); ok {
		return jobs.Refuse(jobs.InputTooLarge, err, dimension.Message(), dimension.Error())
	}

	if unsupported, ok := errors.AsType[*vipsimage.UnsupportedError](err); ok {
		return jobs.Refuse(jobs.UnsupportedInput, err, unsupported.Message(), unsupported.Error())
	}

	if crop, ok := errors.AsType[*vipsimage.CropError](err); ok {
		return jobs.Refuse(jobs.UnsupportedInput, err, crop.Message(), crop.Error())
	}

	if errors.Is(err, vipsimage.ErrPathHasOptionSyntax) {
		// Not the user's fault and not their problem: the tool chooses the
		// local filename, so this can only be a bug here.
		return jobs.Fail(jobs.Internal, err, "the local input path was built unsafely")
	}

	if engineErr, ok := errors.AsType[*vipsimage.EngineError](err); ok {
		return engineFailure(err, string(engineErr.Kind), engineErr.Error())
	}

	return jobs.Fail(jobs.Internal, err, "the image conversion failed")
}

// engineFailure maps an engine error kind onto the taxonomy. The image and
// the ffmpeg packages use the same kind names for the same reasons, so the
// mapping is shared.
func engineFailure(err error, kind, detail string) error {
	switch kind {
	case "unreadable":
		return jobs.Refuse(jobs.UnsupportedInput, err,
			"That file could not be decoded. It may be damaged or only partly uploaded.", detail)
	case "unsupported":
		return jobs.Refuse(jobs.UnsupportedInput, err,
			"That conversion is not available on this server.", detail)
	case "out_of_memory":
		return jobs.Fail(jobs.OutOfMemory, err, detail)
	case "resource_limit":
		// The server's own limits withheld a thread, which is a
		// configuration problem and not the file's fault. Internal is both
		// the truthful code and the one whose message does not send the user
		// looking at a file that is fine.
		return jobs.Fail(jobs.Internal, err, detail)
	case "disk_full":
		return jobs.Fail(jobs.DiskFull, err, detail)
	case "timeout":
		return jobs.Fail(jobs.Timeout, err, detail)
	default:
		return jobs.Fail(jobs.EngineFailure, err, detail)
	}
}

// classifyFFmpeg maps internal/ffmpeg's, internal/video's and
// internal/audio's errors onto the taxonomy.
func classifyFFmpeg(err error) error {
	if err == nil {
		return nil
	}

	// A cancellation the owner asked for travels back out as itself, so the
	// pipeline settles the conversion as cancelled rather than failed.
	if _, ok := errors.AsType[*jobs.ConversionError](err); ok {
		return err
	}

	// Every refusal this layer makes carries its own sentence, and that
	// sentence is better than the taxonomy's: "That video is 3h12m long, and
	// the limit for this tool is 2h0m0s" against "that file is past the
	// limit for this tool".
	switch {
	case isAnyType[*video.DurationError](err), isAnyType[*audio.DurationError](err):
		return jobs.Refuse(jobs.InputTooLarge, err, messageOf(err), err.Error())
	case isAnyType[*video.IncompatibleError](err),
		isAnyType[*video.NoVideoError](err),
		isAnyType[*video.NoReplacementAudioError](err),
		isAnyType[*video.UnknownDitherError](err),
		isAnyType[*audio.NoAudioError](err),
		isAnyType[*audio.MissingFormatError](err),
		isAnyType[*ffmpeg.MissingEncoderError](err):
		return jobs.Refuse(jobs.UnsupportedInput, err, messageOf(err), err.Error())
	case errors.Is(err, ffmpeg.ErrNoStreams):
		return jobs.Refuse(jobs.UnsupportedInput, err,
			"That file holds no audio or video, whatever its name says.", err.Error())
	}

	if engineErr, ok := errors.AsType[*ffmpeg.EngineError](err); ok {
		return engineFailure(err, string(engineErr.Kind), engineErr.Error())
	}

	return jobs.Fail(jobs.Internal, err, "the conversion failed")
}

// isAnyType reports whether err is, or wraps, a T.
func isAnyType[T error](err error) bool {
	_, ok := errors.AsType[T](err)
	return ok
}

// messageOf returns the public sentence an error carries, or the taxonomy's
// when it carries none.
func messageOf(err error) string {
	var carrier interface{ Message() string }
	if errors.As(err, &carrier) {
		return carrier.Message()
	}

	return jobs.MessageOf(err)
}
