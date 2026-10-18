package pdf

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AlexGalhardo/vukem-tools/internal/cpu"
)

// The DPI bounds the API documents and enforces.
//
// The ceiling is not arbitrary: an A4 page at 600 DPI is 4960 by 7016 pixels,
// which is 100 MB of RGB before compression. The engine's address-space limit
// would stop anything much past that, and being stopped by a limit produces a
// worse message than being told the number is too high.
const (
	MinDPI     = 36
	MaxDPI     = 600
	DefaultDPI = 150
)

// rasterPageTimeout bounds rendering one page. A page is rendered on its own,
// so this is a per-page bound rather than a per-document one, which is what
// keeps one pathological page from spending a 500-page document's budget.
const rasterPageTimeout = 2 * time.Minute

// RasterFormat is the image a page is rendered to.
type RasterFormat string

// The raster formats. PNG is the default because a page of text with flat
// colour is both smaller and sharper as PNG than as JPEG.
const (
	RasterPNG  RasterFormat = "png"
	RasterJPEG RasterFormat = "jpeg"
)

// rasterFormats is the closed set.
var rasterFormats = []RasterFormat{RasterPNG, RasterJPEG}

// Valid reports whether f is a format this package renders.
func (f RasterFormat) Valid() bool { return slices.Contains(rasterFormats, f) }

// Extension is the file extension pdftoppm gives this format.
func (f RasterFormat) Extension() string {
	if f == RasterJPEG {
		return ".jpg"
	}

	return ".png"
}

// RasterFormats returns the formats a client may name.
func RasterFormats() []string { return []string{string(RasterPNG), string(RasterJPEG)} }

// RasterRequest is one rasterisation.
type RasterRequest struct {
	Dir   string
	Input string
	// Pages are the 1-based pages to render, in order. Empty renders all.
	Pages []int
	// SourcePages is the input's page count, from Inspect.
	SourcePages int
	DPI         int
	Format      RasterFormat
	// JPEGQuality is 1 to 100, used only by the JPEG format.
	JPEGQuality int
	// Prefix names the output files inside Dir.
	Prefix string
	// Progress, when set, is called as each page finishes. It is called from
	// several goroutines, so it must be safe to call concurrently.
	Progress func(done, total int)
}

// RasterPage is one rendered page.
type RasterPage struct {
	Path string
	// Page is the 1-based page it came from.
	Page int
	// Width and Height are the image's pixel dimensions, read from its
	// header.
	Width, Height int
}

// defaultJPEGQuality is a quality that is visually clean for a page of text
// without being an uncompressed file in all but name.
const defaultJPEGQuality = 85

// Rasterise renders pages to images.
//
// Each page is a separate pdftoppm invocation, which is three decisions at
// once. It gives honest progress — a page that finished is a page that
// finished, counted by this code rather than scraped out of an engine's
// stderr, which ROADMAP's engine rules forbid. It bounds the blast radius: a
// single malformed page fails its own invocation, under its own resource
// limits, instead of taking the other 499 with it. And it parallelises, which
// is the only way a 500-page document finishes in a reasonable time on a
// container with more than one CPU.
//
// The parallelism is sized from the container's CPU quota, never from the
// host's core count: a one-vCPU container on a 64-core host would otherwise
// start 64 renders and finish none of them.
func (e *Engines) Rasterise(ctx context.Context, req RasterRequest) ([]RasterPage, error) {
	pages, err := req.resolve()
	if err != nil {
		return nil, err
	}

	out := make([]RasterPage, len(pages))
	group := newBoundedGroup(ctx, cpu.Limit())

	var done atomic.Int64

	for i, page := range pages {
		group.run(func(ctx context.Context) error {
			rendered, err := e.rasterisePage(ctx, req, page)
			if err != nil {
				return err
			}

			out[i] = rendered

			if req.Progress != nil {
				req.Progress(int(done.Add(1)), len(pages))
			}

			return nil
		})
	}

	if err := group.wait(); err != nil {
		paths := make([]string, 0, len(out))
		for _, page := range out {
			if page.Path != "" {
				paths = append(paths, page.Path)
			}
		}

		removeAll(paths)

		return nil, err
	}

	return out, nil
}

// resolve fills in the defaults and refuses an option this package cannot
// act on.
func (r *RasterRequest) resolve() ([]int, error) {
	if r.DPI == 0 {
		r.DPI = DefaultDPI
	}

	if r.DPI < MinDPI || r.DPI > MaxDPI {
		return nil, &OptionError{
			Field: "dpi",
			Reason: "The resolution must be between " + strconv.Itoa(MinDPI) + " and " +
				strconv.Itoa(MaxDPI) + " DPI.",
		}
	}

	if r.Format == "" {
		r.Format = RasterPNG
	}

	if !r.Format.Valid() {
		return nil, &OptionError{Field: "format", Reason: "Render to PNG or JPEG."}
	}

	if r.JPEGQuality == 0 {
		r.JPEGQuality = defaultJPEGQuality
	}

	if r.JPEGQuality < 1 || r.JPEGQuality > 100 {
		return nil, &OptionError{Field: "quality", Reason: "The JPEG quality must be between 1 and 100."}
	}

	pages := r.Pages
	if len(pages) == 0 {
		pages = sequence(1, r.SourcePages)
	}

	if len(pages) == 0 {
		return nil, &OptionError{Field: "pages", Reason: "There are no pages to render."}
	}

	if highest(pages) > r.SourcePages {
		return nil, &PageRangeError{
			Input: QPDFRange(pages),
			Reason: "That page range names page " + strconv.Itoa(highest(pages)) +
				", but this document has " + strconv.Itoa(r.SourcePages) + ".",
		}
	}

	return pages, nil
}

// rasterisePage renders one page and reads the result's dimensions.
func (e *Engines) rasterisePage(ctx context.Context, req RasterRequest, page int) (RasterPage, error) {
	stem := req.Prefix + "-" + strconv.Itoa(page)

	args := []string{
		"-r", strconv.Itoa(req.DPI),
		"-f", strconv.Itoa(page),
		"-l", strconv.Itoa(page),
		// The crop box, not the media box. pdftoppm defaults to the media
		// box, which is the sheet rather than what a reader shows: a cropped
		// document would rasterise with its margins back, so a page exported
		// as an image would not match the page on screen. For a document
		// with no crop box the two are the same, so this changes nothing
		// except in the case where it matters.
		"-cropbox",
		// One page per invocation, so pdftoppm writes exactly
		// "<stem><extension>" instead of appending its own page number.
		"-singlefile",
	}

	if req.Format == RasterJPEG {
		args = append(args, "-jpeg", "-jpegopt", "quality="+strconv.Itoa(req.JPEGQuality))
	} else {
		args = append(args, "-png")
	}

	args = append(args, req.Input, filepath.Join(req.Dir, stem))

	if _, err := e.run(ctx, command{
		engine:  EnginePDFToPPM,
		args:    args,
		dir:     req.Dir,
		timeout: rasterPageTimeout,
	}); err != nil {
		return RasterPage{}, renderFailure(err, page)
	}

	path := filepath.Join(req.Dir, stem+req.Format.Extension())

	info, err := imageInfoOf(path, req.Format)
	if err != nil {
		return RasterPage{}, err
	}

	return RasterPage{Path: path, Page: page, Width: info.Width, Height: info.Height}, nil
}

// imageInfoOf reads a rendered page's dimensions from its header.
func imageInfoOf(path string, format RasterFormat) (ImageInfo, error) {
	if format == RasterJPEG {
		info, err := JPEGInfo(path)
		if err != nil {
			return ImageInfo{}, &DamagedError{Detail: "the rendered page is not a readable JPEG"}
		}

		return info, nil
	}

	info, err := PNGSize(path)
	if err != nil {
		return ImageInfo{}, &DamagedError{Detail: "the rendered page is not a readable PNG"}
	}

	return info, nil
}

// renderFailure says which page failed, which is the one thing the user can
// act on: "page 83 could not be rendered" tells them where to look.
func renderFailure(err error, page int) error {
	if engineErr, ok := errors.AsType[*EngineError](err); ok {
		return &DamagedError{Detail: "page " + strconv.Itoa(page) + ": " + engineErr.Error()}
	}

	return err
}

// boundedGroup runs functions with a fixed number of workers and keeps the
// first error.
//
// errgroup would do this, and golang.org/x/sync is already an indirect
// dependency — but making it a direct one to save thirty lines is a
// dependency the project would then have to keep, and the project's rule is
// to prefer the standard library. The one behaviour worth copying is
// cancellation: the first failure cancels the rest, so a document with a
// broken page on page 2 does not render the remaining 498.
type boundedGroup struct {
	ctx    context.Context
	cancel context.CancelFunc
	slots  chan struct{}
	wg     sync.WaitGroup
	once   sync.Once
	err    error
}

// newBoundedGroup returns a group that runs at most limit functions at once.
func newBoundedGroup(ctx context.Context, limit int) *boundedGroup {
	ctx, cancel := context.WithCancel(ctx)

	return &boundedGroup{ctx: ctx, cancel: cancel, slots: make(chan struct{}, max(limit, 1))}
}

// run queues fn.
func (g *boundedGroup) run(fn func(context.Context) error) {
	g.wg.Go(func() {
		select {
		case g.slots <- struct{}{}:
			defer func() { <-g.slots }()
		case <-g.ctx.Done():
			g.fail(g.ctx.Err())

			return
		}

		if err := fn(g.ctx); err != nil {
			g.fail(err)
		}
	})
}

// fail records the first error and cancels the rest.
func (g *boundedGroup) fail(err error) {
	g.once.Do(func() {
		g.err = err
		g.cancel()
	})
}

// wait blocks until every queued function has finished and returns the first
// error.
func (g *boundedGroup) wait() error {
	g.wg.Wait()
	g.cancel()

	return g.err
}
