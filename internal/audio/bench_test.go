package audio_test

import (
	"context"
	"os"
	"testing"

	"github.com/AlexGalhardo/vukem-tools/internal/audio"
)

// PeakRSSCeilingAudio is the recorded ceiling for an audio conversion, in
// bytes.
//
// Audio is cheap: ffmpeg holds a few frames of samples whatever the
// recording's length. The ceiling exists so a filter that started buffering
// the whole file shows up here rather than on a worker converting an
// audiobook. It bounds the engine subprocess, not this process.
const PeakRSSCeilingAudio = 256 << 20

// TestPeakMemoryForAnAudioConversionStaysUnderTheCeiling bounds the engine
// subprocess, which is where the samples are.
func TestPeakMemoryForAnAudioConversionStaysUnderTheCeiling(t *testing.T) {
	e, r, caps := engines(t)
	dir := t.TempDir()
	input := tone(t, e, r, dir, 10, "1.0")

	got, err := audio.Convert(context.Background(), e, r, caps, dir, input,
		audio.Options{To: audio.MP3}, nil)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}

	if got.MaxRSS == 0 {
		t.Skip("this platform does not report peak resident memory")
	}

	t.Logf("10s FLAC to MP3: peak RSS %.1f MB, CPU %d ms",
		float64(got.MaxRSS)/(1<<20), got.CPUTime)

	if got.MaxRSS > PeakRSSCeilingAudio {
		t.Errorf("peak RSS was %.1f MB, past the recorded %.0f MB ceiling",
			float64(got.MaxRSS)/(1<<20), float64(PeakRSSCeilingAudio)/(1<<20))
	}
}

// BenchmarkConvert measures each output format for a reference recording, and
// the two-pass normalisation against the single pass — the one option here
// whose cost is a whole extra decode.
func BenchmarkConvert(b *testing.B) {
	e, r, caps := engines(b)

	// Thirty seconds, so the measurement is the encoding rather than process
	// startup.
	const seconds = 30

	cases := []struct {
		name string
		opts audio.Options
	}{
		{"mp3_192k", audio.Options{To: audio.MP3}},
		{"opus_96k", audio.Options{To: audio.Opus}},
		{"aac_160k", audio.Options{To: audio.AAC}},
		{"flac_level8", audio.Options{To: audio.FLAC}},
		{"wav_pcm16", audio.Options{To: audio.WAV}},
		{"mp3_normalise_one_pass", audio.Options{To: audio.MP3, Normalise: true}},
		{
			"mp3_normalise_two_pass",
			audio.Options{To: audio.MP3, Normalise: true, TwoPassNormalise: true},
		},
	}

	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			if !slicesContains(audio.Available(caps), c.opts.To) {
				b.Skipf("this build cannot encode %s", c.opts.To)
			}

			source := tone(b, e, r, b.TempDir(), seconds, "0.5")

			var peak, bytes int64

			for b.Loop() {
				dir := b.TempDir()
				input := copyInto(b, source, dir)

				got, err := audio.Convert(context.Background(), e, r, caps, dir, input, c.opts, nil)
				if err != nil {
					b.Fatalf("convert: %v", err)
				}

				peak = max(peak, got.MaxRSS)

				info, err := os.Stat(got.Path)
				if err != nil {
					b.Fatalf("stat the result: %v", err)
				}

				bytes = max(bytes, info.Size())
			}

			b.ReportMetric(float64(peak)/(1<<20), "peakMB")
			b.ReportMetric(float64(bytes)/(1<<10), "outKB")
		})
	}
}
