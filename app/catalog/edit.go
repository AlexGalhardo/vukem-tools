package catalog

// editCatalog is the cutting, cropping and compression domain (S3.2 and S3.3).
//
// A catalogue entry is written when the tool is planned, not when it ships:
// Status says which. See the package comment for why a missing tool is listed
// rather than silently absent.
var editCatalog = []Tool{
	{
		Name:  "video-clip",
		Title: "Cut and crop a video",
		Summary: "Trims to a range, crops the frame, or splits a long recording into clips of up to " +
			"a minute. Cuts on a keyframe without re-encoding unless you ask for an exact cut.",
		In:       []string{"MP4", "WebM", "MKV", "MOV", "AVI"},
		Out:      []string{"MP4"},
		Group:    GroupVideo,
		Icon:     "video",
		Status:   Live,
		Keywords: []string{"trim", "cut", "split", "shorts", "clips", "crop", "ffmpeg"},
	},
	{
		Name:  "video-compress",
		Title: "Compress a video or audio file",
		Summary: "Makes a video or an audio file smaller and reports exactly how much was saved. " +
			"When it cannot be improved you get the original back, unchanged, and it says so.",
		In:       []string{"MP4", "WebM", "MKV", "MOV", "AVI", "MP3", "WAV", "FLAC", "OGG", "AAC"},
		Out:      []string{"MP4", "MP3"},
		Group:    GroupVideo,
		Icon:     "scale",
		Status:   Live,
		Keywords: []string{"smaller", "shrink", "crf", "bitrate", "optimise", "optimize", "ffmpeg"},
	},
}
