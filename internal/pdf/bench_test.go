package pdf_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/AlexGalhardo/vukem-tools/internal/engine"
	"github.com/AlexGalhardo/vukem-tools/internal/pdf"
)

// benchEngines is testEngines for a benchmark, which has no *testing.T.
//
// A benchmark whose engine is missing is skipped rather than reported as
// zero: a benchmark that did no work and claimed a time would be worse than
// no figure at all.
func benchEngines(b *testing.B, need ...string) *pdf.Engines {
	b.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	registry, err := engine.Resolve(ctx, envLookup{}, testBinaries)
	if err != nil {
		b.Fatalf("resolve the engines: %v", err)
	}

	for _, name := range need {
		if !registry.Has(name) {
			b.Skipf("%s is not installed. These figures are measured inside the production image: "+
				"see docs/deployment.md, \"Verifying the image\".", name)
		}
	}

	runner, err := engine.NewRunner(engine.RunnerOptions{
		Limits: engine.Limits{
			Timeout:      10 * time.Minute,
			AddressSpace: 3 << 30,
			CPU:          8 * time.Minute,
			FileSize:     2 << 30,
			Processes:    64,
			OpenFiles:    512,
		},
		Log: slog.New(slog.DiscardHandler),
	})
	if err != nil {
		b.Fatalf("build the engine runner: %v", err)
	}

	engines, err := pdf.NewEngines(registry, runner)
	if err != nil {
		b.Fatalf("build the engine set: %v", err)
	}

	return engines
}

// benchFixture writes a fixture of the given page count for a benchmark.
func benchFixture(b *testing.B, dir, name string, pages int) string {
	b.Helper()

	a4, _ := pdf.PageSize("a4")

	specs := make([]pdf.PageSpec, 0, pages)

	for i := 1; i <= pages; i++ {
		specs = append(specs, pdf.PageSpec{
			Box: a4,
			Text: []pdf.TextStamp{{
				Text: "Benchmark page " + strconv.Itoa(i),
				Font: pdf.Helvetica,
				Size: 18,
				X:    72,
				Y:    720,
			}},
		})
	}

	path := filepath.Join(dir, name)

	file, err := os.Create(path)
	if err != nil {
		b.Fatalf("create %s: %v", path, err)
	}

	if err := pdf.Write(file, specs); err != nil {
		_ = file.Close()
		b.Fatalf("write %s: %v", path, err)
	}

	if err := file.Close(); err != nil {
		b.Fatalf("close %s: %v", path, err)
	}

	return path
}

// BenchmarkWatermark is the figure ROADMAP S6.6 asks for: a watermark on a
// large document, applied in one pass, with the memory cost reported so that
// "independent of the page count" is a measurement rather than a claim.
//
// Run it at both sizes and divide: what should stay flat is the bytes per
// page, and what is interesting is how small that number is compared with a
// page's worth of content.
func BenchmarkWatermark(b *testing.B) {
	for _, pages := range []int{10, 50, 500} {
		b.Run(strconv.Itoa(pages)+"pages", func(b *testing.B) {
			engines := benchEngines(b, pdf.EngineQPDF)
			dir := b.TempDir()
			source := benchFixture(b, dir, "source.pdf", pages)
			ctx := context.Background()

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; b.Loop(); i++ {
				out := filepath.Join(dir, "out.pdf")

				if _, err := engines.Watermark(ctx, pdf.WatermarkRequest{
					Dir: dir, Input: source, Output: out, Text: "CONFIDENTIAL", Rotate: 45,
				}); err != nil {
					b.Fatalf("Watermark = %v", err)
				}

				if err := os.Remove(out); err != nil {
					b.Fatalf("remove the output: %v", err)
				}
			}

			b.StopTimer()
			b.ReportMetric(float64(pages), "pages")
			b.ReportMetric(float64(peakRSS(b)), "peak-RSS-bytes")
		})
	}
}

// peakRSS is the process's high-water-mark resident memory in bytes, or zero
// where the platform does not report it.
//
// It is read rather than inferred because B/op is bytes *allocated*, not
// bytes live: an operation that allocates a megabyte and frees it reports a
// megabyte, and the figure ROADMAP asks for is the peak. /proc/self/status
// is the only place the kernel keeps the high-water mark, which is also why
// this figure only means anything inside the container.
//
// The mark never falls, so a process that measured a large document first
// would report that figure for a small one. Each size is therefore run in
// its own process; docs/benchmarks.md gives the command.
func peakRSS(b *testing.B) int64 {
	b.Helper()

	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}

	for line := range strings.SplitSeq(string(status), "\n") {
		rest, ok := strings.CutPrefix(line, "VmHWM:")
		if !ok {
			continue
		}

		fields := strings.Fields(rest)
		if len(fields) == 0 {
			return 0
		}

		kilobytes, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			return 0
		}

		return kilobytes * 1024
	}

	return 0
}

// BenchmarkMerge measures joining documents, which is the operation whose
// cost scales with the number of inputs rather than with their size.
func BenchmarkMerge(b *testing.B) {
	engines := benchEngines(b, pdf.EngineQPDF)
	dir := b.TempDir()
	ctx := context.Background()

	inputs := make([]string, 0, 10)
	for i := range 10 {
		inputs = append(inputs, benchFixture(b, dir, "in"+strconv.Itoa(i)+".pdf", 10))
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		out := filepath.Join(dir, "merged.pdf")

		if _, err := engines.Merge(ctx, pdf.MergeRequest{Dir: dir, Inputs: inputs, Output: out}); err != nil {
			b.Fatalf("Merge = %v", err)
		}

		if err := os.Remove(out); err != nil {
			b.Fatalf("remove the output: %v", err)
		}
	}
}

// BenchmarkSplit measures splitting into single pages, which is one engine
// invocation per part and the operation most likely to be the slow one.
func BenchmarkSplit(b *testing.B) {
	engines := benchEngines(b, pdf.EngineQPDF)
	dir := b.TempDir()
	ctx := context.Background()
	source := benchFixture(b, dir, "source.pdf", 20)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		parts, err := engines.Split(ctx, pdf.SplitRequest{
			Dir: dir, Input: source, SourcePages: 20, Groups: pdf.GroupEvery(20, 1), Prefix: "part",
		})
		if err != nil {
			b.Fatalf("Split = %v", err)
		}

		for _, part := range parts {
			if err := os.Remove(part.Path); err != nil {
				b.Fatalf("remove a part: %v", err)
			}
		}
	}
}

// BenchmarkRasterise measures page rendering, which is the CPU-heaviest
// operation and the one whose concurrency is sized from the container's quota.
func BenchmarkRasterise(b *testing.B) {
	engines := benchEngines(b, pdf.EngineQPDF, pdf.EnginePDFToPPM)
	dir := b.TempDir()
	ctx := context.Background()
	source := benchFixture(b, dir, "source.pdf", 10)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		pages, err := engines.Rasterise(ctx, pdf.RasterRequest{
			Dir: dir, Input: source, SourcePages: 10, DPI: 150, Format: pdf.RasterPNG, Prefix: "page",
		})
		if err != nil {
			b.Fatalf("Rasterise = %v", err)
		}

		for _, page := range pages {
			if err := os.Remove(page.Path); err != nil {
				b.Fatalf("remove a page: %v", err)
			}
		}
	}
}

// BenchmarkCompress measures the Ghostscript path, which is the slowest one
// because it re-renders every page.
func BenchmarkCompress(b *testing.B) {
	engines := benchEngines(b, pdf.EngineQPDF, pdf.EngineGhostscript)
	dir := b.TempDir()
	ctx := context.Background()
	source := benchFixture(b, dir, "source.pdf", 20)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		out := filepath.Join(dir, "compressed.pdf")

		if _, err := engines.Compress(ctx, pdf.CompressRequest{
			Dir: dir, Input: source, Output: out, Level: pdf.CompressBalanced, Pages: 20,
		}); err != nil {
			b.Fatalf("Compress = %v", err)
		}

		if err := os.Remove(out); err != nil {
			b.Fatalf("remove the output: %v", err)
		}
	}
}

// BenchmarkToMarkdown measures the one operation with a Go-side pass over the
// whole document's text, which is where a buffering mistake would show.
func BenchmarkToMarkdown(b *testing.B) {
	engines := benchEngines(b, pdf.EngineQPDF, pdf.EnginePDFToText)
	dir := b.TempDir()
	ctx := context.Background()
	source := benchFixture(b, dir, "source.pdf", 100)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		out := filepath.Join(dir, "out.md")

		if _, err := engines.ToMarkdown(ctx, pdf.MarkdownRequest{
			Dir: dir, Input: source, Output: out, SourcePages: 100,
		}); err != nil {
			b.Fatalf("ToMarkdown = %v", err)
		}

		if err := os.Remove(out); err != nil {
			b.Fatalf("remove the output: %v", err)
		}
	}
}

// BenchmarkWriteDocument measures the PDF writer with no engine at all, which
// is what isolates this package's own cost from the engines'.
func BenchmarkWriteDocument(b *testing.B) {
	for _, pages := range []int{10, 500} {
		b.Run(strconv.Itoa(pages)+"pages", func(b *testing.B) {
			a4, _ := pdf.PageSize("a4")

			specs := make([]pdf.PageSpec, 0, pages)
			for i := range pages {
				specs = append(specs, pdf.PageSpec{
					Box:  a4,
					Text: []pdf.TextStamp{{Text: "Page " + strconv.Itoa(i), Font: pdf.Helvetica, Size: 12, X: 1, Y: 1}},
				})
			}

			b.ReportAllocs()
			b.ResetTimer()

			for b.Loop() {
				if err := pdf.Write(io.Discard, specs); err != nil {
					b.Fatalf("Write = %v", err)
				}
			}

			b.ReportMetric(float64(pages), "pages")
		})
	}
}
