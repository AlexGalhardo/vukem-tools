package tools

import (
	"github.com/AlexGalhardo/vukem-tools/internal/files"
	"github.com/AlexGalhardo/vukem-tools/internal/jobs"
)

// audioTools is the audio conversion and transcription domain (S4.4 and S4.5).
//
// Empty until that phase lands. The function exists so the domain can be
// built without touching All, which every other domain is also adding to.
func audioTools(*files.Store) []jobs.Tool { return nil }
