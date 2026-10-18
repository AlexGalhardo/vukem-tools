// Package ingest accepts a finished upload.
//
// Three things happen, in this order, and the order is the point:
//
//  1. The file is identified from its bytes. Nothing downstream reads a type
//     the client supplied.
//  2. It is scanned for malware. A file the scanner has not cleared never
//     reaches the bucket, so an infected file cannot be downloaded by its
//     uploader through a signed link while someone decides what to do with
//     it.
//  3. Only then are the bytes stored and the row written.
//
// The scan comes before the store rather than after, which costs one extra
// pass over a local file and buys the guarantee that the bucket holds nothing
// unscanned.
package ingest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/AlexGalhardo/vukem/upload"

	"github.com/AlexGalhardo/vukem-tools/app/models"
	"github.com/AlexGalhardo/vukem-tools/internal/detect"
	"github.com/AlexGalhardo/vukem-tools/internal/files"
	"github.com/AlexGalhardo/vukem-tools/internal/jobs"
)

// Opener returns a fresh reader over the uploaded bytes. It is called more
// than once — identification, scanning and storing are separate passes — so
// it must open the data again each time rather than hand back one reader.
type Opener func() (io.ReadCloser, error)

// Ingester turns a finished upload into a stored file.
type Ingester struct {
	files    *files.Store
	scanner  upload.Scanner
	accepted []string
	maxBytes int64
	log      *slog.Logger
}

// Options configures an Ingester. Files and MaxBytes are required.
type Options struct {
	Files *files.Store
	// Scanner scans every upload before it is stored. Optional, and its
	// absence is a deliberate, logged choice: see New.
	Scanner upload.Scanner
	// Accepted lists every MIME type the service takes at the door. It is
	// the union of what the tools accept, not a per-tool list — the tool
	// checks its own types again when it runs, because the file and the tool
	// are chosen in separate requests.
	Accepted []string
	// MaxBytes is the largest file accepted.
	MaxBytes int64
	Log      *slog.Logger
}

// New returns an ingester.
//
// A missing scanner is allowed, because `vukem serve` on a laptop has no
// clamd and refusing to start would make the project undevelopable. It is
// logged at warning level every boot so it cannot become the quiet default in
// production; the deployment checklist is what keeps it set there.
func New(o Options) (*Ingester, error) {
	switch {
	case o.Files == nil:
		return nil, errors.New("ingest: needs a file store")
	case o.MaxBytes <= 0:
		return nil, fmt.Errorf("ingest: the size limit must be positive, got %d", o.MaxBytes)
	}

	if o.Log == nil {
		o.Log = slog.Default()
	}

	for _, mime := range o.Accepted {
		if !detect.Known(mime) {
			return nil, fmt.Errorf("ingest: %q is not a type this service recognises", mime)
		}
	}

	if o.Scanner == nil {
		o.Log.Warn("ingest: no malware scanner configured, uploads are stored unscanned")
	}

	return &Ingester{
		files:    o.Files,
		scanner:  o.Scanner,
		accepted: o.Accepted,
		maxBytes: o.MaxBytes,
		log:      o.Log,
	}, nil
}

// Accept identifies, scans and stores a finished upload.
//
// Every refusal comes back as a *jobs.ConversionError carrying a code from
// the taxonomy, so the HTTP layer answers with one mapping and the user gets
// the same wording here as they would from a failed conversion.
func (i *Ingester) Accept(ctx context.Context, userID string, info upload.TusInfo, open Opener) (models.File, error) {
	if info.Length > i.maxBytes {
		return models.File{}, jobs.Refuse(jobs.InputTooLarge, nil,
			fmt.Sprintf("That file is %d MB. The limit is %d MB.", info.Length>>20, i.maxBytes>>20),
			fmt.Sprintf("upload is %d bytes, the limit is %d", info.Length, i.maxBytes))
	}

	kind, err := i.identify(ctx, info, open)
	if err != nil {
		return models.File{}, err
	}

	if err := i.scan(ctx, open); err != nil {
		return models.File{}, err
	}

	body, err := open()
	if err != nil {
		return models.File{}, jobs.Fail(jobs.Internal, err, "reopen the upload to store it")
	}

	defer i.close(ctx, body)

	stored, err := i.files.Put(ctx, files.Incoming{
		UserID: userID,
		Role:   models.FileInput,
		Name:   info.Filename(),
		Kind:   kind,
	}, body)
	if err != nil {
		return models.File{}, jobs.Fail(jobs.Internal, err, "store the upload")
	}

	return stored, nil
}

// identify reads the header and decides what the file is.
func (i *Ingester) identify(ctx context.Context, info upload.TusInfo, open Opener) (detect.Kind, error) {
	r, err := open()
	if err != nil {
		return detect.Kind{}, jobs.Fail(jobs.Internal, err, "open the upload to identify it")
	}

	defer i.close(ctx, r)

	kind, err := detect.Check(io.LimitReader(r, detect.HeaderBytes), info.Filename(), i.accepted)
	if err == nil {
		return kind, nil
	}

	// Both refusals carry a sentence written for the person who uploaded the
	// file; the detail behind them is for the log.
	if mismatch, ok := errors.AsType[*detect.MismatchError](err); ok {
		return detect.Kind{}, jobs.Refuse(jobs.UnsupportedInput, err, mismatch.Message(), err.Error())
	}

	if unsupported, ok := errors.AsType[*detect.UnsupportedError](err); ok {
		return detect.Kind{}, jobs.Refuse(jobs.UnsupportedInput, err, unsupported.Message(), err.Error())
	}

	return detect.Kind{}, jobs.Fail(jobs.Internal, err, "identify the upload")
}

// scan runs the malware scanner over the whole file.
//
// A scanner that cannot be reached is a refusal, not a pass. Failing open here
// would mean that the way to get an infected file through is to make clamd
// unavailable, which is the opposite of what a scanner is for.
func (i *Ingester) scan(ctx context.Context, open Opener) error {
	if i.scanner == nil {
		return nil
	}

	r, err := open()
	if err != nil {
		return jobs.Fail(jobs.Internal, err, "open the upload to scan it")
	}

	defer i.close(ctx, r)

	err = i.scanner.Scan(ctx, r)
	if err == nil {
		return nil
	}

	if errors.Is(err, upload.ErrInfected) {
		// The signature goes to the log, never to the response: naming it
		// tells whoever is probing which scanner and which definitions are in
		// use.
		return jobs.Fail(jobs.Infected, err, "the malware scanner reported a signature")
	}

	return jobs.Fail(jobs.Internal, err, "the malware scanner could not be reached")
}

// close logs a failed close rather than returning it: the operation's own
// result is what the caller needs, and a reader that will not close is a
// symptom, not the answer.
func (i *Ingester) close(ctx context.Context, c io.Closer) {
	if err := c.Close(); err != nil {
		i.log.WarnContext(ctx, "ingest: closing the upload failed", "err", err)
	}
}
