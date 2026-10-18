package catalog

// audioCatalog is the audio conversion and transcription domain (S4.4 and
// S4.5).
//
// A catalogue entry is written when the tool is planned, not when it ships:
// Status says which. See the package comment for why a missing tool is listed
// rather than silently absent.
//
// `audio-convert` is not here for the same reason `image-convert` is not in
// imageCatalog: its entry is already in catalog.go's pixelCatalog slice, and
// repeating it would list it twice.
//
// Transcription has no entry at all yet. whisper.cpp and its model are not in
// the production image, so there is nothing to describe and no date to
// promise; adding a Planned entry for it is the owner's call, since it needs
// a model pinned at build time.
var audioCatalog = []Tool{}
