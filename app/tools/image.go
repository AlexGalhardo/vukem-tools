package tools

import (
	"github.com/AlexGalhardo/vukem-tools/internal/files"
	"github.com/AlexGalhardo/vukem-tools/internal/jobs"
)

// imageTools is the raster images: convert, compress, resize, crop, rotate domain (S4.1 to S4.3).
//
// Empty until that phase lands. The function exists so the domain can be
// built without touching All, which every other domain is also adding to.
func imageTools(*files.Store) []jobs.Tool { return nil }
