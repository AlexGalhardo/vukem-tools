package catalog

// videoCatalog is the video conversion domain (S4.4).
//
// A catalogue entry is written when the tool is planned, not when it ships:
// Status says which. See the package comment for why a missing tool is listed
// rather than silently absent.
//
// `video-convert` is not here. It is already in catalog.go, in the slice
// named pixelCatalog, which holds the whole original catalogue rather than
// only the pixel-art domain — so adding it here would list it twice. The same
// is true of `image-convert` and `audio-convert`. Moving those three entries
// into their own domain's slice is a change to catalog.go, which this domain
// does not own; see the report that accompanies this branch. Until then those
// three read as Planned even once registered, because Grouped only ever
// downgrades a Live entry and never promotes a Planned one.
var videoCatalog = []Tool{
	{
		Name:  "video-to-gif",
		Title: "Video to GIF",
		Summary: "Turns a few seconds of a video into an animated GIF, with a palette computed " +
			"from the clip itself rather than a fixed one.",
		In:    []string{"MP4", "WebM", "MKV", "MOV", "AVI"},
		Out:   []string{"GIF"},
		Group: GroupVideo,
		Icon:  "video",
		// Live here means "this tool exists and is registered". Grouped
		// turns it back into Planned when the pipeline does not have it,
		// which is what keeps the page honest on a deploy whose engines are
		// missing.
		Status:   Live,
		Keywords: []string{"gif", "animation", "loop", "clip", "palette", "ffmpeg"},
	},
}
