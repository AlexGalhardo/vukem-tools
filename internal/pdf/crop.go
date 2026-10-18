package pdf

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"strconv"
	"strings"
)

// CropRequest is one crop.
type CropRequest struct {
	Dir    string
	Input  string
	Output string
	// Pages are the 1-based pages to crop. Empty crops every page.
	Pages []int
	// Margins trims this many points off each edge of the page's current
	// box. It is the option a person actually wants — "take 20 points off
	// every side" — and it works on pages of different sizes.
	Margins Margins
	// Absolute, when it has area, sets the crop box to exactly these
	// coordinates instead of applying Margins. It is for a client that has
	// measured the box it wants.
	Absolute Box
}

// Margins is how much to trim from each edge, in points.
type Margins struct {
	Left, Bottom, Right, Top float64
}

// Any reports whether any margin is set.
func (m Margins) Any() bool {
	return m.Left != 0 || m.Bottom != 0 || m.Right != 0 || m.Top != 0
}

// CropResult is what a crop produced.
type CropResult struct {
	// Cropped is how many pages were changed.
	Cropped int
	// Boxes is the new box of each changed page, in page order, for the
	// report.
	Boxes []Box
}

// maxCropJSONBytes caps the structural JSON a crop reads.
//
// It is tighter than the general cap because a crop decodes the JSON rather
// than scanning past it: the page dictionaries and the page-tree nodes are
// retained, and the bound on that is MaxPages, but the bound on what the
// decoder walks past has to be stated too.
const maxCropJSONBytes = 32 << 20

// Crop sets a crop box on pages.
//
// There is no qpdf option for this, which is why it goes the long way round:
// qpdf is asked to print the document's object graph as JSON, the page
// dictionaries in it are given a /CropBox, and qpdf is asked to apply those
// objects back onto the original file. The result is the original document
// with a few dictionary entries changed — every content stream, every link,
// the outline and the metadata are untouched, because nothing else was
// rewritten.
//
// The alternative would have been Ghostscript with -dFIXEDMEDIA and a
// PageOffset, which re-renders the whole document to change a rectangle: it
// would lose the outline, re-encode every image and only ever crop uniformly.
// Changing four numbers is both more correct and faster.
//
// /CropBox rather than /MediaBox, which is the distinction that matters for
// not losing anything: the crop box is what a reader displays and prints,
// while the media box is the sheet the content lives on. Setting the crop box
// hides the margins; setting the media box would throw them away, and a crop
// is meant to be undoable.
func (e *Engines) Crop(ctx context.Context, req CropRequest) (CropResult, error) {
	if !req.Margins.Any() && req.Absolute.Empty() {
		return CropResult{}, &OptionError{
			Field:  "margins",
			Reason: "Say how much to trim, or give the box to crop to.",
		}
	}

	doc, err := e.Inspect(ctx, req.Dir, req.Input)
	if err != nil {
		return CropResult{}, err
	}

	pages := req.Pages
	if len(pages) == 0 {
		pages = sequence(1, doc.Count)
	}

	if highest(pages) > doc.Count {
		return CropResult{}, &PageRangeError{
			Input: QPDFRange(pages),
			Reason: "That page range names page " + strconv.Itoa(highest(pages)) +
				", but this document has " + strconv.Itoa(doc.Count) + ".",
		}
	}

	graph, err := e.readGraph(ctx, req.Dir, req.Input)
	if err != nil {
		return CropResult{}, err
	}

	update, boxes, err := graph.cropped(doc, pages, req)
	if err != nil {
		return CropResult{}, err
	}

	path, err := writeJSONFile(req.Dir, update)
	if err != nil {
		return CropResult{}, err
	}

	defer func() { _ = os.Remove(path) }()

	if err := e.qpdf(ctx, req.Dir, []string{"--update-from-json=" + path, req.Input, req.Output}); err != nil {
		return CropResult{}, err
	}

	return CropResult{Cropped: len(boxes), Boxes: boxes}, nil
}

// objectGraph is the part of a document's structure a crop needs: the page
// dictionaries, the page-tree nodes above them, and the header qpdf requires
// an update to repeat back.
type objectGraph struct {
	// header is the first element of qpdf's "qpdf" array, copied verbatim.
	// qpdf validates it against the file being updated, so inventing one
	// would be inventing a claim about the document.
	header json.RawMessage
	// nodes holds the page dictionaries and the /Pages nodes, by object id.
	// Nothing else is kept: a document's structure is mostly fonts and
	// content streams, and a crop has no business holding them.
	nodes map[string]map[string]json.RawMessage
}

// readGraph asks qpdf for the document's structure and keeps the page tree.
//
// --json-stream-data=none is what makes this affordable: without it qpdf
// writes every stream's bytes into the JSON, which for a document of scanned
// pages is the whole document again, base64-encoded. With it, the JSON is the
// object graph alone.
//
// What is retained is bounded by the page count: the decoder sees every
// object's JSON, and only the page-tree nodes are kept. The read itself is
// capped at maxCropJSONBytes, so a document built to exhaust a parser is
// refused rather than decoded — with the stream data excluded, no honest
// document comes close to that bound.
func (e *Engines) readGraph(ctx context.Context, dir, input string) (objectGraph, error) {
	dump, err := os.CreateTemp(dir, "graph-*.json")
	if err != nil {
		return objectGraph{}, fmt.Errorf("pdf: create a structure file: %w", err)
	}

	name := dump.Name()

	defer func() {
		_ = dump.Close()
		_ = os.Remove(name)
	}()

	// qpdf writes the JSON to its output argument rather than to stdout.
	if err := e.qpdf(ctx, dir, []string{
		"--json-output=2", "--json-stream-data=none", input, name,
	}); err != nil {
		return objectGraph{}, err
	}

	if _, err := dump.Seek(0, io.SeekStart); err != nil {
		return objectGraph{}, fmt.Errorf("pdf: rewind the structure file: %w", err)
	}

	return decodeGraph(io.LimitReader(dump, maxCropJSONBytes))
}

// decodeGraph reads qpdf's JSON and keeps the page tree.
func decodeGraph(r io.Reader) (objectGraph, error) {
	var envelope struct {
		QPDF []json.RawMessage `json:"qpdf"`
	}

	if err := json.NewDecoder(r).Decode(&envelope); err != nil {
		return objectGraph{}, &DamagedError{Detail: "could not read the document's structure: " + err.Error()}
	}

	if len(envelope.QPDF) < 2 {
		return objectGraph{}, &DamagedError{Detail: "the document's structure has no objects"}
	}

	graph := objectGraph{header: envelope.QPDF[0], nodes: map[string]map[string]json.RawMessage{}}

	var objects map[string]struct {
		Value map[string]json.RawMessage `json:"value"`
	}

	if err := json.Unmarshal(envelope.QPDF[1], &objects); err != nil {
		return objectGraph{}, &DamagedError{Detail: "could not read the document's objects: " + err.Error()}
	}

	for key, object := range objects {
		kind := string(object.Value["/Type"])
		if kind != `"/Page"` && kind != `"/Pages"` {
			continue
		}

		id, ok := strings.CutPrefix(key, "obj:")
		if !ok {
			continue
		}

		graph.nodes[id] = object.Value
	}

	if len(graph.nodes) == 0 {
		return objectGraph{}, &DamagedError{Detail: "the document has no page tree"}
	}

	return graph, nil
}

// cropped builds the update JSON and reports each changed page's new box.
func (g objectGraph) cropped(
	doc Document,
	pages []int,
	req CropRequest,
) (map[string]any, []Box, error) {
	changed := make(map[string]json.RawMessage, len(pages))
	boxes := make([]Box, 0, len(pages))

	for _, page := range pages {
		id, ok := doc.Object(page)
		if !ok {
			return nil, nil, &DamagedError{Detail: "page " + strconv.Itoa(page) + " has no object"}
		}

		node, ok := g.nodes[id]
		if !ok {
			return nil, nil, &DamagedError{Detail: "page " + strconv.Itoa(page) + " is not in the page tree"}
		}

		current, err := g.boxOf(id, node)
		if err != nil {
			return nil, nil, err
		}

		next, err := cropBox(current, req, page)
		if err != nil {
			return nil, nil, err
		}

		// The whole dictionary is written back because qpdf's update
		// replaces an object's value rather than merging into it: writing
		// only /CropBox would write a page with nothing else on it.
		edited := make(map[string]json.RawMessage, len(node)+1)
		maps.Copy(edited, node)

		encoded, err := json.Marshal([]float64{next.LLX, next.LLY, next.URX, next.URY})
		if err != nil {
			return nil, nil, fmt.Errorf("pdf: encode the crop box: %w", err)
		}

		edited["/CropBox"] = encoded

		// A page whose media box is inherited needs its own once it has a
		// crop box, or a reader resolving one without the other can show the
		// crop against a default sheet size.
		if _, has := edited["/MediaBox"]; !has {
			media, err := json.Marshal([]float64{current.LLX, current.LLY, current.URX, current.URY})
			if err != nil {
				return nil, nil, fmt.Errorf("pdf: encode the media box: %w", err)
			}

			edited["/MediaBox"] = media
		}

		raw, err := json.Marshal(map[string]any{"value": edited})
		if err != nil {
			return nil, nil, fmt.Errorf("pdf: encode page %d: %w", page, err)
		}

		changed["obj:"+id] = raw
		boxes = append(boxes, next)
	}

	return map[string]any{"qpdf": []any{g.header, changed}}, boxes, nil
}

// maxInheritanceDepth bounds walking the /Parent chain.
//
// A page tree is a tree, but a malformed document can have a cycle in it, and
// a cycle in a loop with no bound is a hang rather than an error.
const maxInheritanceDepth = 64

// boxOf returns a page's effective box: its own crop box, or its media box,
// or the nearest ancestor's media box.
//
// The inheritance is the part that is easy to get wrong. /MediaBox is an
// inheritable attribute, so a hundred-page document commonly declares it once
// on the page-tree root and not at all on the pages. A crop that assumed the
// page carried its own would compute a box from zeros and produce a document
// of empty pages.
func (g objectGraph) boxOf(id string, node map[string]json.RawMessage) (Box, error) {
	for depth := range maxInheritanceDepth {
		if raw, ok := node["/CropBox"]; ok && depth == 0 {
			box, err := decodeBox(raw)
			if err == nil && !box.Empty() {
				return box, nil
			}
		}

		if raw, ok := node["/MediaBox"]; ok {
			box, err := decodeBox(raw)
			if err != nil {
				return Box{}, err
			}

			if !box.Empty() {
				return box, nil
			}
		}

		parentID, ok := parentOf(node)
		if !ok || parentID == id {
			break
		}

		next, ok := g.nodes[parentID]
		if !ok {
			break
		}

		id, node = parentID, next
	}

	// US Letter is the specification's own default for a page with no media
	// box anywhere in its chain. Guessing is better than cropping to zero,
	// and a document in this state is already malformed.
	return pageSizes["letter"], nil
}

// parentOf returns the object id of a page-tree node's parent.
//
// It reports absence rather than an error, because every way of not having a
// usable parent means the same thing here: the walk up the tree stops and the
// box falls back. A malformed /Parent is not worth a distinct failure when
// the answer is identical to there being no /Parent at all.
func parentOf(node map[string]json.RawMessage) (string, bool) {
	raw, ok := node["/Parent"]
	if !ok {
		return "", false
	}

	var ref string
	if err := json.Unmarshal(raw, &ref); err != nil {
		return "", false
	}

	if !strings.HasSuffix(ref, " R") {
		return "", false
	}

	return ref, true
}

// decodeBox reads a PDF rectangle from JSON.
func decodeBox(raw json.RawMessage) (Box, error) {
	var numbers []float64
	if err := json.Unmarshal(raw, &numbers); err != nil {
		return Box{}, &DamagedError{Detail: "a page box is not four numbers"}
	}

	if len(numbers) != 4 {
		return Box{}, &DamagedError{Detail: "a page box has " + strconv.Itoa(len(numbers)) + " numbers, not four"}
	}

	// A rectangle's corners may be given in either order, which the
	// specification allows and several producers do.
	return Box{
		LLX: min(numbers[0], numbers[2]),
		LLY: min(numbers[1], numbers[3]),
		URX: max(numbers[0], numbers[2]),
		URY: max(numbers[1], numbers[3]),
	}, nil
}

// minCropSize is the smallest crop box this package will write, in points.
// Ten points is about three millimetres: below that the result is not a
// cropped page, it is a mistake.
const minCropSize = 10

// cropBox computes a page's new box and refuses one that is not inside the
// page or has no usable area.
func cropBox(current Box, req CropRequest, page int) (Box, error) {
	next := current

	if !req.Absolute.Empty() {
		next = req.Absolute
	} else {
		next = Box{
			LLX: current.LLX + req.Margins.Left,
			LLY: current.LLY + req.Margins.Bottom,
			URX: current.URX - req.Margins.Right,
			URY: current.URY - req.Margins.Top,
		}
	}

	if next.Width() < minCropSize || next.Height() < minCropSize {
		return Box{}, &OptionError{
			Field: "margins",
			Reason: "Those margins leave nothing of page " + strconv.Itoa(page) +
				": it is " + num(current.Width()) + " by " + num(current.Height()) + " points.",
		}
	}

	// A crop box outside the media box is undefined behaviour in the
	// specification and renders differently in every reader, so it is
	// clamped rather than written.
	return Box{
		LLX: max(next.LLX, current.LLX),
		LLY: max(next.LLY, current.LLY),
		URX: min(next.URX, current.URX),
		URY: min(next.URY, current.URY),
	}, nil
}

// writeJSONFile writes an update document for qpdf to apply.
func writeJSONFile(dir string, body map[string]any) (string, error) {
	file, err := os.CreateTemp(dir, "update-*.json")
	if err != nil {
		return "", fmt.Errorf("pdf: create an update file: %w", err)
	}

	name := file.Name()

	if err := json.NewEncoder(file).Encode(body); err != nil {
		_ = file.Close()
		_ = os.Remove(name)

		return "", fmt.Errorf("pdf: write an update file: %w", err)
	}

	if err := file.Close(); err != nil {
		_ = os.Remove(name)

		return "", fmt.Errorf("pdf: close an update file: %w", err)
	}

	return name, nil
}
