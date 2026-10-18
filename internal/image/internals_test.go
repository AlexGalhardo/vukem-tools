package image

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/AlexGalhardo/vukem-tools/internal/engine"
)

// The tests in this file need no engine: they cover the argument building,
// the header parsing and the error classification, which are the parts that
// decide what libvips is asked to do. They run everywhere, which matters
// because the engine-backed tests only run inside the production image.

// recorder is a Runner that records the commands it was given and answers
// from a script, so a test can assert on an argument list without a binary.
type recorder struct {
	commands []engine.Command
	// stdout is what each successive call writes to the command's Stdout,
	// which is how a fake vipsheader answers.
	stdout []string
	err    error
}

func (f *recorder) Run(_ context.Context, cmd engine.Command) (engine.Result, error) {
	f.commands = append(f.commands, cmd)

	if index := len(f.commands) - 1; index < len(f.stdout) && cmd.Stdout != nil {
		if _, err := cmd.Stdout.Write([]byte(f.stdout[index])); err != nil {
			return engine.Result{}, err
		}
	}

	return engine.Result{}, f.err
}

// headerOf is a vipsheader answer for an image of this size.
func headerOf(width, height int) string {
	return strings.Join([]string{
		"input.png: " + itoa(width) + "x" + itoa(height) + " uchar, 3 bands, srgb, pngload",
		"width: " + itoa(width),
		"height: " + itoa(height),
		"bands: 3",
		"format: uchar",
		"interpretation: srgb",
		"vips-loader: pngload",
		"",
	}, "\n")
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

func TestTheConvertCommandAsksForShrinkOnLoadAndSRGB(t *testing.T) {
	fake := &recorder{stdout: []string{headerOf(1600, 1200), "", headerOf(800, 600)}}
	e := Engine{Vips: "/usr/bin/vips", VipsHeader: "/usr/bin/vipsheader"}

	_, err := Convert(context.Background(), e, fake, t.TempDir(), "input.png", Options{
		To:     JPEG,
		Resize: ResizeFit,
		Width:  800,
	})
	if err != nil {
		t.Fatalf("convert: %v", err)
	}

	if len(fake.commands) != 3 {
		t.Fatalf("ran %d commands, want three: probe, convert, probe", len(fake.commands))
	}

	convert := fake.commands[1]
	argv := strings.Join(convert.Args, " ")

	// thumbnail rather than copy: it is the only operation that reads with
	// shrink-on-load, which is the whole memory argument for this package.
	if convert.Args[0] != "thumbnail" {
		t.Errorf("the conversion uses %q, want thumbnail", convert.Args[0])
	}

	for _, want := range []string{
		"access=sequential",     // a few scanlines in memory, not the frame
		"--export-profile srgb", // convert to sRGB before the strip
		"--fail-on error",       // refuse a half-decoded image
		"keep=none",             // EXIF stripped by default
		"optimize-coding=true",  // the JPEG encoder settings reach the saver
		"--size down",           // no upscaling unless asked
	} {
		if !strings.Contains(argv, want) {
			t.Errorf("the command is missing %q\ngot: %s", want, argv)
		}
	}

	// The environment is set from the container's CPU quota, not left to
	// libvips's own guess at the host's core count.
	if !hasPrefix(convert.Env, "VIPS_CONCURRENCY=") {
		t.Errorf("VIPS_CONCURRENCY is not set: %v", convert.Env)
	}
}

func hasPrefix(env []string, prefix string) bool {
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			return true
		}
	}

	return false
}

func TestKeepingMetadataNeverKeepsTheOldProfile(t *testing.T) {
	// The pixels have already been converted to sRGB, so keeping the
	// original ICC profile would describe the new numbers with the old
	// profile and shift every colour. "all" would do exactly that.
	if got := keepFlags(true); strings.Contains(got, "icc") || strings.Contains(got, "all") {
		t.Errorf("keep=%q would re-tag sRGB pixels with the source profile", got)
	}

	if got := keepFlags(false); got != "none" {
		t.Errorf("keep=%q, want none when metadata is stripped", got)
	}
}

func TestAnAnimatedInputOnlyLoadsEveryFrameWhenItIsAsked(t *testing.T) {
	animated := Metadata{Width: 100, Height: 100, Pages: 12}

	if got := loadSuffix(animated, Options{To: GIF, KeepAnimation: true}); !strings.Contains(got, "n=-1") {
		t.Errorf("load options %q should ask for every page", got)
	}

	if got := loadSuffix(animated, Options{To: GIF}); strings.Contains(got, "n=-1") {
		t.Errorf("load options %q should read one page by default", got)
	}

	// A still output format cannot hold the frames, so loading them all
	// would be pure waste.
	if got := loadSuffix(animated, Options{To: JPEG, KeepAnimation: true}); strings.Contains(got, "n=-1") {
		t.Errorf("load options %q should not read every page for a still output", got)
	}
}

func TestTheHeaderParserReadsAnAnimatedFramesRealHeight(t *testing.T) {
	// libvips stacks an animation's pages vertically, so the height field is
	// the whole strip. A user's "the image is 100x50" means one frame.
	raw := strings.Join([]string{
		"a.gif: 100x600 uchar, 4 bands, srgb, gifload",
		"width: 100",
		"height: 600",
		"bands: 4",
		"n-pages: 12",
		"vips-loader: gifload",
		"orientation: 6",
		"icc-profile-data: 3144 bytes",
	}, "\n")

	got, err := parseHeader(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if got.Width != 100 || got.Height != 50 {
		t.Errorf("got %dx%d, want 100x50 per frame", got.Width, got.Height)
	}

	if got.Pages != 12 || !got.Animated() {
		t.Errorf("pages = %d, animated = %v", got.Pages, got.Animated())
	}

	if !got.HasProfile {
		t.Error("the embedded profile was not noticed, so it would be stripped without a transform")
	}

	if got.Orientation != 6 {
		t.Errorf("orientation = %d, want 6", got.Orientation)
	}
}

func TestAHeaderWithNoSizeIsAnError(t *testing.T) {
	if _, err := parseHeader("vips: unable to load: not a known file format"); !errors.Is(err, errNoHeader) {
		t.Errorf("want errNoHeader, got %v", err)
	}
}

func TestAMissingPageCountMeansOnePage(t *testing.T) {
	got, err := parseHeader("width: 10\nheight: 10\nbands: 3\nvips-loader: pngload")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if got.Pages != 1 {
		t.Errorf("pages = %d, want 1", got.Pages)
	}

	if got.Orientation != 1 {
		t.Errorf("orientation = %d, want 1 when the tag is absent", got.Orientation)
	}
}

func TestTheCapabilityProbeMatchesWholeOperationNames(t *testing.T) {
	// jpegsave appears inside jpegsave_target. A listing holding only the
	// buffer and target variants must not report the format as writable to
	// a file.
	// The real shape of a `vips -l` line: the class, the nickname in
	// parentheses, then a description that is itself full of parenthesised
	// extensions.
	listing := strings.Join([]string{
		"      VipsForeignLoadJpegBuffer (jpegload_buffer), load jpeg from buffer (.jpg, .jpeg)",
		"      VipsForeignSaveJpegTarget (jpegsave_target), save image to jpeg target (.jpg, .jpeg)",
		"      VipsForeignLoadPngFile (pngload), load png from file (.png), priority=200",
		"      VipsForeignSaveSpngFile (pngsave), save image to file as PNG (.png), priority=0",
	}, "\n")

	got := capabilitiesFrom(listing)

	for _, c := range got {
		switch c.Format {
		case JPEG:
			if c.Read || c.Write {
				t.Errorf("JPEG reported read=%v write=%v from buffer-only operations", c.Read, c.Write)
			}
		case PNG:
			if !c.Read || !c.Write {
				t.Errorf("PNG reported read=%v write=%v, want both", c.Read, c.Write)
			}
		case BMP, ICO:
			if c.Write {
				t.Errorf("%s is reported writable, and libvips has no saver for it", c.Format)
			}
		case WebP, AVIF, JXL, HEIC, GIF, TIFF:
			if c.Read || c.Write {
				t.Errorf("%s is reported available and is not in the listing", c.Format)
			}
		}
	}
}

func TestEveryWritableFormatHasEncoderDefaults(t *testing.T) {
	// A format whose saver exists but whose defaults are missing would be
	// written with libvips's own settings, which for JPEG means Q=75 and a
	// visibly softer image than this project ships.
	for _, s := range specs {
		if s.saver == "" {
			continue
		}

		d, ok := defaults[s.format]
		if !ok {
			t.Errorf("%s has a saver and no encoder defaults", s.format)
			continue
		}

		if d.quality && (d.Quality < 1 || d.Quality > 100) {
			t.Errorf("%s has quality %d, outside 1 to 100", s.format, d.Quality)
		}

		if d.effort && (d.Effort < d.minEffort || d.Effort > d.maxEffort) {
			t.Errorf("%s has effort %d, outside its own %d to %d range",
				s.format, d.Effort, d.minEffort, d.maxEffort)
		}
	}
}

func TestEveryDeclaredFormatMapsToARecognisedMIMEType(t *testing.T) {
	for _, s := range specs {
		got, ok := FormatOf(s.mime)
		if !ok {
			t.Errorf("%s declares %q, which maps to no format", s.format, s.mime)
			continue
		}

		// AVIF and HEIC share a loader but not a MIME type, so the mapping
		// has to be one to one in this direction.
		if got != s.format {
			t.Errorf("%q maps to %s, want %s", s.mime, got, s.format)
		}
	}
}

func TestLibvipsErrorsAreClassifiedFromItsOwnWords(t *testing.T) {
	cases := []struct {
		stderr string
		want   EngineErrorKind
	}{
		{"vips__file_read: file has zero length", KindOther},
		{"VipsJpeg: Premature end of JPEG file", KindUnreadable},
		{"vips_image_new_from_file: is not a known file format", KindUnsupported},
		{"vips_tracked_malloc: out of memory --- size == 1200000000", KindOutOfMemory},
		{"vips__file_write: write error --- no space left on device", KindDiskFull},
		{"something nobody has seen before", KindOther},
	}

	for _, c := range cases {
		err := classify(&engine.ExitError{Code: 1, Stderr: c.stderr}, "convert the image")

		var engineErr *EngineError
		if !errors.As(err, &engineErr) {
			t.Fatalf("classify returned %T", err)
		}

		if engineErr.Kind != c.want {
			t.Errorf("%q classified as %s, want %s", c.stderr, engineErr.Kind, c.want)
		}

		// The engine's own text stays on the error for the owner and out of
		// the message a user would see.
		if engineErr.Stderr != c.stderr {
			t.Errorf("the engine's output was lost: %q", engineErr.Stderr)
		}
	}
}

func TestATimeoutIsNotMistakenForABadFile(t *testing.T) {
	// A killed engine exits non-zero, and reporting that as "the converter
	// rejected your file" would send the user looking at a file that is
	// fine.
	err := classify(engine.ErrTimeout, "convert the image")

	var engineErr *EngineError
	if !errors.As(err, &engineErr) || engineErr.Kind != KindTimeout {
		t.Fatalf("want a timeout, got %v", err)
	}
}

func TestTheTargetSizeIsTheImagesOwnWhenNothingIsResized(t *testing.T) {
	source := Metadata{Width: 1234, Height: 567}

	width, height, size := target(source, Options{To: PNG})
	if width != 1234 || height != 567 || size != "down" {
		t.Errorf("got %dx%d %s, want the image's own size, downsize only", width, height, size)
	}
}

func TestAnOpenAxisInFitModeDoesNotConstrainAnything(t *testing.T) {
	source := Metadata{Width: 4000, Height: 3000}

	width, height, _ := target(source, Options{To: PNG, Resize: ResizeFit, Width: 800})
	if width != 800 {
		t.Errorf("width = %d, want 800", width)
	}

	if height < source.Height {
		t.Errorf("height bound = %d, which would constrain a %d tall image", height, source.Height)
	}
}

func TestACropIsCheckedAgainstTheRealDimensions(t *testing.T) {
	source := Metadata{Width: 100, Height: 100}

	cases := []struct {
		name string
		rect Rect
		ok   bool
	}{
		{"inside", Rect{Left: 0, Top: 0, Width: 100, Height: 100}, true},
		{"one pixel", Rect{Left: 99, Top: 99, Width: 1, Height: 1}, true},
		{"past the right edge", Rect{Left: 50, Top: 0, Width: 51, Height: 10}, false},
		{"past the bottom", Rect{Left: 0, Top: 50, Width: 10, Height: 51}, false},
		{"negative origin", Rect{Left: -1, Top: 0, Width: 10, Height: 10}, false},
		{"zero width", Rect{Left: 0, Top: 0, Width: 0, Height: 10}, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := checkCrop(c.rect, source)
			if (err == nil) != c.ok {
				t.Errorf("checkCrop = %v, want ok=%v", err, c.ok)
			}
		})
	}
}

func TestAnInvalidRotationIsRefusedBeforeAnyProcessStarts(t *testing.T) {
	fake := &recorder{}

	_, err := Edit(context.Background(), Engine{Vips: "vips", VipsHeader: "vipsheader"}, fake,
		t.TempDir(), "in.png", EditOptions{Rotate: 45, Encode: Options{To: PNG}})
	if err == nil {
		t.Fatal("a 45 degree rotation was accepted")
	}

	if len(fake.commands) != 0 {
		t.Errorf("%d processes were started for an invalid rotation", len(fake.commands))
	}
}

// TestAPathCannotSmuggleLibvipsLoadOptions is the image-side half of the
// filtergraph finding.
//
// libvips takes its options inside the filename — `in.jpg[access=sequential]`
// is the documented interface — so a filename is a sub-language, and a path
// containing a bracket is one libvips parses options out of. argv safety is
// irrelevant: there is no shell, and libvips interprets the string itself.
// libvips offers no escape for the syntax, so such a path is refused.
func TestAPathCannotSmuggleLibvipsLoadOptions(t *testing.T) {
	hostile := []string{
		"holiday[shrink=8].jpg",
		"in.jpg[access=random",
		"/app/work/x[page=3].gif",
	}

	for _, path := range hostile {
		t.Run(path, func(t *testing.T) {
			fake := &recorder{}

			_, err := Convert(context.Background(),
				Engine{Vips: "vips", VipsHeader: "vipsheader"}, fake, t.TempDir(), path,
				Options{To: PNG})

			if !errors.Is(err, ErrPathHasOptionSyntax) {
				t.Fatalf("want ErrPathHasOptionSyntax, got %v", err)
			}

			if len(fake.commands) != 0 {
				t.Errorf("%d processes were started for a hostile path", len(fake.commands))
			}
		})
	}

	// An ordinary path still works, or the guard would refuse everything.
	fake := &recorder{stdout: []string{headerOf(10, 10), "", headerOf(10, 10)}}

	if _, err := Convert(context.Background(),
		Engine{Vips: "vips", VipsHeader: "vipsheader"}, fake, t.TempDir(),
		"/app/work/input.png", Options{To: PNG}); err != nil {
		t.Errorf("an ordinary path was refused: %v", err)
	}
}

// TestEverySaveOptionIsBuiltFromATableOrAClampedInteger is the sweep result,
// as an assertion: nothing caller-supplied reaches the libvips option
// sub-language as a free string.
func TestEverySaveOptionIsBuiltFromATableOrAClampedInteger(t *testing.T) {
	// The option list is built from the format's own defaults plus Quality
	// and Effort, both integers clamped to the saver's range. An absurd
	// value therefore becomes a number in range rather than anything else.
	for _, quality := range []int{-5000, 0, 1, 50, 100, 999999} {
		got := saveSuffix(Options{To: JPEG, Quality: quality, Effort: quality})

		if strings.ContainsAny(got, " \t\n\"'`$;") {
			t.Errorf("Q=%d produced option syntax: %q", quality, got)
		}

		if !strings.HasPrefix(got, "[") || !strings.HasSuffix(got, "]") {
			t.Errorf("Q=%d produced a malformed option list: %q", quality, got)
		}
	}
}
