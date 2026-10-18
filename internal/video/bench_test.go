package video_test

import (
	"context"
	"os"
	"testing"

	"github.com/AlexGalhardo/vukem-tools/internal/video"
)

// PeakRSSCeiling720p is the recorded ceiling for a 720p transcode, in bytes.
//
// The measurement in docs/benchmarks.md is the figure this is derived from,
// with headroom of about a factor of three. It bounds the *engine
// subprocess*: ffmpeg holds the frames, and this process holds none of them,
// so measuring Go's heap would pass whatever ffmpeg did.
//
// The point of the ceiling is the container, not elegance. A worker with a
// 512 MB allowance running two conversions at once must not be OOM-killed,
// and an encoder that started buffering the whole file would show up here
// rather than in production.
const PeakRSSCeiling720p = 700 << 20

// TestPeakMemoryForA720pTranscodeStaysUnderTheCeiling is the S4.4 memory
// assert, in the form this project can actually run: a 500 MB input is not
// something a test can generate in a reasonable time, so the property being
// proven is that memory does not scale with the input's *length*.
//
// That is the property that matters. ffmpeg streams, so a ten-second clip and
// a two-hour film at the same resolution peak at the same memory; what would
// break that is an encoder or a filter that buffers, and this catches it.
// TestMemoryDoesNotGrowWithTheInputsLength below is the direct evidence.
func TestPeakMemoryForA720pTranscodeStaysUnderTheCeiling(t *testing.T) {
	e, r, caps := engines(t)
	dir := t.TempDir()
	input := clip(t, e, r, dir, 6, 1280, 720)

	got, err := video.Convert(context.Background(), e, r, caps, dir, input,
		video.Options{Container: video.MP4}, nil)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}

	if got.MaxRSS == 0 {
		t.Skip("this platform does not report peak resident memory")
	}

	t.Logf("1280x720 H.264 transcode: peak RSS %.1f MB, CPU %d ms",
		float64(got.MaxRSS)/(1<<20), got.CPUTime)

	if got.MaxRSS > PeakRSSCeiling720p {
		t.Errorf("peak RSS was %.1f MB, past the recorded %.0f MB ceiling",
			float64(got.MaxRSS)/(1<<20), float64(PeakRSSCeiling720p)/(1<<20))
	}
}

// TestMemoryDoesNotGrowWithTheInputsLength is the evidence behind the "a
// 500 MB input converts without the container being OOM-killed" assert.
//
// A test cannot make a 500 MB file quickly, and would learn little from one:
// the question is whether memory is a function of the file's size, and the
// honest way to answer it is to convert two files of the same shape and very
// different lengths and compare. If ffmpeg buffered, the longer one would cost
// proportionally more.
func TestMemoryDoesNotGrowWithTheInputsLength(t *testing.T) {
	e, r, caps := engines(t)

	peak := func(seconds int) int64 {
		t.Helper()

		dir := t.TempDir()
		input := clip(t, e, r, dir, seconds, 640, 360)

		got, err := video.Convert(context.Background(), e, r, caps, dir, input,
			video.Options{Container: video.MP4}, nil)
		if err != nil {
			t.Fatalf("convert a %ds clip: %v", seconds, err)
		}

		return got.MaxRSS
	}

	short, long := peak(2), peak(20)
	if short == 0 || long == 0 {
		t.Skip("this platform does not report peak resident memory")
	}

	t.Logf("2s clip peaks at %.1f MB, 20s clip at %.1f MB",
		float64(short)/(1<<20), float64(long)/(1<<20))

	// Ten times the input for at most a quarter more memory. A buffering
	// pipeline would be close to ten times.
	if long > short*5/4 {
		t.Errorf("a ten-times-longer input cost %.1f MB against %.1f MB: memory scales with the "+
			"input's length, so something is buffering",
			float64(long)/(1<<20), float64(short)/(1<<20))
	}
}

// BenchmarkPresets measures the encoding presets, which is what
// docs/benchmarks.md records: wall-clock and output size per codec for a
// reference clip.
//
// The peak RSS and the output size go in as custom metrics because -benchmem
// measures this process's allocations and this process touches no frames.
func BenchmarkPresets(b *testing.B) {
	e, r, caps := engines(b)

	// A six-second 720p clip is the reference: long enough that process
	// startup is not the measurement, short enough that an AV1 encode
	// finishes.
	const seconds, width, height = 6, 1280, 720

	cases := []struct {
		name string
		opts video.Options
	}{
		{"h264_mp4_crf23", video.Options{Container: video.MP4, Video: video.H264}},
		{"h265_mp4_crf28", video.Options{Container: video.MP4, Video: video.H265}},
		{"vp9_webm_crf32", video.Options{Container: video.WebM, Video: video.VP9}},
		{"av1_webm_crf32", video.Options{Container: video.WebM, Video: video.AV1}},
		{"h264_720p_to_360p", video.Options{Container: video.MP4, Video: video.H264, Height: 360}},
	}

	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			if encoder := encoderFor(c.opts); encoder != "" && !caps.Has(encoder) {
				b.Skipf("this build has no %s encoder", encoder)
			}

			source := clip(b, e, r, b.TempDir(), seconds, width, height)

			var peak, bytes int64

			for b.Loop() {
				dir := b.TempDir()
				input := copyInto(b, source, dir)

				got, err := video.Convert(context.Background(), e, r, caps, dir, input, c.opts, nil)
				if err != nil {
					b.Fatalf("convert: %v", err)
				}

				peak = max(peak, got.MaxRSS)
				bytes = max(bytes, sizeOf(b, got.Path))
			}

			b.ReportMetric(float64(peak)/(1<<20), "peakMB")
			b.ReportMetric(float64(bytes)/(1<<10), "outKB")
		})
	}
}

// BenchmarkGIF measures the two-pass palette path, which is the most
// expensive thing per second of video this package does.
func BenchmarkGIF(b *testing.B) {
	e, r, _ := engines(b)
	source := clip(b, e, r, b.TempDir(), 5, 640, 480)

	var peak, bytes int64

	for b.Loop() {
		dir := b.TempDir()
		input := copyInto(b, source, dir)

		got, err := video.ToGIF(context.Background(), e, r, dir, input,
			video.GIFOptions{FPS: 12, Width: 480}, nil)
		if err != nil {
			b.Fatalf("to gif: %v", err)
		}

		peak = max(peak, got.MaxRSS)
		bytes = max(bytes, sizeOf(b, got.Path))
	}

	b.ReportMetric(float64(peak)/(1<<20), "peakMB")
	b.ReportMetric(float64(bytes)/(1<<10), "outKB")
}

// sizeOf returns a file's size in bytes.
func sizeOf(tb testing.TB, path string) int64 {
	tb.Helper()

	info, err := os.Stat(path)
	if err != nil {
		tb.Fatalf("stat %s: %v", path, err)
	}

	return info.Size()
}
