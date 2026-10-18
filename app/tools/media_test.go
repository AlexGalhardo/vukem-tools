package tools_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/AlexGalhardo/vukem-tools/app/tools"
	"github.com/AlexGalhardo/vukem-tools/internal/audio"
	vipsimage "github.com/AlexGalhardo/vukem-tools/internal/image"
	"github.com/AlexGalhardo/vukem-tools/internal/video"
)

// These tests need no engine. They cover option validation, which is the
// layer that decides what reaches an engine's own option syntax, and they run
// everywhere.

// writableImages is the set the production image was probed to write:
// AVIF and HEIC are absent because Debian's libheif ships no encoder, which
// is exactly the kind of gap the validation has to report rather than hide.
var writableImages = []vipsimage.Format{
	vipsimage.JPEG,
	vipsimage.PNG,
	vipsimage.WebP,
	vipsimage.JXL,
	vipsimage.GIF,
	vipsimage.TIFF,
}

// gplCodecs and lgplCodecs are the two builds the video options have to cope
// with.
var (
	gplCodecs  = []video.Codec{video.H264, video.H265, video.VP9, video.AV1}
	lgplCodecs = []video.Codec{video.VP9, video.AV1}
)

var allAudio = []audio.Format{audio.MP3, audio.WAV, audio.FLAC, audio.Opus, audio.AAC}

// TestAnUnwritableImageFormatIsRefusedWithWhatIsAvailable: the list comes
// from what the engine was probed to do, so a build without AVIF must not
// offer AVIF and must say what it does have.
func TestAnUnwritableImageFormatIsRefusedWithWhatIsAvailable(t *testing.T) {
	_, err := tools.ParseImageConvertOptions(json.RawMessage(`{"to":"avif"}`), writableImages)
	if err == nil {
		t.Fatal("a format this build cannot write was accepted")
	}

	message := err.Error()

	for _, want := range []string{"avif", "jpeg", "webp"} {
		if !strings.Contains(message, want) {
			t.Errorf("the refusal should mention %q, got %q", want, message)
		}
	}
}

func TestAnImageConversionNeedsAnOutputFormat(t *testing.T) {
	// There is no sensible default: converting to the input's own format is
	// a re-encode nobody asked for, and guessing is worse than asking.
	if _, err := tools.ParseImageConvertOptions(nil, writableImages); err == nil {
		t.Error("an image conversion with no output format was accepted")
	}
}

func TestEveryImageEnumIsCheckedAtSubmission(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		// The values below reach libvips's own option syntax, which libvips
		// parses itself, so each one has to be refused here rather than
		// where the command line is built.
		{"unknown resize mode", `{"to":"png","resize":"squish","width":10,"height":10}`},
		{"unknown flip", `{"to":"png","flip":"diagonal"}`},
		{"rotation that is not a quarter turn", `{"to":"png","rotate":45}`},
		{"resize with no dimensions", `{"to":"png","resize":"fit"}`},
		{"fill with only a width", `{"to":"png","resize":"fill","width":100}`},
		{"exact with only a height", `{"to":"png","resize":"exact","height":100}`},
		{"a crop with no height", `{"to":"png","crop_width":10}`},
		{"a crop with no width", `{"to":"png","crop_height":10}`},
		{"quality past the top", `{"to":"jpeg","quality":101}`},
		{"negative effort", `{"to":"png","effort":-1}`},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := tools.ParseImageConvertOptions(json.RawMessage(c.body), writableImages); err == nil {
				t.Errorf("%s was accepted", c.name)
			}
		})
	}
}

func TestAValidImageConversionSurvivesNormalisation(t *testing.T) {
	got, err := tools.ParseImageConvertOptions(json.RawMessage(
		`{"to":"WebP","resize":"Fit","width":800,"quality":90,"rotate":90,"flip":"Horizontal"}`),
		writableImages)
	if err != nil {
		t.Fatalf("a valid conversion was refused: %v", err)
	}

	// The enums are lowercased, so a client that sent "WebP" and one that
	// sent "webp" store the same options and the worker reads one spelling.
	if got.To != "webp" || got.Resize != "fit" || got.Flip != "horizontal" {
		t.Errorf("the enums were not normalised: %+v", got)
	}

	if got.Width != 800 || got.Quality != 90 || got.Rotate != 90 {
		t.Errorf("a value was lost: %+v", got)
	}
}

func TestACropOriginWithoutACropIsCleared(t *testing.T) {
	// Otherwise the stored options imply a crop that will not happen, and a
	// later reader cannot tell which was meant.
	got, err := tools.ParseImageConvertOptions(json.RawMessage(
		`{"to":"png","crop_left":40,"crop_top":40}`), writableImages)
	if err != nil {
		t.Fatalf("normalise: %v", err)
	}

	if got.CropLeft != 0 || got.CropTop != 0 {
		t.Errorf("a crop origin survived with no crop: %+v", got)
	}
}

// TestAGPLOnlyCodecIsRefusedAtSubmissionOnAnLGPLBuild is the submission-time
// half of the S4.4 GPL assert: the refusal happens before a job is queued,
// and it names what the build does have.
func TestAGPLOnlyCodecIsRefusedAtSubmissionOnAnLGPLBuild(t *testing.T) {
	for _, codec := range []string{"h264", "h265"} {
		t.Run(codec, func(t *testing.T) {
			body := json.RawMessage(`{"container":"mp4","video":"` + codec + `"}`)

			if _, err := tools.ParseVideoConvertOptions(body, gplCodecs); err != nil {
				t.Fatalf("the GPL build refused %s: %v", codec, err)
			}

			_, err := tools.ParseVideoConvertOptions(body, lgplCodecs)
			if err == nil {
				t.Fatalf("the LGPL build accepted %s", codec)
			}

			// The message has to name the alternatives, which is the whole
			// difference between this and ffmpeg's "Unknown encoder".
			for _, want := range []string{"vp9", "av1"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the refusal should offer %q, got %q", want, err.Error())
				}
			}
		})
	}
}

func TestAnIncompatibleContainerAndCodecIsRefusedAtSubmission(t *testing.T) {
	cases := []struct{ name, body string }{
		{"vp9 in mov", `{"container":"mov","video":"vp9"}`},
		{"h264 in webm", `{"container":"webm","video":"h264"}`},
		{"opus in mov", `{"container":"mov","audio":"opus"}`},
		{"unknown container", `{"container":"avi"}`},
		{"no container", `{}`},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := tools.ParseVideoConvertOptions(json.RawMessage(c.body), gplCodecs); err == nil {
				t.Errorf("%s was accepted", c.name)
			}
		})
	}
}

// TestADitheringModeCannotReachTheFiltergraph is the submission-time half of
// the filtergraph finding.
//
// The value goes into `-lavfi`, which ffmpeg parses itself: a comma starts
// the next filter, a semicolon the next chain, and brackets name a pad.
// internal/engine passes argv directly, so there is no shell to inject into,
// and that is beside the point.
func TestADitheringModeCannotReachTheFiltergraph(t *testing.T) {
	hostile := []string{
		"none,movie=/app/work/input.mp4",
		"sierra2_4a;[x]null[y]",
		"none[a];[a]scale=1:1",
		"none=1",
		"sierra4",
	}

	for _, value := range hostile {
		t.Run(value, func(t *testing.T) {
			body, err := json.Marshal(map[string]string{"dither": value})
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			_, err = tools.ParseVideoToGIFOptions(body)
			if err == nil {
				t.Fatalf("%q was accepted as a dithering mode", value)
			}

			// And the refusal names the accepted modes rather than leaving
			// somebody to guess through nine of them.
			if !strings.Contains(err.Error(), "sierra2_4a") {
				t.Errorf("the refusal does not list the modes: %q", err.Error())
			}
		})
	}
}

func TestEveryDitheringModeTheEngineHasIsAccepted(t *testing.T) {
	// The complement: an allowlist that refuses a mode that works is a bug
	// too. Each of these was confirmed to encode in the production image.
	for _, mode := range video.DitherNames() {
		body, err := json.Marshal(map[string]string{"dither": mode})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		if _, err := tools.ParseVideoToGIFOptions(body); err != nil {
			t.Errorf("%q was refused and the engine accepts it: %v", mode, err)
		}
	}

	// And an empty mode means "the default" rather than an error.
	if _, err := tools.ParseVideoToGIFOptions(json.RawMessage(`{}`)); err != nil {
		t.Errorf("the default dithering mode was refused: %v", err)
	}
}

func TestAGIFRangeIsBoundedAtSubmission(t *testing.T) {
	// A GIF has no interframe compression worth the name, so the file grows
	// linearly with the duration: a one-minute GIF at any useful size is
	// tens of megabytes and a worse result than the video it came from.
	if _, err := tools.ParseVideoToGIFOptions(json.RawMessage(`{"duration_seconds":60}`)); err == nil {
		t.Error("a sixty-second GIF was accepted")
	}

	if _, err := tools.ParseVideoToGIFOptions(json.RawMessage(`{"duration_seconds":10}`)); err != nil {
		t.Errorf("a ten-second GIF was refused: %v", err)
	}
}

func TestABitrateOnALosslessFormatIsRefusedRatherThanIgnored(t *testing.T) {
	// Silently producing a 900 kbit/s FLAC would leave somebody thinking the
	// option worked.
	for _, format := range []string{"flac", "wav"} {
		body := json.RawMessage(`{"to":"` + format + `","bitrate":128}`)

		_, err := tools.ParseAudioConvertOptions(body, allAudio)
		if err == nil {
			t.Errorf("a bit rate on %s was accepted", format)
			continue
		}

		if !strings.Contains(strings.ToLower(err.Error()), "lossless") {
			t.Errorf("the refusal should say why, got %q", err.Error())
		}
	}

	// On a lossy format it is accepted.
	if _, err := tools.ParseAudioConvertOptions(
		json.RawMessage(`{"to":"mp3","bitrate":192}`), allAudio); err != nil {
		t.Errorf("a bit rate on MP3 was refused: %v", err)
	}
}

func TestTwoPassesWithoutNormalisationIsRefused(t *testing.T) {
	// There is no second pass to be exact about, so accepting it would mean
	// an option that does nothing.
	if _, err := tools.ParseAudioConvertOptions(
		json.RawMessage(`{"to":"mp3","two_pass":true}`), allAudio); err == nil {
		t.Error("two passes without normalisation was accepted")
	}

	if _, err := tools.ParseAudioConvertOptions(
		json.RawMessage(`{"to":"mp3","normalise":true,"two_pass":true}`), allAudio); err != nil {
		t.Errorf("a two-pass normalisation was refused: %v", err)
	}
}

func TestAnArbitrarySampleRateIsRefused(t *testing.T) {
	// Every encoder here takes a fixed set, and 44101 Hz gets a resampler
	// doing real work to produce a file no player benefits from.
	if _, err := tools.ParseAudioConvertOptions(
		json.RawMessage(`{"to":"mp3","sample_rate":44101}`), allAudio); err == nil {
		t.Error("an arbitrary sample rate was accepted")
	}

	for _, rate := range tools.AudioSampleRates() {
		body, err := json.Marshal(map[string]any{"to": "mp3", "sample_rate": rate})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		if _, err := tools.ParseAudioConvertOptions(body, allAudio); err != nil {
			t.Errorf("%d Hz was refused: %v", rate, err)
		}
	}
}

func TestAnAudioFormatThisBuildCannotWriteIsRefused(t *testing.T) {
	only := []audio.Format{audio.FLAC}

	_, err := tools.ParseAudioConvertOptions(json.RawMessage(`{"to":"mp3"}`), only)
	if err == nil {
		t.Fatal("a format this build cannot write was accepted")
	}

	if !strings.Contains(err.Error(), "flac") {
		t.Errorf("the refusal should name what is available, got %q", err.Error())
	}
}

func TestOptionsThatAreNotAnObjectAreRefusedOnTheRightField(t *testing.T) {
	// A client sending a string or an array must be told the field, not get
	// a generic 500 from a failed unmarshal.
	for _, body := range []string{`"png"`, `[1,2]`, `5`} {
		if _, err := tools.ParseImageConvertOptions(json.RawMessage(body), writableImages); err == nil {
			t.Errorf("%s was accepted as an options object", body)
		}

		if _, err := tools.ParseVideoConvertOptions(json.RawMessage(body), gplCodecs); err == nil {
			t.Errorf("%s was accepted as an options object", body)
		}

		if _, err := tools.ParseAudioConvertOptions(json.RawMessage(body), allAudio); err == nil {
			t.Errorf("%s was accepted as an options object", body)
		}
	}
}

// TestTheMediaToolsAreUnavailableWithoutTheirEngine: a missing engine makes a
// tool invisible rather than producing one that accepts work it cannot do.
func TestTheMediaToolsAreUnavailableWithoutTheirEngine(t *testing.T) {
	none := &tools.Media{Engines: noEngines{}}

	if got := tools.ImageTools(nil, none); len(got) != 0 {
		t.Errorf("ImageTools returned %d tools with no libvips", len(got))
	}

	if got := tools.VideoTools(nil, none); len(got) != 0 {
		t.Errorf("VideoTools returned %d tools with no ffmpeg", len(got))
	}

	if got := tools.AudioTools(nil, none); len(got) != 0 {
		t.Errorf("AudioTools returned %d tools with no ffmpeg", len(got))
	}

	// And with the engines present they exist, or the guard above would be
	// hiding everything.
	all := &tools.Media{Engines: everyEngine{}}

	if got := tools.ImageTools(nil, all); len(got) == 0 {
		t.Error("ImageTools returned nothing with libvips present")
	}

	if got := tools.VideoTools(nil, all); len(got) == 0 {
		t.Error("VideoTools returned nothing with ffmpeg present")
	}

	if got := tools.AudioTools(nil, all); len(got) == 0 {
		t.Error("AudioTools returned nothing with ffmpeg present")
	}
}

// noEngines resolves nothing.
type noEngines struct{}

func (noEngines) Path(string) (string, bool) { return "", false }
func (noEngines) Has(string) bool            { return false }

// everyEngine resolves everything, at a path nothing will run.
type everyEngine struct{}

func (everyEngine) Path(name string) (string, bool) { return "/usr/bin/" + name, true }
func (everyEngine) Has(string) bool                 { return true }

// TestEveryMediaToolHasAnAbilityAndATimeout guards the two pieces of the Tool
// contract that are easy to forget and silent when wrong: a tool with no
// ability would be callable by any key, and one with no timeout would be
// bounded only by the queue's default.
func TestEveryMediaToolHasAnAbilityAndATimeout(t *testing.T) {
	all := &tools.Media{Engines: everyEngine{}}

	registered := append(append(tools.ImageTools(nil, all), tools.VideoTools(nil, all)...),
		tools.AudioTools(nil, all)...)

	if len(registered) == 0 {
		t.Fatal("no media tools were built, so this test proved nothing")
	}

	for _, tool := range registered {
		if tool.Name() == "" {
			t.Error("a tool has no name")
		}

		if tool.Ability() == "" {
			t.Errorf("%s has no ability, so any key could use it", tool.Name())
		}

		if tool.Timeout() <= 0 {
			t.Errorf("%s has no timeout", tool.Name())
		}
	}
}
