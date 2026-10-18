// Package abilities names what an API key is allowed to do.
//
// One ability per tool family, so a key minted for a script that only converts
// images cannot start a video download. The list lives here, in one place: a
// route asks for its ability by name and the issuing endpoint refuses anything
// that is not on this list, which keeps a typo from minting a key that grants
// nothing and fails only at call time.
package abilities

import "slices"

// The abilities, in the order the roadmap introduces their tools.
const (
	PixelsSVG       = "pixels:svg"
	VideoDownload   = "video:download"
	VideoEdit       = "video:edit"
	VideoConvert    = "video:convert"
	AudioConvert    = "audio:convert"
	ImageConvert    = "image:convert"
	ImageCompress   = "image:compress"
	ImageEdit       = "image:edit"
	PDFCompress     = "pdf:compress"
	VideoTranscribe = "video:transcribe"
	SpriteSheet     = "sprite:sheet"
)

// all is the closed set, built from one slice per domain so that a domain can
// add its abilities in its own file.
var all = slices.Concat(coreAbilities, pdfAbilities)

// coreAbilities are the abilities declared in this file.
var coreAbilities = []string{
	PixelsSVG,
	VideoDownload,
	VideoEdit,
	VideoConvert,
	AudioConvert,
	ImageConvert,
	ImageCompress,
	ImageEdit,
	PDFCompress,
	VideoTranscribe,
	SpriteSheet,
}

// All returns every ability a key may be granted, in a stable order.
func All() []string { return slices.Clone(all) }

// Valid reports whether ability is one this app defines.
//
// The wildcard "*" that auth.Token.Can understands is deliberately not valid
// here: a key issued through the API always lists what it may do, so revoking
// one tool never means revoking a key that silently had everything.
func Valid(ability string) bool { return slices.Contains(all, ability) }

// Unknown returns the requested abilities this app does not define, in the
// order they were requested, so the caller is told exactly what was wrong.
func Unknown(requested []string) []string {
	var bad []string

	for _, a := range requested {
		if !Valid(a) {
			bad = append(bad, a)
		}
	}

	return bad
}
