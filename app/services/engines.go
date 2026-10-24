package services

import (
	"context"
	"fmt"
	"time"

	"github.com/AlexGalhardo/vukem"

	"github.com/AlexGalhardo/vukem-tools/internal/cpu"
	"github.com/AlexGalhardo/vukem-tools/internal/engine"
)

// engines is every external program the site can use, with the setting that
// overrides its path and the arguments that make it print its version.
//
// Required marks an engine the deploy cannot run without. It is true only for
// an engine some live tool needs: an engine whose tools are all still planned
// is logged as missing and its tools stay unavailable, which is the difference
// between "this feature is not ready" and "the image is broken". A tool going
// live flips its engine here, so a release that ships the tool but not the
// engine fails the deploy rather than the first conversion.
//
// Versions are pinned in the Dockerfile. These are only the paths.
var engines = []engine.Binary{
	// ffmpeg: video and audio (S3, S4). GPL-3.0 build.
	{Name: "ffmpeg", EnvVar: "FFMPEG_PATH", VersionArgs: []string{"-version"}},
	{Name: "ffprobe", EnvVar: "FFPROBE_PATH", VersionArgs: []string{"-version"}},
	// yt-dlp: the downloader (S3). Unlicense.
	{Name: "yt-dlp", EnvVar: "YTDLP_PATH", VersionArgs: []string{"--version"}},
	// libvips: the primary raster image engine (S2). LGPL-2.1-or-later.
	{Name: "vips", EnvVar: "VIPS_PATH", VersionArgs: []string{"--version"}},
	{Name: "vipsheader", EnvVar: "VIPSHEADER_PATH", VersionArgs: []string{"--vips-version"}},
	// Ghostscript: PDF compression and rasterising (S6). AGPL-3.0-or-later,
	// which the /licenses page states.
	{Name: "gs", EnvVar: "GHOSTSCRIPT_PATH", VersionArgs: []string{"--version"}},
	// poppler: PDF text and page extraction (S6). GPL-2.0-or-later.
	{Name: "pdftoppm", EnvVar: "PDFTOPPM_PATH", VersionArgs: []string{"-v"}},
	{Name: "pdftotext", EnvVar: "PDFTOTEXT_PATH", VersionArgs: []string{"-v"}},
	// qpdf: PDF structure — split, merge, rotate, encrypt (S6). Apache-2.0.
	{Name: "qpdf", EnvVar: "QPDF_PATH", VersionArgs: []string{"--version"}},
	// LibreOffice: Word, Excel and PowerPoint to PDF (S6). MPL-2.0 with
	// LGPL and other parts, declared on /licenses.
	{Name: "soffice", EnvVar: "SOFFICE_PATH", VersionArgs: []string{"--version"}},
	// OCRmyPDF, with Tesseract underneath: scans to searchable PDF (S6).
	// MPL-2.0 and Apache-2.0.
	{Name: "ocrmypdf", EnvVar: "OCRMYPDF_PATH", VersionArgs: []string{"--version"}},
}

// Engine limits. They are deliberately generous enough for a 4K image and
// mean enough that one bad file cannot take the container with it.
const (
	// engineAddressSpace caps mapped memory. A decompression bomb asks for
	// its memory before doing any work, so the allocation fails and the
	// program exits instead of the container being OOM-killed. 3 GiB fits a
	// 4K raster pipeline with room for libvips's cache.
	engineAddressSpace = 3 << 30
	// engineFileSize caps any single output. Well above the 500 MB upload
	// limit, so a legitimate conversion that grows is fine while a runaway
	// write still stops.
	engineFileSize = 2 << 30
	// engineProcesses: ffmpeg and yt-dlp both fork, and yt-dlp calls ffmpeg.
	// This is headroom above the tasks the real UID already owns, which the
	// sandbox adds in, because RLIMIT_NPROC counts per UID and an absolute
	// figure would budget for whatever else shares it. Measured peaks in the
	// image at -threads 2: x264 12, SVT-AV1 lp=1 25, lp=2 56, lp=4 79. 512
	// leaves room for a filter graph without letting an engine fork
	// unbounded.
	engineProcesses = 512
	engineOpenFiles = 512
	// engineTimeout is the default wall clock for one program. A tool with a
	// different shape (a long video, a short thumbnail) passes its own.
	engineTimeout = 15 * time.Minute
	// engineCPU is below the timeout on purpose: a program spinning in a loop
	// is caught by processor time sooner, and with less wasted work, than by
	// the clock. The margin covers a conversion that genuinely waits on I/O.
	engineCPU = 10 * time.Minute
)

// resolveEngines finds every engine, logs each one's version, and fails when a
// required one is absent.
func (c *Conversions) resolveEngines(app *vukem.App) error {
	ctx, cancel := context.WithTimeout(context.Background(), engineProbeTimeout)
	defer cancel()

	registry, err := engine.Resolve(ctx, app.Source, engines)
	if err != nil {
		return fmt.Errorf("services: engines: %w", err)
	}

	registry.Log(ctx, app.Log)

	c.engines = registry

	c.runner, err = engine.NewRunner(engine.RunnerOptions{
		Limits: engine.Limits{
			Timeout:      engineTimeout,
			AddressSpace: engineAddressSpace,
			CPU:          engineCPU,
			FileSize:     engineFileSize,
			Processes:    engineProcesses,
			OpenFiles:    engineOpenFiles,
		},
		Production: app.Config.Env == "production",
		Log:        app.Log,
	})
	if err != nil {
		return fmt.Errorf("services: engine runner: %w", err)
	}

	return nil
}

// engineProbeTimeout bounds the whole boot-time version probe. An engine that
// does not answer `--version` promptly is a broken engine, and a deploy should
// say so rather than hang.
const engineProbeTimeout = 30 * time.Second

// Engines returns the resolved engines. Resolve must have succeeded.
//
// A tool asks this whether its engine is present rather than assuming it, so a
// missing optional engine disables one tool instead of failing a conversion
// with "no such file or directory".
func (c *Conversions) Engines() *engine.Registry { return c.engines }

// Runner runs an engine under its limits. Resolve must have succeeded.
func (c *Conversions) Runner() *engine.Runner { return c.runner }

// EngineConcurrency is how many engine subprocesses may run at once, sized
// from the container's CPU quota rather than the host's core count: a 1-vCPU
// container on a 64-core host would otherwise start 64 conversions and finish
// none of them.
func EngineConcurrency() int { return cpu.Limit() }
