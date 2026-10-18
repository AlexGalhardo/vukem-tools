package editor

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/AlexGalhardo/vukem-tools/internal/engine"
)

// Media is what ffprobe said about a file.
//
// Every number in it is read from the file rather than taken from the
// request. That is the whole reason this type exists: a crop or a trim
// validated against what the client claimed is not validated at all.
type Media struct {
	// Format is ffprobe's container name, e.g. "mov,mp4,m4a,3gp,3g2,mj2".
	Format string `json:"format"`
	// Duration is the container's duration.
	Duration time.Duration `json:"duration"`
	// Width and Height are the first video stream's real dimensions, zero
	// when there is no video.
	Width  int `json:"width"`
	Height int `json:"height"`
	// VideoCodec and AudioCodec are the codec names, empty when absent.
	VideoCodec string `json:"video_codec"`
	AudioCodec string `json:"audio_codec"`
	// Bitrate is the container's overall bitrate in bits per second.
	Bitrate int64 `json:"bitrate"`
}

// HasVideo reports whether there is a video stream to crop or scale.
func (m Media) HasVideo() bool { return m.VideoCodec != "" && m.Width > 0 && m.Height > 0 }

// HasAudio reports whether there is an audio stream.
func (m Media) HasAudio() bool { return m.AudioCodec != "" }

// Dimensions renders the real frame size the way a refusal names it.
func (m Media) Dimensions() string { return strconv.Itoa(m.Width) + "x" + strconv.Itoa(m.Height) }

// probeDocument is the part of ffprobe's JSON this package reads.
type probeDocument struct {
	Format struct {
		FormatName string `json:"format_name"`
		Duration   string `json:"duration"`
		BitRate    string `json:"bit_rate"`
	} `json:"format"`
	Streams []struct {
		CodecType string `json:"codec_type"`
		CodecName string `json:"codec_name"`
		Width     int    `json:"width"`
		Height    int    `json:"height"`
		Duration  string `json:"duration"`
	} `json:"streams"`
}

// Probe reads a file's real shape.
//
// dir is the engine's working directory; path is the file, which must be
// inside it. ffprobe is given -v error so its only output is the JSON, which
// keeps the decode from having to skip a banner.
func (e *Editor) Probe(ctx context.Context, dir, path string) (Media, error) {
	var out strings.Builder

	if _, err := e.run.Run(ctx, engine.Command{
		Path: e.ffprobe,
		Args: []string{
			"-v", "error",
			"-print_format", "json",
			"-show_format",
			"-show_streams",
			"-i", path,
		},
		Dir:    dir,
		Stdout: &out,
	}); err != nil {
		return Media{}, Classify(err)
	}

	return parseProbe(out.String())
}

// parseProbe decodes ffprobe's document.
//
// It is separate from Probe and exported through the test so the decoding can
// be checked against real ffprobe output without an ffmpeg installation.
func parseProbe(raw string) (Media, error) {
	var doc probeDocument
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &doc); err != nil {
		return Media{}, fail(ReasonUnsupportedInput, "That file is not a video this tool can read.",
			"decode ffprobe's JSON: "+err.Error(), err)
	}

	m := Media{
		Format:   doc.Format.FormatName,
		Duration: parseSeconds(doc.Format.Duration),
		Bitrate:  parseInt(doc.Format.BitRate),
	}

	for i := range doc.Streams {
		s := doc.Streams[i]

		switch s.CodecType {
		case "video":
			if m.VideoCodec != "" {
				continue // the first video stream is the one that is edited
			}

			m.VideoCodec, m.Width, m.Height = s.CodecName, s.Width, s.Height

			// A container without a duration still has one on its streams,
			// which is the normal case for a stream remuxed from a live
			// source.
			if m.Duration == 0 {
				m.Duration = parseSeconds(s.Duration)
			}
		case "audio":
			if m.AudioCodec == "" {
				m.AudioCodec = s.CodecName
			}
		}
	}

	if !m.HasVideo() && !m.HasAudio() {
		return Media{}, fail(ReasonUnsupportedInput, "That file has no video or audio to edit.",
			"ffprobe reported no usable stream", nil)
	}

	return m, nil
}

// parseSeconds reads one of ffprobe's duration strings, which are decimal
// seconds and are "N/A" when the container does not say.
func parseSeconds(s string) time.Duration {
	s = strings.TrimSpace(s)
	if s == "" || s == "N/A" {
		return 0
	}

	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f <= 0 {
		return 0
	}

	return time.Duration(f * float64(time.Second))
}

// parseInt reads one of ffprobe's integer strings.
func parseInt(s string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || n < 0 {
		return 0
	}

	return n
}

// keyframeWindow is how much of the stream is examined when looking for the
// keyframe a cut would land on.
//
// Listing every keyframe of a two-hour recording is tens of thousands of
// packets for a question about one instant, so ffprobe is asked for a short
// interval around the cut instead. Six seconds covers any sane keyframe
// interval, including the 5-second one YouTube uses.
const keyframeWindow = 6 * time.Second

// keyframeNear returns the keyframe timestamp closest to at, and whether one
// was found in the window around it.
func (e *Editor) keyframeNear(ctx context.Context, dir, path string, at time.Duration) (time.Duration, bool) {
	from := max(at-keyframeWindow/2, 0)

	var out strings.Builder

	if _, err := e.run.Run(ctx, engine.Command{
		Path: e.ffprobe,
		Args: []string{
			"-v", "error",
			"-select_streams", "v:0",
			"-show_entries", "packet=pts_time,flags",
			"-read_intervals", fmt.Sprintf("%s%%+%s", seconds(from), seconds(keyframeWindow)),
			"-print_format", "json",
			"-i", path,
		},
		Dir:    dir,
		Stdout: &out,
	}); err != nil {
		// A probe that fails is not an edit that fails: the answer only
		// decides between copying and re-encoding, and re-encoding is always
		// correct. Reporting "not found" picks the safe branch.
		e.log.DebugContext(ctx, "editor: could not list keyframes, will re-encode", "err", err)

		return 0, false
	}

	return nearestKeyframe(out.String(), at)
}

// packetDocument is the part of ffprobe's packet listing this package reads.
type packetDocument struct {
	Packets []struct {
		PTSTime string `json:"pts_time"`
		Flags   string `json:"flags"`
	} `json:"packets"`
}

// nearestKeyframe returns the keyframe closest to at in a packet listing.
//
// A keyframe is a packet whose flags contain "K". Reading the flag rather
// than trusting a frame listing is what makes this cheap: packets carry the
// flag without the file being decoded.
func nearestKeyframe(raw string, at time.Duration) (time.Duration, bool) {
	var doc packetDocument
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &doc); err != nil {
		return 0, false
	}

	var (
		best  time.Duration
		found bool
	)

	for i := range doc.Packets {
		p := doc.Packets[i]

		if !strings.Contains(p.Flags, "K") {
			continue
		}

		t := parseSeconds(p.PTSTime)

		// pts_time 0 is a real keyframe at the start of the stream, so an
		// unparseable timestamp has to be told apart from it: parseSeconds
		// returns 0 for both, and only the first packet can legitimately be
		// at zero.
		if t == 0 && i > 0 {
			continue
		}

		if !found || absDuration(t-at) < absDuration(best-at) {
			best, found = t, true
		}
	}

	return best, found
}

// absDuration is the absolute value of a duration.
func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}

	return d
}

// seconds renders a duration the way ffmpeg and ffprobe want it on the
// command line: decimal seconds, no unit.
func seconds(d time.Duration) string {
	return strconv.FormatFloat(d.Seconds(), 'f', 3, 64)
}
