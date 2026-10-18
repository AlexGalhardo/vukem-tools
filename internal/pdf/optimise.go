package pdf

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// ghostscriptTimeout bounds one Ghostscript run. Ghostscript re-renders every
// page, which is the slowest thing this package does, so the bound is the
// loosest here.
const ghostscriptTimeout = 20 * time.Minute

// CompressLevel is how hard a compression tries.
type CompressLevel string

// The compression levels, from gentlest to hardest.
const (
	// CompressLight keeps every page's appearance: images are downsampled
	// only above 300 DPI, which is above what any screen or office printer
	// resolves.
	CompressLight CompressLevel = "light"
	// CompressBalanced is the default: 150 DPI images, which is sharp on
	// screen and acceptable in print.
	CompressBalanced CompressLevel = "balanced"
	// CompressStrong is for sending by email: 96 DPI images, visibly softer
	// when printed.
	CompressStrong CompressLevel = "strong"
)

// compressLevels is the closed set, gentlest first.
var compressLevels = []CompressLevel{CompressLight, CompressBalanced, CompressStrong}

// Valid reports whether l is a level this package knows.
func (l CompressLevel) Valid() bool { return slices.Contains(compressLevels, l) }

// CompressLevels returns the levels a client may name.
func CompressLevels() []string {
	out := make([]string, 0, len(compressLevels))
	for _, l := range compressLevels {
		out = append(out, string(l))
	}

	return out
}

// imageDPI is the resolution each level downsamples images to.
//
// Ghostscript's named presets (/screen, /ebook, /printer) are deliberately
// not used. Each one sets a dozen parameters at once, including some that
// change the fonts, and "we asked for /ebook" is not an explanation anybody
// can act on when a document comes back wrong. Setting the resolution
// directly means each level is one decision, written here.
func (l CompressLevel) imageDPI() int {
	switch l {
	case CompressLight:
		return 300
	case CompressStrong:
		return 96
	case CompressBalanced:
		return 150
	default:
		return 150
	}
}

// CompressRequest is one compression.
type CompressRequest struct {
	Dir    string
	Input  string
	Output string
	Level  CompressLevel
	// Pages is the input's page count, used to turn Ghostscript's page
	// reports into a percentage.
	Pages int
	// Progress, when set, is called as Ghostscript finishes each page.
	Progress func(page, total int)
}

// CompressResult is what a compression achieved.
type CompressResult struct {
	// Before and After are the two sizes in bytes.
	Before, After int64
	// Returned reports whether the output is the compressed document. When
	// it is false the input was already smaller and was copied through
	// unchanged, which is the honest answer rather than a larger file.
	Returned bool
}

// Saved is how many bytes the compression removed, zero when it did not help.
func (r CompressResult) Saved() int64 {
	if !r.Returned {
		return 0
	}

	return r.Before - r.After
}

// Compress makes a document smaller with Ghostscript.
//
// The one rule that shapes this function is that compression must never
// return a larger file. Ghostscript re-renders a document, and re-rendering
// an already-optimised one routinely produces something bigger — a PDF whose
// images are already JPEG at 100 DPI gets them decoded and re-encoded for no
// gain, and the new file carries a fresh font subset as well. So the result
// is measured, and a document that did not shrink is returned exactly as it
// arrived, with CompressResult saying so. ROADMAP S6.4 asks for precisely
// this, and the reason is that a user who clicked "compress" and received a
// larger file has been given a worse document and a reason not to trust the
// tool.
//
// Progress comes from Ghostscript's own page reports on standard output,
// which it writes as "Page 1", "Page 2" and so on. That is a structured
// stream rather than scraped stderr, which is what the project's engine rules
// require — and it is honest progress, because the page Ghostscript says it
// has reached is the page it has reached.
func (e *Engines) Compress(ctx context.Context, req CompressRequest) (CompressResult, error) {
	if req.Level == "" {
		req.Level = CompressBalanced
	}

	if !req.Level.Valid() {
		return CompressResult{}, &OptionError{
			Field:  "level",
			Reason: "The compression level is one of: " + strings.Join(CompressLevels(), ", ") + ".",
		}
	}

	before, err := fileSize(req.Input)
	if err != nil {
		return CompressResult{}, err
	}

	candidate := req.Output + ".candidate"
	dpi := strconv.Itoa(req.Level.imageDPI())

	args := append(ghostscriptBase(),
		"-dCompatibilityLevel=1.7",
		// Downsample above the target resolution and leave anything below it
		// alone: upsampling a low-resolution scan would make the file larger
		// for no gain, which is the failure this whole function guards.
		"-dDownsampleColorImages=true", "-dColorImageResolution="+dpi,
		"-dDownsampleGrayImages=true", "-dGrayImageResolution="+dpi,
		"-dDownsampleMonoImages=true", "-dMonoImageResolution="+strconv.Itoa(req.Level.imageDPI()*2),
		"-dColorImageDownsampleThreshold=1.0",
		"-dGrayImageDownsampleThreshold=1.0",
		"-dMonoImageDownsampleThreshold=1.0",
		// Subset the fonts rather than drop them: a document whose fonts were
		// removed renders in substitutes, which is a different document.
		"-dSubsetFonts=true", "-dEmbedAllFonts=true",
		"-dDetectDuplicateImages=true",
		"-sOutputFile="+candidate,
		req.Input,
	)

	if err := e.ghostscript(ctx, req.Dir, args, pageProgress(req.Pages, req.Progress)); err != nil {
		removeAll([]string{candidate})

		return CompressResult{}, err
	}

	after, err := fileSize(candidate)
	if err != nil {
		removeAll([]string{candidate})

		return CompressResult{}, err
	}

	if after >= before {
		removeAll([]string{candidate})

		if err := copyFile(req.Input, req.Output); err != nil {
			return CompressResult{}, err
		}

		return CompressResult{Before: before, After: before, Returned: false}, nil
	}

	if err := os.Rename(candidate, req.Output); err != nil {
		removeAll([]string{candidate})

		return CompressResult{}, fmt.Errorf("pdf: keep the compressed document: %w", err)
	}

	return CompressResult{Before: before, After: after, Returned: true}, nil
}

// ArchivePart is which PDF/A conformance level to produce.
type ArchivePart int

// The PDF/A parts Ghostscript can target. Each one is a different archival
// promise, and 2b is the usual answer: 1b forbids transparency, which most
// modern documents use, and 3b allows attachments, which most archives do not
// want.
const (
	PDFA1b ArchivePart = 1
	PDFA2b ArchivePart = 2
	PDFA3b ArchivePart = 3
)

// pdfaParts is the closed set.
var pdfaParts = []ArchivePart{PDFA1b, PDFA2b, PDFA3b}

// Valid reports whether p is a part this package targets.
func (p ArchivePart) Valid() bool { return slices.Contains(pdfaParts, p) }

// ArchiveParts returns the conformance levels a client may name.
func ArchiveParts() []int { return []int{int(PDFA1b), int(PDFA2b), int(PDFA3b)} }

// ArchiveRequest is one PDF/A conversion.
type ArchiveRequest struct {
	Dir      string
	Input    string
	Output   string
	Part     ArchivePart
	Pages    int
	Progress func(page, total int)
}

// ArchiveResult is the conformance report a conversion produced.
type ArchiveResult struct {
	Part ArchivePart
	// Conformant reports whether Ghostscript produced a document it
	// considers conformant. It is false when the converter had to break
	// conformance to keep the content, which the policy below makes a
	// reported failure rather than a silent one.
	Conformant bool
	// Complaints are the conformance problems Ghostscript reported, for the
	// report shown to the user. They are the engine's own words about the
	// document's content, not about the server, so they are safe to show.
	Complaints []string
}

// maxComplaints caps the report. A document with thousands of conformance
// problems needs the first few and a count, not all of them.
const maxComplaints = 50

// pdfaComplaint matches the conformance problems Ghostscript reports.
//
// Its PDF/A messages all carry this prefix, and reading them is how the tool
// produces a report at all: Ghostscript has no structured conformance output,
// and ROADMAP S6.3 accepts its own report as the second opinion in place of
// veraPDF, which is not in the image. This is the engine's verdict on the
// document, not progress, so reading it does not conflict with the rule that
// progress must come from a structured stream.
var pdfaComplaint = regexp.MustCompile(
	`(?i)^\s*(?:\*\*\*\s*)?(?:.*not permitted in PDF/A.*|.*PDF/A.*(?:cannot|unable|falling back|removed|not).*)$`,
)

// ToPDFA converts a document to PDF/A.
//
// PDFACompatibilityPolicy=1 is the decision worth explaining. Policy 0 drops
// content Ghostscript cannot make conformant; policy 1 keeps the content and
// abandons conformance, saying so; policy 2 fails. Policy 1 is the only one
// that cannot lose a page silently: a user converting a contract to PDF/A
// would rather be told "this is not conformant because of the transparency on
// page 3" than receive a conformant document with page 3's logo missing.
func (e *Engines) ToPDFA(ctx context.Context, req ArchiveRequest) (ArchiveResult, error) {
	if req.Part == 0 {
		req.Part = PDFA2b
	}

	if !req.Part.Valid() {
		return ArchiveResult{}, &OptionError{Field: "part", Reason: "PDF/A conformance is 1, 2 or 3."}
	}

	complaints := make([]string, 0, 8)

	args := append(ghostscriptBase(),
		"-dPDFA="+strconv.Itoa(int(req.Part)),
		"-dPDFACompatibilityPolicy=1",
		"-dNOOUTERSAVE",
		// Device-independent colour, so a page's colours survive the
		// conversion instead of being flattened into the output device's
		// space.
		"-sColorConversionStrategy=UseDeviceIndependentColor",
		"-dCompatibilityLevel=1.7",
		"-sOutputFile="+req.Output,
		req.Input,
	)

	progress := pageProgress(req.Pages, req.Progress)

	err := e.ghostscript(ctx, req.Dir, args, func(line string) {
		progress(line)

		if len(complaints) < maxComplaints && pdfaComplaint.MatchString(line) {
			if trimmed := strings.TrimSpace(strings.TrimLeft(line, "* ")); trimmed != "" {
				complaints = append(complaints, trimmed)
			}
		}
	})
	if err != nil {
		return ArchiveResult{}, err
	}

	return ArchiveResult{Part: req.Part, Conformant: len(complaints) == 0, Complaints: complaints}, nil
}

// RepairRequest is one structural rebuild.
type RepairRequest struct {
	Dir    string
	Input  string
	Output string
}

// RepairResult is what a repair managed.
type RepairResult struct {
	// Pages is the rebuilt document's page count.
	Pages int
	// Rebuilt reports whether qpdf had to reconstruct the cross-reference
	// table, which is the difference between a document that was broken and
	// one that was merely unoptimised.
	Rebuilt bool
}

// Repair rebuilds a document's structure with qpdf.
//
// It is honest about failing, which ROADMAP S6.4 asks for explicitly: a file
// qpdf cannot rebuild comes back as a DamagedError rather than as a document
// that opens empty. That distinction is the whole value of the tool —
// anybody can produce a file; producing a file that is a readable version of
// the input is the part that is hard, and the only way to be sure is to read
// the result back and count its pages.
//
// The header is checked first and the input is never run through
// Ghostscript. A damaged PDF is the one input most likely to be hostile
// rather than merely broken, and qpdf's parser is both the smaller attack
// surface and the one designed to be pointed at a broken file.
func (e *Engines) Repair(ctx context.Context, req RepairRequest) (RepairResult, error) {
	if err := ValidateHeader(req.Input); err != nil {
		return RepairResult{}, err
	}

	encrypted, err := e.IsEncrypted(ctx, req.Dir, req.Input)
	if err == nil && encrypted {
		return RepairResult{}, ErrEncrypted
	}

	// A document too damaged for --is-encrypted to answer is exactly what
	// this tool is for, so that failure is not fatal here.

	result, runErr := e.run(ctx, command{
		engine: EngineQPDF,
		// Not --replace-input: that rewrites the uploaded file in place, and
		// a repair that destroyed its own input would leave nothing to retry
		// with. qpdf rebuilds whatever it has to in order to write the
		// output, which is the same rebuild.
		args:    []string{req.Input, req.Output},
		dir:     req.Dir,
		timeout: structureTimeout,
	})

	rebuilt := strings.Contains(result.Stderr, "reconstruct")

	if runErr != nil {
		engineErr, ok := errors.AsType[*EngineError](runErr)
		if !ok {
			return RepairResult{}, runErr
		}

		if !usableExit(engineErr.Code) {
			removeAll([]string{req.Output})

			return RepairResult{}, &DamagedError{Detail: engineErr.Error()}
		}

		rebuilt = true
	}

	// Read the result back. A repair that produced a file qpdf cannot now
	// read, or one with no pages, has not repaired anything.
	doc, err := e.Inspect(ctx, req.Dir, req.Output)
	if err != nil {
		removeAll([]string{req.Output})

		return RepairResult{}, &DamagedError{Detail: "the rebuilt document is still unreadable: " + err.Error()}
	}

	return RepairResult{Pages: doc.Count, Rebuilt: rebuilt}, nil
}

// ghostscriptBase is the flags every Ghostscript run carries.
//
// -dSAFER is the important one and it is never omitted. It confines
// Ghostscript to the files named on its command line and disables the
// PostScript operators that open pipes and arbitrary paths — which is what
// stops a crafted document reading /etc/passwd or reaching the network
// through `%pipe%`. Ghostscript 10 enables it by default; it is written out
// anyway, because a default is a thing that can change and this one is the
// difference between a sandbox and none.
func ghostscriptBase() []string {
	return []string{
		"-dSAFER",
		"-dBATCH",
		"-dNOPAUSE",
		// No -q: Ghostscript's page reports on standard output are where
		// progress comes from, and -q silences them.
		"-sDEVICE=pdfwrite",
	}
}

// gsPageLine matches Ghostscript's page reports on standard output.
var gsPageLine = regexp.MustCompile(`^Page (\d+)$`)

// pageProgress turns Ghostscript's page reports into progress calls.
func pageProgress(total int, report func(page, total int)) func(line string) {
	return func(line string) {
		if report == nil {
			return
		}

		match := gsPageLine.FindStringSubmatch(strings.TrimSpace(line))
		if match == nil {
			return
		}

		page, err := strconv.Atoi(match[1])
		if err != nil {
			return
		}

		report(page, total)
	}
}

// ghostscript runs one Ghostscript invocation, reading its standard output
// line by line.
func (e *Engines) ghostscript(ctx context.Context, dir string, args []string, onLine func(string)) error {
	if _, err := e.run(ctx, command{
		engine:   EngineGhostscript,
		args:     args,
		dir:      dir,
		progress: onLine,
		timeout:  ghostscriptTimeout,
	}); err != nil {
		if engineErr, ok := errors.AsType[*EngineError](err); ok {
			// Ghostscript refuses a document it cannot parse, which is the
			// input's problem rather than the engine's, and retrying would
			// reach the same answer.
			return &DamagedError{Detail: engineErr.Error()}
		}

		return err
	}

	return nil
}

// fileSize is one file's size in bytes.
func fileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, fmt.Errorf("pdf: measure %s: %w", path, err)
	}

	return info.Size(), nil
}

// copyFile copies src to dst, streaming.
//
// It is used to return an input unchanged when compression did not help. The
// alternative — telling the caller to use the input as the output — would mean
// every caller knowing that the result might be the input, and one that
// forgot would store a file it had already deleted.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("pdf: open %s: %w", src, err)
	}

	defer func() { _ = in.Close() }()

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("pdf: create %s: %w", dst, err)
	}

	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()

		return err
	}

	if err := out.Close(); err != nil {
		return fmt.Errorf("pdf: close %s: %w", dst, err)
	}

	return nil
}
