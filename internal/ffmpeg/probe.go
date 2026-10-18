package ffmpeg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/AlexGalhardo/vukem-tools/internal/engine"
)

// Media is what ffprobe reports about a file.
//
// It is the one source of the duration, which every progress percentage and
// every duration limit is computed from, and the one source of the real
// stream dimensions, which a crop is validated against. Both are read from
// the file rather than from anything a client said.
type Media struct {
	// FormatName is ffprobe's container name, which is a comma-separated
	// list for the families it cannot tell apart: an MP4 reports
	// "mov,mp4,m4a,3gp,3g2,mj2".
	FormatName string
	// Duration is the container's duration. Zero when the container does
	// not carry one, which happens with a raw stream and with some MKVs,
	// and which is why a progress percentage has to cope with no total.
	Duration time.Duration
	// BitRate is the container's overall bit rate in bits per second, zero
	// when absent.
	BitRate int64
	// Video and Audio are the first stream of each kind, which is what every
	// tool here operates on. A file with four audio tracks is not something
	// this service edits.
	Video *Stream
	Audio *Stream
	// Streams is every stream, so a caller can say "this file has no audio"
	// or count subtitle tracks.
	Streams []Stream
}

// Stream is one elementary stream.
type Stream struct {
	Index int
	// Type is "video", "audio", "subtitle" or whatever else ffprobe found.
	Type string
	// Codec is the codec name, e.g. "h264", "aac", "vp9".
	Codec string
	// Width and Height are the coded dimensions, zero for audio.
	Width  int
	Height int
	// FrameRate is the average frame rate in frames per second, zero when
	// ffprobe reports none.
	FrameRate float64
	// Channels and SampleRate describe an audio stream.
	Channels   int
	SampleRate int
	// BitRate is the stream's bit rate in bits per second, zero when absent.
	BitRate int64
	// Duration is the stream's own duration, which can differ from the
	// container's.
	Duration time.Duration
}

// HasVideo reports whether there is a video stream to work on.
func (m Media) HasVideo() bool { return m.Video != nil }

// HasAudio reports whether there is an audio stream to work on.
func (m Media) HasAudio() bool { return m.Audio != nil }

// Describe is a one-line summary for a log or an owner-only error detail.
func (m Media) Describe() string {
	parts := []string{m.FormatName, m.Duration.Round(time.Millisecond).String()}

	if m.Video != nil {
		parts = append(parts, fmt.Sprintf("video %s %dx%d @%.3f",
			m.Video.Codec, m.Video.Width, m.Video.Height, m.Video.FrameRate))
	}

	if m.Audio != nil {
		parts = append(parts, fmt.Sprintf("audio %s %d ch %d Hz",
			m.Audio.Codec, m.Audio.Channels, m.Audio.SampleRate))
	}

	return strings.Join(parts, ", ")
}

// ErrNoStreams means ffprobe read the file and found nothing to convert,
// which is what a renamed text file or a truncated upload looks like.
var ErrNoStreams = errors.New("ffmpeg: the file holds no audio or video stream")

// ProbeMedia reads a file's container and streams with ffprobe.
//
// JSON rather than ffprobe's default output, and `-v error` so the JSON is
// the only thing on stdout: parsing ffprobe's human-readable output is the
// same mistake as scraping ffmpeg's stderr for progress, and it breaks on the
// first version that reorders a line.
func ProbeMedia(ctx context.Context, e Engine, r Runner, dir, path string) (Media, error) {
	var out strings.Builder

	if _, err := r.Run(ctx, engine.Command{
		Path: e.FFprobe,
		Args: []string{
			"-v", "error",
			"-print_format", "json",
			"-show_format",
			"-show_streams",
			// ffprobe can open a network input too, and for the same reason
			// as ffmpeg it is not allowed to.
			"-protocol_whitelist", "file",
			path,
		},
		Dir:    dir,
		Stdout: &out,
		Env:    Env(),
	}); err != nil {
		return Media{}, Classify(err, "read the file's streams")
	}

	return parseProbe(out.String())
}

// probeJSON mirrors the part of ffprobe's output this package reads.
//
// Every numeric field is a string in ffprobe's JSON, including the ones that
// are obviously numbers, and several are absent rather than zero. So they are
// decoded as strings and converted deliberately: a json.Number would still
// fail on the absent ones, and failing a probe because a container carries no
// bit rate would refuse perfectly good files.
type probeJSON struct {
	Format struct {
		FormatName string `json:"format_name"`
		Duration   string `json:"duration"`
		BitRate    string `json:"bit_rate"`
	} `json:"format"`

	Streams []struct {
		Index         int    `json:"index"`
		CodecName     string `json:"codec_name"`
		CodecType     string `json:"codec_type"`
		Width         int    `json:"width"`
		Height        int    `json:"height"`
		AvgFrameRate  string `json:"avg_frame_rate"`
		RFrameRate    string `json:"r_frame_rate"`
		Channels      int    `json:"channels"`
		SampleRateRaw string `json:"sample_rate"`
		BitRate       string `json:"bit_rate"`
		Duration      string `json:"duration"`
	} `json:"streams"`
}

// parseProbe turns ffprobe's JSON into a Media.
func parseProbe(raw string) (Media, error) {
	var decoded probeJSON

	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return Media{}, fmt.Errorf("ffmpeg: read ffprobe's output: %w", err)
	}

	m := Media{
		FormatName: decoded.Format.FormatName,
		Duration:   seconds(decoded.Format.Duration),
		BitRate:    integer(decoded.Format.BitRate),
	}

	for i := range decoded.Streams {
		s := &decoded.Streams[i]

		stream := Stream{
			Index:      s.Index,
			Type:       s.CodecType,
			Codec:      s.CodecName,
			Width:      s.Width,
			Height:     s.Height,
			FrameRate:  rate(s.AvgFrameRate, s.RFrameRate),
			Channels:   s.Channels,
			SampleRate: int(integer(s.SampleRateRaw)),
			BitRate:    integer(s.BitRate),
			Duration:   seconds(s.Duration),
		}

		m.Streams = append(m.Streams, stream)

		switch {
		case stream.Type == "video" && m.Video == nil:
			// The index matters for the stream map later, so the stored
			// value is the one from the file rather than a copy of the loop
			// variable's position.
			first := stream
			m.Video = &first
		case stream.Type == "audio" && m.Audio == nil:
			first := stream
			m.Audio = &first
		}
	}

	// A container with no duration is normal; a container with no stream at
	// all is not a media file, whatever its name says.
	if m.Video == nil && m.Audio == nil {
		return Media{}, ErrNoStreams
	}

	// The container's duration is missing often enough — raw streams, some
	// MKVs, a file still being written — that falling back to the longest
	// stream is worth it: without a total there is no progress percentage.
	if m.Duration == 0 {
		for _, s := range m.Streams {
			m.Duration = max(m.Duration, s.Duration)
		}
	}

	return m, nil
}

// seconds reads ffprobe's fractional-second duration.
//
// "N/A" is what ffprobe prints for a duration it does not know, and it has to
// read as zero rather than as an error: a file with no duration is still
// convertible.
func seconds(raw string) time.Duration {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || value <= 0 {
		return 0
	}

	return time.Duration(value * float64(time.Second))
}

// integer reads one of ffprobe's string-encoded integers.
func integer(raw string) int64 {
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || value < 0 {
		return 0
	}

	return value
}

// rate reads a frame rate, which ffprobe gives as a rational like "30000/1001".
//
// The average rate is preferred over the real-time one because it is what a
// variable-frame-rate file actually plays at, and the fallback matters: a
// still image stream reports "0/0" for the average and a usable value for the
// other.
func rate(average, realTime string) float64 {
	for _, raw := range []string{average, realTime} {
		numerator, denominator, ok := strings.Cut(strings.TrimSpace(raw), "/")
		if !ok {
			continue
		}

		n, errN := strconv.ParseFloat(numerator, 64)
		d, errD := strconv.ParseFloat(denominator, 64)

		if errN != nil || errD != nil || d == 0 || n <= 0 {
			continue
		}

		return n / d
	}

	return 0
}
