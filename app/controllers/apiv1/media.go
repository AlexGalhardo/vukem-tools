package apiv1

import (
	"github.com/AlexGalhardo/vukem-tools/app/tools"
	"github.com/AlexGalhardo/vukem-tools/internal/audio"
	vipsimage "github.com/AlexGalhardo/vukem-tools/internal/image"
	"github.com/AlexGalhardo/vukem-tools/internal/video"
)

// Media is the image, video and audio submit handlers.
//
// It embeds *Conversions rather than extending it, which is a deliberate
// seam. Everything a submit route needs beyond the engines — the ability
// check, the budget check, the upload lookup and the one transaction that
// creates the conversion and attaches its input — already lives on
// *Conversions as submit, and that is exactly one implementation that every
// tool should go through. Embedding promotes it; a copy would be a second
// place for the attach-in-one-transaction rule to drift.
//
// The engine capabilities are here rather than there because they belong to
// this domain. A deploy with no media engines builds this with a Media whose
// capability lists are empty, and every route then answers "this server can
// write nothing" rather than guessing.
type Media struct {
	*Conversions

	media *tools.Media
}

// NewMedia returns the media handlers on top of the conversion handlers.
func NewMedia(c *Conversions, m *tools.Media) *Media {
	return &Media{Conversions: c, media: m}
}

// imageCapabilities returns the probed libvips capabilities, or nothing when
// no media engines were wired.
//
// Nothing rather than a guess: a client told the server can write no format
// shows no output picker, which is the truth on a deploy with no image
// engine, and a hard-coded list would be a promise the binary may not keep.
func (h *Media) imageCapabilities() []vipsimage.Capability {
	if h.media == nil {
		return nil
	}

	return h.media.Image
}

// writableImageFormats is what an image conversion may ask for.
func (h *Media) writableImageFormats() []vipsimage.Format {
	return h.media.WritableImageFormats()
}

// readableImageTypes is what an image conversion may be given.
func (h *Media) readableImageTypes() []string {
	return h.media.ReadableImageTypes()
}

// writableVideoCodecs is what a video conversion may ask for. On an ffmpeg
// built without --enable-gpl this excludes H.264 and H.265, because that
// build has no x264 and no x265 at all.
func (h *Media) writableVideoCodecs() []video.Codec {
	return h.media.WritableVideoCodecs()
}

// writableAudioFormats is what an audio conversion may ask for.
func (h *Media) writableAudioFormats() []audio.Format {
	return h.media.WritableAudioFormats()
}
