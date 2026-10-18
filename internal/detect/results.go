package detect

import "slices"

// resultOnly are types this service produces but does not accept.
//
// The distinction exists because the two lists are used for different things.
// `known` is what an upload may be: every entry in it is a type the upload
// endpoint accepts, because routes/uploads.go hands the ingester
// detect.Types(). These are results — the text a PDF was converted to, the
// comparison report two documents produced — and nothing here is a type any
// tool reads.
//
// Putting them in `known` instead would widen the upload gate, and widen it
// in the worst direction: `mimetype` reports text/plain for anything that is
// not recognisably binary, so accepting it would turn "unrecognised files are
// refused at the door" into "any text file is accepted". The gate is the
// cheapest defence the service has, and a result type is not a reason to open
// it.
var resultOnly = map[string]Kind{
	"text/plain":    {"text/plain", ".txt", "a text file"},
	"text/markdown": {"text/markdown", ".md", "a Markdown file"},
}

// ResultTypes returns every type this service produces but does not accept,
// sorted, for the API documentation.
func ResultTypes() []string {
	out := make([]string, 0, len(resultOnly))
	for mime := range resultOnly {
		out = append(out, mime)
	}

	slices.Sort(out)

	return out
}
