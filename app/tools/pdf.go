package tools

import (
	"github.com/AlexGalhardo/vukem-tools/internal/files"
	"github.com/AlexGalhardo/vukem-tools/internal/jobs"
)

// pdfTools is the PDF domain (S6).
//
// Empty until that phase lands. The function exists so the domain can be
// built without touching All, which every other domain is also adding to.
func pdfTools(*files.Store) []jobs.Tool { return nil }
