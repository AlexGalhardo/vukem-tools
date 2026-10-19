package detect

import (
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/gabriel-vasile/mimetype"
)

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
	// What a file extracted from an archive is called when its bytes match
	// nothing more specific. An archive may hold any type at all, and a
	// member whose type this service does not otherwise name still has to be
	// storable and downloadable — refusing the whole archive over one
	// unrecognised member would make the tool useless on real archives.
	//
	// Result-only is load-bearing here rather than incidental: the upload
	// gate reads Types(), which reads `known`, so this entry cannot be
	// uploaded. Putting it in `known` would turn "unrecognised files are
	// refused at the door" into "anything at all is accepted", which is the
	// single cheapest defence this service has.
	"application/octet-stream": {"application/octet-stream", ".bin", "a file"},
}

// OfResult identifies a file this service produced rather than received.
//
// Of is the input identifier: it consults `known` only, so a type the upload
// gate does not accept comes back as ErrUnknown. That is right at the door and
// wrong on the way out — a file extracted from an archive is produced, not
// received, and a .txt member must be storable even though a .txt upload is
// refused. So this walks `known` first and then `resultOnly`, and anything
// still unmatched is a plain file rather than an error: an archive may hold
// any type at all, and refusing the whole archive over one unrecognised member
// would make extraction useless on real archives.
//
// It is never used on an upload. Doing so would accept everything, because
// the fallback matches anything.
func OfResult(r io.Reader) (Kind, error) {
	head := make([]byte, HeaderBytes)

	n, err := io.ReadFull(r, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return Kind{}, fmt.Errorf("detect: read the header: %w", err)
	}

	for t := mimetype.Detect(head[:n]); t != nil; t = t.Parent() {
		clean := cleanMIME(t.String())

		if k, ok := known[clean]; ok {
			return k, nil
		}

		if k, ok := resultOnly[clean]; ok {
			return k, nil
		}
	}

	return resultOnly["application/octet-stream"], nil
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
