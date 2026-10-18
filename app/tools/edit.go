package tools

import (
	"github.com/AlexGalhardo/vukem-tools/internal/files"
	"github.com/AlexGalhardo/vukem-tools/internal/jobs"
)

// editTools is the cutting, cropping and compression domain (S3.2 and S3.3).
//
// Empty until that phase lands. The function exists so the domain can be
// built without touching All, which every other domain is also adding to.
func editTools(*files.Store) []jobs.Tool { return nil }
