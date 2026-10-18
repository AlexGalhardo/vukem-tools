package video

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/AlexGalhardo/vukem-tools/internal/ffmpeg"
)

// These tests need no ffmpeg. They cover the codec tables and the argument
// building, which is where every refusal is decided, and they are the only
// place the "GPL-only codec on an LGPL build" path can be proven: the
// capabilities are a value, so a test can hand this package an LGPL build
// without one being installed.

// gplBuild and lgplBuild are the two builds this project has to cope with.
var (
	gplBuild = ffmpeg.CapabilitiesFrom(
		"ffmpeg version 7.1.5\nconfiguration: --enable-gpl --enable-libx264 --enable-libx265\n",
		strings.Join([]string{
			"Encoders:",
			" ------",
			" V....D libx264              libx264 H.264",
			" V....D libx265              libx265 H.265",
			" V....D libvpx-vp9           libvpx VP9",
			" V..... libsvtav1            SVT-AV1",
			" A....D aac                  AAC",
			" A....D libopus              libopus Opus",
			" A....D libmp3lame           MP3",
			" A....D flac                 FLAC",
			" A....D libvorbis            Vorbis",
		}, "\n"),
	)

	lgplBuild = ffmpeg.CapabilitiesFrom(
		"ffmpeg version 7.1.5\nconfiguration: --enable-libvpx --enable-shared\n",
		strings.Join([]string{
			"Encoders:",
			" ------",
			" V....D libvpx-vp9           libvpx VP9",
			" V..... libsvtav1            SVT-AV1",
			" A....D aac                  AAC",
			" A....D libopus              libopus Opus",
		}, "\n"),
	)
)

// withBoth is a source file that has a video and an audio stream, which is
// what the planner is given for an ordinary conversion.
var withBoth = ffmpeg.Media{
	FormatName: "mov,mp4,m4a,3gp,3g2,mj2",
	Duration:   10 * time.Second,
	Video:      &ffmpeg.Stream{Type: "video", Codec: "h264", Width: 1920, Height: 1080, FrameRate: 25},
	Audio:      &ffmpeg.Stream{Type: "audio", Codec: "aac", Channels: 2, SampleRate: 48000},
}

// TestAGPLOnlyCodecOnAnLGPLBuildFailsWithSomethingToDo is the S4.4 assert
// that a GPL-build-only codec path fails with a clear message on an LGPL
// build, proven by faking the build rather than by installing one.
func TestAGPLOnlyCodecOnAnLGPLBuildFailsWithSomethingToDo(t *testing.T) {
	for _, codec := range []Codec{H264, H265} {
		t.Run(string(codec), func(t *testing.T) {
			// On the GPL build it works.
			if _, err := buildPlan("in.mp4", "out.mp4",
				Options{Container: MP4, Video: codec}, gplBuild, withBoth); err != nil {
				t.Fatalf("the GPL build refused %s: %v", codec, err)
			}

			// On the LGPL build it is refused, and the refusal says why and
			// what to use instead.
			_, err := buildPlan("in.mp4", "out.mp4",
				Options{Container: MP4, Video: codec}, lgplBuild, withBoth)

			var missing *ffmpeg.MissingEncoderError
			if !errors.As(err, &missing) {
				t.Fatalf("want a MissingEncoderError, got %v", err)
			}

			if !missing.GPLOnly {
				t.Errorf("%s was not marked as GPL-only", codec)
			}

			if missing.BuildIsGPL {
				t.Error("the LGPL build was reported as GPL")
			}

			message := missing.Message()

			for _, want := range []string{codec.Label(), "GPL", "VP9", "AV1"} {
				if !strings.Contains(message, want) {
					t.Errorf("the message should mention %q, got %q", want, message)
				}
			}
		})
	}
}

// TestTheRoyaltyFreeCodecsStillWorkOnAnLGPLBuild: the refusal above points
// somebody at VP9 and AV1, so those have to actually work there.
func TestTheRoyaltyFreeCodecsStillWorkOnAnLGPLBuild(t *testing.T) {
	for _, codec := range []Codec{VP9, AV1} {
		if _, err := buildPlan("in.mp4", "out.webm",
			Options{Container: WebM, Video: codec}, lgplBuild, withBoth); err != nil {
			t.Errorf("the LGPL build refused %s, which the error message recommends: %v", codec, err)
		}
	}
}

// TestEveryBuilderBoundsTheEncodersThreads is the regression test for a bug
// that cost a day and produced a message naming no cause.
//
// ffmpeg's -threads is positional: before an input it bounds that input's
// decoder, after the inputs it bounds the encoder. With only the global one,
// x264 on a 16-core host asks for about 24 threads, Linux counts threads
// against RLIMIT_NPROC, the engine's limit is lower than that, and ffmpeg
// exits having written nothing with "at least one of its streams received no
// packets".
func TestEveryBuilderBoundsTheEncodersThreads(t *testing.T) {
	p, err := buildPlan("in.mp4", "out.mp4", Options{Container: MP4}, gplBuild, withBoth)
	if err != nil {
		t.Fatalf("build a plan: %v", err)
	}

	assertThreadsAfterTheLastInput(t, p.args)
}

// assertThreadsAfterTheLastInput checks that a -threads flag appears after
// the last -i, which is the position that reaches the encoder.
func assertThreadsAfterTheLastInput(t *testing.T, args []string) {
	t.Helper()

	lastInput := -1

	for i, arg := range args {
		if arg == "-i" {
			lastInput = i
		}
	}

	if lastInput < 0 {
		t.Fatalf("the argument list has no input: %v", args)
	}

	if !slices.Contains(args[lastInput:], "-threads") {
		t.Errorf("no -threads after the last input, so the encoder is unbounded: %v", args)
	}
}

func TestTheArgumentsCarryTheProgressStreamAndTheStreamMap(t *testing.T) {
	p, err := buildPlan("in.mp4", "out.mp4", Options{Container: MP4}, gplBuild, withBoth)
	if err != nil {
		t.Fatalf("build a plan: %v", err)
	}

	argv := strings.Join(p.args, " ")

	for _, want := range []string{
		// Progress from the structured stream, never from stderr.
		"-progress pipe:1",
		// Only the first video and first audio stream: a file with an
		// embedded cover image otherwise has the cover treated as a second
		// video stream.
		"-map 0:v:0",
		"-map 0:a:0?",
		// An MP4 that starts playing before it has finished downloading.
		"-movflags +faststart",
		// Browser-playable pixels: ffmpeg happily produces yuv444p from an
		// RGB source, which nothing plays.
		"-pix_fmt yuv420p",
	} {
		if !strings.Contains(argv, want) {
			t.Errorf("the arguments are missing %q\ngot: %s", want, argv)
		}
	}
}

func TestAMutedConversionMapsNoAudio(t *testing.T) {
	p, err := buildPlan("in.mp4", "out.mp4",
		Options{Container: MP4, Audio: Mute}, gplBuild, withBoth)
	if err != nil {
		t.Fatalf("build a plan: %v", err)
	}

	argv := strings.Join(p.args, " ")

	if !strings.Contains(argv, "-an") {
		t.Errorf("a muted conversion does not pass -an: %s", argv)
	}

	// Mapping an audio stream that is then dropped would make ffmpeg warn
	// and, with some containers, write an empty track.
	if strings.Contains(argv, "-map 0:a") {
		t.Errorf("a muted conversion still maps audio: %s", argv)
	}
}

func TestASourceWithNoAudioIsMutedRatherThanRefused(t *testing.T) {
	// A silent clip asked for in MP4 with the default AAC must not fail for
	// want of an audio stream: there is simply nothing to encode.
	silent := ffmpeg.Media{
		FormatName: "mov,mp4",
		Duration:   time.Second,
		Video:      &ffmpeg.Stream{Type: "video", Codec: "h264", Width: 320, Height: 240},
	}

	p, err := buildPlan("in.mp4", "out.mp4", Options{Container: MP4}, gplBuild, silent)
	if err != nil {
		t.Fatalf("build a plan for a silent source: %v", err)
	}

	if !p.mute {
		t.Error("a source with no audio was not muted")
	}
}

func TestAnIncompatibleCodecNamesTheContainersOwnList(t *testing.T) {
	cases := []struct {
		name string
		opts Options
		want string
	}{
		// ffmpeg will mux VP9 into a MOV and produce a file nothing plays.
		{"vp9 in mov", Options{Container: MOV, Video: VP9}, "VP9"},
		// H.264 in WebM is not in the specification at all.
		{"h264 in webm", Options{Container: WebM, Video: H264}, "H.264"},
		// Opus in a MOV is muxable and plays nowhere.
		{"opus in mov", Options{Container: MOV, Audio: Opus}, "Opus"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := buildPlan("in.mp4", "out", c.opts, gplBuild, withBoth)

			var incompatible *IncompatibleError
			if !errors.As(err, &incompatible) {
				t.Fatalf("want an IncompatibleError, got %v", err)
			}

			if !strings.Contains(incompatible.Message(), c.want) {
				t.Errorf("the message should name %q, got %q", c.want, incompatible.Message())
			}

			// And it must name what the container does carry, or the user is
			// left guessing through four codecs.
			if len(incompatible.Allowed) == 0 {
				t.Error("the refusal lists no alternatives")
			}
		})
	}
}

func TestEveryContainersDefaultsAreCodecsItCarries(t *testing.T) {
	// A container whose own default is not in its own list would refuse
	// every conversion that named no codec, which is the common case.
	for _, s := range containerSpecs {
		if !slices.Contains(s.video, s.defaultVideo) {
			t.Errorf("%s defaults to %s, which it does not carry", s.container, s.defaultVideo)
		}

		if !slices.Contains(s.audio, s.defaultAudio) {
			t.Errorf("%s defaults to %s, which it does not carry", s.container, s.defaultAudio)
		}
	}
}

func TestEveryCodecInEveryContainerHasASpec(t *testing.T) {
	for _, c := range containerSpecs {
		for _, v := range c.video {
			if _, ok := videoByCodec[v]; !ok {
				t.Errorf("%s lists %s, which has no encoder spec", c.container, v)
			}
		}

		for _, a := range c.audio {
			if a == Copy || a == Mute {
				continue
			}

			if _, ok := audioByCodec[a]; !ok {
				t.Errorf("%s lists %s, which has no encoder spec", c.container, a)
			}
		}
	}
}

func TestEveryCodecsDefaultCRFIsInsideItsOwnRange(t *testing.T) {
	// The CRF scales are not comparable between codecs, so a default copied
	// from another codec's range would be silently wrong rather than
	// rejected.
	for _, s := range videoSpecs {
		if s.crf < s.minCRF || s.crf > s.maxCRF {
			t.Errorf("%s defaults to CRF %d, outside its own %d to %d",
				s.codec, s.crf, s.minCRF, s.maxCRF)
		}
	}
}

func TestACRFOutsideTheCodecsRangeIsClampedNotPassedThrough(t *testing.T) {
	// A CRF of 0 on x264 is lossless and enormous; of 63 it is unwatchable.
	// Both are clamped rather than refused, because a client that asks for
	// "maximum quality" means the top of the useful range.
	p, err := buildPlan("in.mp4", "out.mp4",
		Options{Container: MP4, Video: H264, CRF: 1}, gplBuild, withBoth)
	if err != nil {
		t.Fatalf("build a plan: %v", err)
	}

	spec := videoByCodec[H264]

	if got := valueAfter(p.args, "-crf"); got != itoa(spec.minCRF) {
		t.Errorf("CRF 1 became %q, want the codec's minimum %d", got, spec.minCRF)
	}

	p, err = buildPlan("in.mp4", "out.mp4",
		Options{Container: MP4, Video: H264, CRF: 99}, gplBuild, withBoth)
	if err != nil {
		t.Fatalf("build a plan: %v", err)
	}

	if got := valueAfter(p.args, "-crf"); got != itoa(spec.maxCRF) {
		t.Errorf("CRF 99 became %q, want the codec's maximum %d", got, spec.maxCRF)
	}
}

// valueAfter returns the argument following flag, or "" when it is absent.
func valueAfter(args []string, flag string) string {
	for i, arg := range args {
		if arg == flag && i+1 < len(args) {
			return args[i+1]
		}
	}

	return ""
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}

	var digits []byte

	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}

	return string(digits)
}

func TestAResizeOnlyEverScalesDownAndKeepsDimensionsEven(t *testing.T) {
	p, err := buildPlan("in.mp4", "out.mp4",
		Options{Container: MP4, Height: 720}, gplBuild, withBoth)
	if err != nil {
		t.Fatalf("build a plan: %v", err)
	}

	filter := valueAfter(p.args, "-vf")

	// -2 rather than -1 on the open axis: every codec here needs even
	// dimensions, and -1 produces an odd width on a 1080x1920 source.
	if !strings.Contains(filter, "scale=-2:") {
		t.Errorf("the scale filter does not keep the width even: %q", filter)
	}

	// min(,ih) is what makes it downscale-only: upscaling a video produces a
	// larger file that looks worse.
	if !strings.Contains(filter, "min(720,ih)") {
		t.Errorf("the scale filter would upscale a smaller source: %q", filter)
	}
}

func TestAnUnknownContainerIsRefusedByName(t *testing.T) {
	_, err := buildPlan("in.mp4", "out.avi", Options{Container: "avi"}, gplBuild, withBoth)
	if err == nil {
		t.Fatal("an unwritten container was accepted")
	}

	if !strings.Contains(err.Error(), "avi") {
		t.Errorf("the error should name the container, got %v", err)
	}
}

// TestADitheringModeCannotSmuggleFiltersIntoTheGraph is the regression test
// for a real finding: `-lavfi` is its own language, and internal/engine's
// argv safety says nothing about a sub-language ffmpeg parses itself.
//
// The values below are not shell injection — there is no shell — they are
// filtergraph syntax. A comma starts the next filter in a chain, a semicolon
// starts the next chain, and brackets name a pad, so a dither value carrying
// any of them adds filters to the graph rather than choosing a dither mode.
func TestADitheringModeCannotSmuggleFiltersIntoTheGraph(t *testing.T) {
	hostile := []string{
		"none,movie=/app/work/input.mp4",
		"sierra2_4a;[x]null[y]",
		"none[a];[a]scale=1:1",
		"none=1",
		"",
		"NONE",
		"sierra4",
	}

	for _, value := range hostile {
		t.Run(value, func(t *testing.T) {
			if Dither(value).Valid() {
				t.Fatalf("%q was accepted as a dithering mode", value)
			}

			// And the refusal has to say what is accepted, because a bare
			// "invalid" leaves somebody guessing through nine names.
			message := (&UnknownDitherError{Asked: value}).Message()

			for _, want := range []string{"sierra2_4a", "bayer", "none"} {
				if !strings.Contains(message, want) {
					t.Errorf("the refusal does not name %q: %q", want, message)
				}
			}
		})
	}
}

func TestEveryDitherModeInTheEnumIsAccepted(t *testing.T) {
	// The complement of the test above: an allowlist that refuses a mode
	// that works is a bug too, so every name here was confirmed against
	// `ffmpeg -h filter=paletteuse` in the production image.
	for _, d := range Dithers() {
		if !d.Valid() {
			t.Errorf("%q is in the list and not valid", d)
		}
	}

	if len(Dithers()) != 9 {
		t.Errorf("the enum has %d modes; the engine lists nine", len(Dithers()))
	}

	// The default has to be one of them, or every GIF conversion that named
	// no mode would be refused.
	if !defaultGIFDither.Valid() {
		t.Errorf("the default mode %q is not in the enum", defaultGIFDither)
	}
}

// TestNoFilterGraphIsBuiltFromAnUnconstrainedString is the sweep this finding
// prompted, written down as an assertion rather than as a note.
//
// Every value this package splices into -vf, -lavfi or -af is either an
// integer formatted with %d, a float formatted by strconv, or a member of a
// typed enum. The test covers the integer paths: a geometry built from
// Options cannot contain filtergraph syntax, because the fields are ints.
func TestNoFilterGraphIsBuiltFromAnUnconstrainedString(t *testing.T) {
	p, err := buildPlan("in.mp4", "out.mp4",
		Options{Container: MP4, Height: 720, FrameRate: 29.97}, gplBuild, withBoth)
	if err != nil {
		t.Fatalf("build a plan: %v", err)
	}

	filter := valueAfter(p.args, "-vf")

	// One filter, no chain and no pad labels. The comma inside min(720,ih)
	// is expression syntax rather than a chain separator, which is why this
	// asserts on the filter's name and the graph characters rather than
	// counting commas.
	if !strings.HasPrefix(filter, "scale=") {
		t.Errorf("the geometry filter is not a scale: %q", filter)
	}

	if strings.ContainsAny(filter, ";[]") {
		t.Errorf("the scale filter has graph syntax in it: %q", filter)
	}

	// The frame rate goes to -r, which is a codec option rather than part of
	// the graph, so a float there cannot reach the filtergraph at all.
	if rate := valueAfter(p.args, "-r"); strings.ContainsAny(rate, ",;[]=") {
		t.Errorf("the frame rate carries syntax: %q", rate)
	}
}
