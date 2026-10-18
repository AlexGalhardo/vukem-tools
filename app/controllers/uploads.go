package controllers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/AlexGalhardo/vukem/auth"
	"github.com/AlexGalhardo/vukem/errs"
	"github.com/AlexGalhardo/vukem/upload"
	"github.com/AlexGalhardo/vukem/web"

	"github.com/AlexGalhardo/vukem-tools/app/models"
	"github.com/AlexGalhardo/vukem-tools/internal/detect"
	"github.com/AlexGalhardo/vukem-tools/internal/files"
	"github.com/AlexGalhardo/vukem-tools/internal/ingest"
	"github.com/AlexGalhardo/vukem-tools/internal/jobs"
)

// Uploads accepts resumable uploads and serves downloads.
type Uploads struct {
	ingester *ingest.Ingester
	files    *files.Store
	guards   []string
	log      *slog.Logger
}

// NewUploads returns the upload and download handlers. guards are the auth
// guards that may own an upload, tried in order: the browser session first,
// then an API key, so one endpoint serves the interface and the API.
func NewUploads(i *ingest.Ingester, f *files.Store, log *slog.Logger, guards ...string) *Uploads {
	if log == nil {
		log = slog.Default()
	}

	return &Uploads{ingester: i, files: f, guards: guards, log: log}
}

// TusConfig builds the resumable-upload handler's configuration.
//
// The upload directory is scratch space: a finished upload is identified,
// scanned, moved to the bucket and then deleted from here, so nothing in it
// has to survive a restart. Cleanup removes what was abandoned.
func (u *Uploads) TusConfig(dir, basePath string, maxBytes int64, expiry time.Duration) upload.TusConfig {
	return upload.TusConfig{
		Dir:        dir,
		BasePath:   basePath,
		MaxSize:    maxBytes,
		Expiry:     expiry,
		Authorize:  u.authorize,
		OnComplete: u.onComplete,
	}
}

// authorize refuses an unauthenticated upload.
//
// The route already sits behind the auth middleware, so this is the second
// lock on the same door: a change to the route tree that dropped the
// middleware would otherwise make the endpoint anonymous, and an anonymous
// resumable upload endpoint is free disk for anybody who finds it.
func (u *Uploads) authorize(r *http.Request) error {
	if _, err := u.owner(r.Context()); err != nil {
		return err
	}

	return nil
}

// owner returns the id of the user this request belongs to, trying each guard
// in turn.
func (u *Uploads) owner(ctx context.Context) (string, error) {
	for _, guard := range u.guards {
		if user, err := auth.UserFrom[models.User](ctx, guard); err == nil {
			return user.AuthID(), nil
		}
	}

	return "", auth.ErrUnauthenticated
}

// onComplete turns a finished upload into a stored file.
func (u *Uploads) onComplete(ctx context.Context, info upload.TusInfo, open func() (io.ReadCloser, error)) error {
	userID, err := u.owner(ctx)
	if err != nil {
		return err
	}

	stored, err := u.ingester.Accept(ctx, userID, info, open)
	if err != nil {
		return asHTTP(err)
	}

	u.log.InfoContext(ctx, "upload accepted",
		"file", stored.ID, "mime", stored.MIME, "bytes", stored.SizeBytes)

	return nil
}

// asHTTP turns a taxonomy failure into the response the client sees.
//
// The status and the sentence both come from the taxonomy, so a file refused
// at the door is worded exactly like a conversion that failed for the same
// reason; the owner-only detail goes nowhere near the response.
func asHTTP(err error) error {
	ce, ok := errors.AsType[*jobs.ConversionError](err)
	if !ok {
		return err
	}

	return errs.NewKey(jobs.Describe(ce.Type).Status, "upload."+string(ce.Type), jobs.MessageOf(ce), nil)
}

// UploadedResponse is what the client is told once an upload is stored. The
// tus protocol has no body of its own on a completed PATCH, so a client reads
// the file back with Show.
type UploadedResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	MIME string `json:"mime"`
	Size int64  `json:"size_bytes"`
}

// Download streams a file to its owner: GET /files/{id}.
//
// The link is signed and short-lived, and the signature covers the id, so the
// only thing the handler has to decide is whether the file still exists and
// is still within its lifetime. It is deliberately not scoped to the session:
// the signature is the authority, which is what lets a download link be
// opened in another tab, handed to `curl`, or followed by a download manager
// that carries no cookie.
func (u *Uploads) Download(c *web.Ctx) error {
	f, err := u.files.ByID(c.Context(), c.Request().PathValue("id"))
	if err != nil {
		return err
	}

	// The signature's own expiry is checked by the middleware. This is the
	// file's lifetime, which the cleanup may already have acted on.
	if !f.ExpiresAt.After(time.Now()) {
		return errs.NewKey(http.StatusGone, "files.expired",
			"That file has expired. Convert it again to get a new link.", nil)
	}

	body, err := u.files.Open(c.Context(), f)
	if err != nil {
		return err
	}

	defer func() {
		if cerr := body.Close(); cerr != nil {
			u.log.WarnContext(c.Context(), "download: closing the object failed", "file", f.ID, "err", cerr)
		}
	}()

	w := c.Writer()
	w.Header().Set("Content-Type", f.MIME)
	w.Header().Set("Content-Length", strconv.FormatInt(f.SizeBytes, 10))
	// attachment, not inline: a converted file is something the user asked
	// for, and serving user-supplied bytes inline on our own origin is how an
	// SVG becomes a script.
	w.Header().Set("Content-Disposition", contentDisposition(f.Name))
	// A signed link is per-user by construction; a shared cache must not keep
	// it, and no cache should ever serve it to somebody else.
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	if _, err := io.Copy(w, body); err != nil {
		// The status is already sent, so there is nothing to tell the client.
		// The log is the record that a download did not finish.
		u.log.WarnContext(c.Context(), "download: stream interrupted", "file", f.ID, "err", err)
	}

	return nil
}

// contentDisposition builds the header, giving a non-ASCII name the RFC 5987
// form so it survives rather than being mangled.
func contentDisposition(name string) string {
	ascii := make([]rune, 0, len(name))

	for _, r := range name {
		if r < 0x20 || r > 0x7e || r == '"' || r == '\\' {
			ascii = append(ascii, '_')

			continue
		}

		ascii = append(ascii, r)
	}

	return fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`,
		string(ascii), url.PathEscape(name))
}

// Types lists every type the service accepts at the door, so the interface
// can set a file picker's filter from the server rather than hardcoding it.
func (*Uploads) Types(_ *web.Ctx, _ struct{}) (TypeList, error) {
	return TypeList{Data: detect.Types()}, nil
}

// TypeList is the answer to GET /api/v1/upload-types.
type TypeList struct {
	Data []string `json:"data" doc:"Every MIME type an upload may have."`
}
