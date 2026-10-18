package tools

import (
	"github.com/AlexGalhardo/vukem-tools/internal/files"
	"github.com/AlexGalhardo/vukem-tools/internal/jobs"
)

// downloadTools is the downloads driving yt-dlp domain (S3.1).
//
// Empty until that phase lands. The function exists so the domain can be
// built without touching All, which every other domain is also adding to.
func downloadTools(*files.Store) []jobs.Tool { return nil }
