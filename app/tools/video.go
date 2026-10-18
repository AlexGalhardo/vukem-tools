package tools

import (
	"github.com/AlexGalhardo/vukem-tools/internal/files"
	"github.com/AlexGalhardo/vukem-tools/internal/jobs"
)

// videoTools is the video conversion domain (S4.4).
//
// Empty until that phase lands. The function exists so the domain can be
// built without touching All, which every other domain is also adding to.
func videoTools(*files.Store) []jobs.Tool { return nil }
