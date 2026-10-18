// Package tools lists the conversions the service offers.
//
// One Tool per entry on the tools page. A tool here is the thin layer between
// the pipeline and a conversion library: it validates its own options, says
// how long it may run and which ability a key needs, and reports progress.
// The conversion itself lives in internal/<name>, so it can be benchmarked
// and fuzzed without a queue, a database or an HTTP request.
package tools

import "github.com/AlexGalhardo/vukem-tools/internal/jobs"

// All returns every registered tool. Adding one here is what makes it
// submittable, listed by GET /api/v1/tools and described in the OpenAPI
// document.
//
// It is empty until the upload and storage path exists: a tool needs somewhere
// to read its input from and somewhere to put its result, and the web service
// and the worker do not share a filesystem (phase S1.7).
func All() []jobs.Tool { return nil }
