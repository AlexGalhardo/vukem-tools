// Package tools lists the conversions the service offers.
//
// One Tool per entry on the tools page. A tool here is the thin layer between
// the pipeline and a conversion library: it validates its own options, says
// how long it may run and which ability a key needs, and reports progress.
// The conversion itself lives in internal/<name>, so it can be benchmarked
// and fuzzed without a queue, a database or an HTTP request.
package tools

import (
	"slices"

	"github.com/AlexGalhardo/vukem-tools/internal/files"
	"github.com/AlexGalhardo/vukem-tools/internal/jobs"
)

// All returns every registered tool. Registering one is what makes it
// submittable, listed by GET /api/v1/tools and described in the OpenAPI
// document.
//
// One function per domain, each in its own file, rather than one slice
// literal here: domains are built in parallel, and a single literal makes
// every tool a conflict with every other tool being added at the same time.
// Adding a domain is one line here and one new file.
//
// Engines carries the resolved external programs, the runner that applies
// their limits and the configuration they are tuned from. It is passed only
// to the domains that drive a subprocess; a domain whose conversions are
// in-process (the pixel tools) does not take it.
//
// m is the probed media capability set (ResolveMedia). The image, video and
// audio domains offer only the formats this build can actually produce, so
// they need the probe rather than the registry: an operation being compiled
// in and a format being writable are different facts.
func All(f *files.Store, e Engines, m *Media) []jobs.Tool {
	return slices.Concat(
		pixelTools(f),
		ImageTools(f, m),
		VideoTools(f, m),
		AudioTools(f, m),
		downloadTools(f, e),
		editTools(f, e),
		pdfTools(f),
		spriteTools(f),
	)
}

// pixelTools is the pixel-art domain (S2).
func pixelTools(f *files.Store) []jobs.Tool {
	return []jobs.Tool{NewPixelSVG(f)}
}
