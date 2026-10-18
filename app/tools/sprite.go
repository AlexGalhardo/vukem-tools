package tools

import (
	"github.com/AlexGalhardo/vukem-tools/internal/files"
	"github.com/AlexGalhardo/vukem-tools/internal/jobs"
)

// spriteTools is the sprite sheets and frame atlases domain (S5).
//
// Empty until that phase lands. The function exists so the domain can be
// built without touching All, which every other domain is also adding to.
func spriteTools(*files.Store) []jobs.Tool { return nil }
