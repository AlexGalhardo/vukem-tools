package routes

import (
	"github.com/AlexGalhardo/vukem/openapi"
)

// The OpenAPI examples for the download, cut and compress operations (S3).
//
// They live beside the routes they document rather than in the two maps in
// apidocs.go, which every domain would otherwise edit at once. apidocs.go
// calls applyDomainExamples once per domain, which is one line per domain
// instead of a conflict per tool.
//
// workaround(vukem): openapi.MediaType carries a schema and nothing else, and
// no struct tag feeds an example, so an example can only go in the JSON
// Schema `examples` keyword of the schema itself. That is also why these are
// written by hand rather than derived from the Go types.

// DomainExamples is one domain's contribution to the document: examples for
// its component schemas and for its request bodies.
type DomainExamples struct {
	// Components maps a component schema name — the Go type's name — to its
	// example.
	Components map[string]any
	// Requests maps an operationId to its request body example.
	Requests map[string]any
}

// applyDomainExamples attaches one domain's examples to the document.
func applyDomainExamples(doc *openapi.Document, ex DomainExamples) {
	for name, example := range ex.Components {
		if schema, ok := doc.Components.Schemas[name]; ok {
			schema["examples"] = []any{example}
		}
	}

	for _, methods := range doc.Paths {
		for _, op := range methods {
			example, ok := ex.Requests[op.OperationID]
			if !ok || op.RequestBody == nil {
				continue
			}

			for _, mt := range op.RequestBody.Content {
				mt.Schema["examples"] = []any{example}
			}
		}
	}
}

// downloadExamples is the S3 domain's examples.
func downloadExamples() DomainExamples {
	return DomainExamples{
		Components: map[string]any{
			"MediaView":        mediaExample(),
			"MediaFormat":      mediaFormatExample(),
			"PlatformList":     map[string]any{"data": []any{platformExample()}},
			"PlatformView":     platformExample(),
			"CapabilitiesView": capabilitiesExample(),
			"EngineView":       engineExample(),
			"LimitsView":       limitsExample(),
			"NameList":         map[string]any{"data": []any{"trim", "split"}},
		},
		Requests: map[string]any{
			"tools.video-download": map[string]any{
				"idempotency_key": "weekly-highlights-7",
				"options": map[string]any{
					"url":          "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
					"kind":         "video",
					"quality":      "best",
					"compress":     false,
					"clips":        false,
					"clip_seconds": 60,
				},
			},
			"tools.video-download.metadata": map[string]any{
				"url": "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
			},
			"tools.video-clip": map[string]any{
				"file_id":         "01JQ8Z5X0K7M3N9P2R4T6V8W0Y",
				"idempotency_key": "opening-shot",
				"options": map[string]any{
					"mode":             "trim",
					"start_seconds":    12.5,
					"duration_seconds": 30,
					"frame_accurate":   false,
					"crop": map[string]any{
						"x":      0,
						"y":      0,
						"width":  1280,
						"height": 720,
					},
					"clip_seconds": 60,
				},
			},
			"tools.video-compress": map[string]any{
				"file_id":         "01JQ8Z5X0K7M3N9P2R4T6V8W0Y",
				"idempotency_key": "shrink-the-recording",
			},
		},
	}
}

// mediaExample is what a platform says about a video. The sizes are a real
// shape rather than round numbers, so a client reading the example can see
// that estimated_bytes is an estimate.
func mediaExample() map[string]any {
	return map[string]any{
		"platform":         "youtube",
		"title":            "Never Gonna Give You Up",
		"uploader":         "Rick Astley",
		"thumbnail":        "https://i.ytimg.com/vi/dQw4w9WgXcQ/maxresdefault.jpg",
		"duration_seconds": 212.0,
		"estimated_bytes":  36_438_016,
		"max_bytes":        524_288_000,
		"too_large":        false,
		"formats":          []any{mediaFormatExample()},
	}
}

func mediaFormatExample() map[string]any {
	return map[string]any{
		"id":           "22",
		"ext":          "mp4",
		"resolution":   "1280x720",
		"vcodec":       "avc1.64001F",
		"acodec":       "mp4a.40.2",
		"bytes":        36_438_016,
		"bitrate_kbps": 1_376.5,
	}
}

func platformExample() map[string]any {
	return map[string]any{
		"name":  "youtube",
		"label": "YouTube",
		"hosts": []any{"youtube.com", "youtu.be", "youtube-nocookie.com"},
	}
}

// capabilitiesExample shows an image that has every engine. One with a
// missing engine reports present false and an empty version, and its tools
// are absent from the list.
func capabilitiesExample() map[string]any {
	return map[string]any{
		"engines": []any{
			map[string]any{"name": "yt-dlp", "present": true, "version": "2026.08.19"},
			map[string]any{
				"name":    "ffmpeg",
				"present": true,
				"version": "ffmpeg version 7.1.5-0+deb13u1 Copyright (c) 2000-2026 the FFmpeg developers",
			},
			map[string]any{
				"name":    "ffprobe",
				"present": true,
				"version": "ffprobe version 7.1.5-0+deb13u1 Copyright (c) 2007-2026 the FFmpeg developers",
			},
			map[string]any{"name": "gs", "present": true, "version": "10.05.1"},
		},
		"tools":  []any{"pixels-to-svg", "video-clip", "video-compress", "video-download"},
		"limits": limitsExample(),
	}
}

func engineExample() map[string]any {
	return map[string]any{"name": "yt-dlp", "present": true, "version": "2026.08.19"}
}

func limitsExample() map[string]any {
	return map[string]any{
		"max_download_bytes": 524_288_000,
		"max_clip_seconds":   60,
		"max_clips":          60,
		"max_output_seconds": 1_800,
	}
}
