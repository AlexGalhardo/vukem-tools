package catalog

// downloadCatalog is the downloads driving yt-dlp domain (S3.1).
//
// A catalogue entry is written when the tool is planned, not when it ships:
// Status says which. See the package comment for why a missing tool is listed
// rather than silently absent.
var downloadCatalog = []Tool{
	{
		Name:  "video-download",
		Title: "Download a video",
		Summary: "Fetches a video from YouTube, Instagram, X or TikTok at the quality you pick, " +
			"keeps the audio only if you prefer, and can split it into short clips.",
		In:    []string{"URL"},
		Out:   []string{"MP4", "MP3", "ZIP"},
		Group: GroupVideo,
		Icon:  "video",
		// Live, but only when yt-dlp was resolved: Grouped downgrades this
		// to Planned when the pipeline has no such tool, which is exactly
		// what an image without yt-dlp produces.
		Status: Live,
		Keywords: []string{
			"youtube", "instagram", "tiktok", "twitter", "x", "url", "yt-dlp",
			"mp3", "audio only", "shorts", "reels", "clips",
		},
	},
}
