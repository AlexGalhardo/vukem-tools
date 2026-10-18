package pdf

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"
)

// structureTimeout bounds one structural invocation. qpdf rewrites the object
// graph rather than re-rendering pages, so even a large document is seconds
// of work; a run approaching this bound is a document it is struggling with.
const structureTimeout = 10 * time.Minute

// MaxMergeInputs caps how many documents one merge may join.
//
// Every input is validated before the merge runs, and each one is an engine
// invocation of its own, so the cap bounds the probe as much as the merge. It
// is also a command line: qpdf is given one argument per input.
const MaxMergeInputs = 50

// MergeRequest is one merge.
type MergeRequest struct {
	// Dir is the job's working directory. Every path must be inside it.
	Dir string
	// Inputs are the documents to join, in the order they should appear.
	Inputs []string
	// Output is where the joined document goes.
	Output string
	// Progress, when set, is called with the number of inputs validated so
	// far. Validation is the part that scales with the input count, so it is
	// the part worth reporting.
	Progress func(done, total int)
}

// MergeResult is what a merge produced.
type MergeResult struct {
	// Pages is the joined document's page count, which is the sum of the
	// inputs'.
	Pages int
	// InputPages is each input's page count, in order.
	InputPages []int
	// OutlineKept says whether the result carries a table of contents. Only
	// the first document's survives, so this is false when the first document
	// had none — the tool page states this rather than implying every
	// bookmark is kept.
	OutlineKept bool
}

// Merge joins documents into one.
//
// The first input is given to qpdf as the base document and the rest are
// appended through `--pages`, rather than building from `--empty`. The
// difference matters and is not obvious: building from empty produces a
// document with no catalog of its own, which means **no outline at all** —
// every bookmark in every input is silently lost. Using the first input as
// the base keeps its outline, its metadata and its named destinations, and
// keeps every input's link annotations. What it cannot keep is the second and
// later documents' bookmarks, because there is nowhere to graft them onto;
// MergeResult says so and the tool page repeats it, which is what ROADMAP
// S6.1 asks for in place of a silently broken table of contents.
func (e *Engines) Merge(ctx context.Context, req MergeRequest) (MergeResult, error) {
	switch {
	case len(req.Inputs) < 2:
		return MergeResult{}, &OptionError{Field: "file_ids", Reason: "Merging needs at least two documents."}
	case len(req.Inputs) > MaxMergeInputs:
		return MergeResult{}, &OptionError{
			Field:  "file_ids",
			Reason: "A merge takes at most " + strconv.Itoa(MaxMergeInputs) + " documents.",
		}
	}

	counts := make([]int, 0, len(req.Inputs))
	total := 0

	for i, input := range req.Inputs {
		doc, err := e.Inspect(ctx, req.Dir, input)
		if err != nil {
			return MergeResult{}, fmt.Errorf("pdf: document %d of %d: %w", i+1, len(req.Inputs), err)
		}

		counts = append(counts, doc.Count)
		total += doc.Count

		if req.Progress != nil {
			req.Progress(i+1, len(req.Inputs))
		}
	}

	if total > MaxPages {
		return MergeResult{}, &TooManyPagesError{Pages: total, Limit: MaxPages}
	}

	hadOutline, err := e.hasOutline(ctx, req.Dir, req.Inputs[0])
	if err != nil {
		return MergeResult{}, err
	}

	// "." names the base document, so page one of the output is page one of
	// the first input.
	args := []string{req.Inputs[0], "--pages", "."}
	args = append(args, req.Inputs[1:]...)
	args = append(args, "--", req.Output)

	if err := e.qpdf(ctx, req.Dir, args); err != nil {
		return MergeResult{}, err
	}

	return MergeResult{Pages: total, InputPages: counts, OutlineKept: hadOutline}, nil
}

// SelectRequest is one page selection: extract, remove or reorder.
type SelectRequest struct {
	Dir    string
	Input  string
	Output string
	// Pages are the 1-based pages to keep, in the order they should appear.
	Pages []int
	// SourcePages is the input's own page count, from Inspect.
	SourcePages int
}

// Select writes a document holding Pages, in that order.
//
// It is the one operation behind "extract pages", "remove pages" and
// "organise": all three reduce to a list of pages to keep, which is also why
// they share a test.
//
// Whether the outline survives is decided here rather than left to the
// engine, and the decision is the interesting part. qpdf can keep the base
// document's catalog, which carries the outline — but an outline entry
// pointing at a page the selection dropped is a bookmark that goes nowhere,
// and ROADMAP S6.1 names exactly that as the regression nobody notices. So
// the outline is kept only when every page is still present, which is true of
// a reordering and false of an extraction; otherwise the result is built from
// empty, which drops the outline cleanly. A missing table of contents is
// honest. A broken one is not.
func (e *Engines) Select(ctx context.Context, req SelectRequest) error {
	if len(req.Pages) == 0 {
		return &OptionError{Field: "pages", Reason: "That would leave a document with no pages."}
	}

	if highest(req.Pages) > req.SourcePages {
		return &PageRangeError{
			Input: QPDFRange(req.Pages),
			Reason: "That selection names page " + strconv.Itoa(highest(req.Pages)) +
				", but this document has " + strconv.Itoa(req.SourcePages) + ".",
		}
	}

	rng := QPDFRange(req.Pages)

	var args []string

	if isPermutation(req.Pages, req.SourcePages) {
		args = []string{req.Input, "--pages", ".", rng, "--", req.Output}
	} else {
		args = []string{"--empty", "--pages", req.Input, rng, "--", req.Output}
	}

	return e.qpdf(ctx, req.Dir, args)
}

// KeepsOutline reports whether a selection of these pages will keep the
// document's outline, so the tool can tell the user before they convert.
func KeepsOutline(pages []int, sourcePages int) bool { return isPermutation(pages, sourcePages) }

// isPermutation reports whether pages holds every page of the document
// exactly once, in any order.
func isPermutation(pages []int, sourcePages int) bool {
	if len(pages) != sourcePages {
		return false
	}

	sorted := slices.Clone(pages)
	slices.Sort(sorted)

	for i, page := range sorted {
		if page != i+1 {
			return false
		}
	}

	return true
}

// SplitRequest is one split.
type SplitRequest struct {
	Dir   string
	Input string
	// SourcePages is the input's page count, from Inspect.
	SourcePages int
	// Groups are the parts to produce, each a list of 1-based pages. Use
	// GroupEvery for "every N pages" and ParsePageRange per part for explicit
	// ranges.
	Groups [][]int
	// Prefix names the output files: prefix-1.pdf, prefix-2.pdf, and so on,
	// inside Dir.
	Prefix string
	// Progress, when set, is called as each part is written.
	Progress func(done, total int)
}

// MaxSplitParts caps how many files one split may produce.
//
// Every part is a file the user has five minutes to download (the service's
// result lifetime), and a thousand links is not a thing anybody downloads in
// five minutes. It is also one engine invocation per part when the parts are
// explicit ranges, so the cap bounds the work as well as the result.
const MaxSplitParts = 500

// SplitPart is one file a split produced.
type SplitPart struct {
	// Path is the file on local disk.
	Path string
	// Pages are the pages it holds, for naming it.
	Pages []int
}

// Split writes one document per group.
//
// Each part is built from empty, so none of them carries the original's
// outline: a part holds a subset of the pages, and every bookmark pointing
// outside that subset would be a bookmark to nowhere. The tool page says so.
func (e *Engines) Split(ctx context.Context, req SplitRequest) ([]SplitPart, error) {
	switch {
	case len(req.Groups) == 0:
		return nil, &OptionError{Field: "mode", Reason: "That split would produce no files."}
	case len(req.Groups) > MaxSplitParts:
		return nil, &OptionError{
			Field: "mode",
			Reason: "That split would produce " + strconv.Itoa(len(req.Groups)) +
				" files. The limit is " + strconv.Itoa(MaxSplitParts) +
				", because every result has to be downloaded within five minutes.",
		}
	}

	parts := make([]SplitPart, 0, len(req.Groups))

	for i, group := range req.Groups {
		if len(group) == 0 {
			return nil, &OptionError{Field: "ranges", Reason: "One of those ranges names no pages."}
		}

		if highest(group) > req.SourcePages {
			return nil, &PageRangeError{
				Input: QPDFRange(group),
				Reason: "That range names page " + strconv.Itoa(highest(group)) +
					", but this document has " + strconv.Itoa(req.SourcePages) + ".",
			}
		}

		out := filepath.Join(req.Dir, req.Prefix+"-"+strconv.Itoa(i+1)+".pdf")

		err := e.qpdf(ctx, req.Dir, []string{"--empty", "--pages", req.Input, QPDFRange(group), "--", out})
		if err != nil {
			return nil, err
		}

		parts = append(parts, SplitPart{Path: out, Pages: group})

		if req.Progress != nil {
			req.Progress(i+1, len(req.Groups))
		}
	}

	return parts, nil
}

// RotateRequest is one rotation.
type RotateRequest struct {
	Dir    string
	Input  string
	Output string
	// Degrees is 90, 180 or 270, clockwise.
	Degrees int
	// Pages are the 1-based pages to rotate. Empty rotates every page.
	Pages []int
	// SourcePages is the input's page count.
	SourcePages int
	// Absolute sets the rotation rather than adding to what the page already
	// has. Adding is the default, because a user asking to turn a page
	// ninety degrees means ninety degrees from where it is now.
	Absolute bool
}

// validRotations are the only rotations a PDF page may carry: /Rotate is a
// multiple of 90.
var validRotations = []int{90, 180, 270}

// Rotate turns pages.
//
// It keeps everything else: qpdf edits each page's /Rotate entry and rewrites
// nothing, so the outline, the named destinations, the links and the content
// streams come out exactly as they went in. That is why rotation does not go
// through Select even though both take a page list — going through `--pages`
// would rebuild the page tree for no reason and cost the outline.
func (e *Engines) Rotate(ctx context.Context, req RotateRequest) error {
	if !slices.Contains(validRotations, req.Degrees) {
		return &OptionError{Field: "degrees", Reason: "Rotate by 90, 180 or 270 degrees."}
	}

	if highest(req.Pages) > req.SourcePages {
		return &PageRangeError{
			Input: QPDFRange(req.Pages),
			Reason: "That page range names page " + strconv.Itoa(highest(req.Pages)) +
				", but this document has " + strconv.Itoa(req.SourcePages) + ".",
		}
	}

	// qpdf's spelling: "+90" adds to the page's current rotation, "90" sets
	// it. The page list after the colon is optional and means every page when
	// absent.
	spec := "+" + strconv.Itoa(req.Degrees)
	if req.Absolute {
		spec = strconv.Itoa(req.Degrees)
	}

	if len(req.Pages) > 0 {
		spec += ":" + QPDFRange(req.Pages)
	}

	return e.qpdf(ctx, req.Dir, []string{"--rotate=" + spec, req.Input, req.Output})
}

// qpdf runs one qpdf invocation and maps its exit code onto this package's
// errors.
//
// Exit 3 is accepted: qpdf uses it for warnings, and a document it had to
// reconstruct is the entire point of the repair tool. Exit 2 is a document it
// could not use, which is a damaged input rather than an engine fault — the
// distinction decides whether the conversion is retried, and retrying a
// damaged file three times only wastes three workers.
func (e *Engines) qpdf(ctx context.Context, dir string, args []string) error {
	_, err := e.run(ctx, command{
		engine:  EngineQPDF,
		args:    args,
		dir:     dir,
		timeout: structureTimeout,
	})
	if err == nil {
		return nil
	}

	engineErr, ok := errors.AsType[*EngineError](err)
	if ok {
		if usableExit(engineErr.Code) {
			return nil
		}

		return &DamagedError{Detail: engineErr.Error()}
	}

	return err
}

// hasOutline reports whether a document carries a table of contents.
func (e *Engines) hasOutline(ctx context.Context, dir, path string) (bool, error) {
	var decoded struct {
		Outlines []struct {
			Title string `json:"title"`
		} `json:"outlines"`
	}

	err := e.runJSON(ctx, command{
		engine:  EngineQPDF,
		args:    []string{"--json=latest", "--json-key=outlines", path},
		dir:     dir,
		timeout: inspectTimeout,
	}, &decoded)
	if err != nil {
		return false, err
	}

	return len(decoded.Outlines) > 0, nil
}

// OutlineTitles returns the document's top-level bookmark titles, which is
// what a test checks when it has to prove an outline survived an operation.
func (e *Engines) OutlineTitles(ctx context.Context, dir, path string) ([]string, error) {
	var decoded struct {
		Outlines []struct {
			Title string `json:"title"`
		} `json:"outlines"`
	}

	err := e.runJSON(ctx, command{
		engine:  EngineQPDF,
		args:    []string{"--json=latest", "--json-key=outlines", path},
		dir:     dir,
		timeout: inspectTimeout,
	}, &decoded)
	if err != nil {
		return nil, err
	}

	out := make([]string, 0, len(decoded.Outlines))
	for _, entry := range decoded.Outlines {
		out = append(out, entry.Title)
	}

	return out, nil
}

// removeAll deletes files an operation produced before it failed, so a job
// directory never holds a half-written result that a later step might store.
func removeAll(paths []string) {
	for _, path := range paths {
		_ = os.Remove(path)
	}
}
