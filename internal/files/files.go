// Package files records every file the service holds and moves their bytes.
//
// The bytes live in object storage; the `files` table is the index over them
// and the only thing the cleanup has to read. The two services share the
// bucket and nothing else: the web service accepts an upload and puts it
// there, the worker reads it from there and puts its result back. Local disk
// is scratch space for one conversion, never the store — a worker can be
// replaced between two attempts and the result must survive it.
//
// Nothing here buffers a whole file. A half-gigabyte upload is streamed from
// the request to the bucket and back, because this app has to run on a small
// container.
package files

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/AlexGalhardo/vukem/database"
	"github.com/AlexGalhardo/vukem/orm"
	"github.com/AlexGalhardo/vukem/signed"
	"github.com/AlexGalhardo/vukem/storage"
	"github.com/AlexGalhardo/vukem/upload"

	"github.com/AlexGalhardo/vukem-tools/app/models"
	"github.com/AlexGalhardo/vukem-tools/internal/detect"
	"github.com/AlexGalhardo/vukem-tools/internal/ids"
)

// ErrNotFound is returned when no file matches. It is the ORM's own 404, so
// a controller that returns it answers 404 without mapping anything, and a
// file owned by somebody else is answered exactly like one that never
// existed.
var ErrNotFound = orm.ErrNotFound

// Store reads and writes files.
type Store struct {
	db     *database.DB
	disk   storage.Disk
	ids    *ids.Generator
	signer *signed.Signer
	base   string
	ttl    time.Duration
	now    func() time.Time
	log    *slog.Logger
}

// Options configures a Store. DB, Disk, IDs, Signer and TTL are required.
type Options struct {
	DB   *database.DB
	Disk storage.Disk
	IDs  *ids.Generator
	// Signer signs download URLs. A download goes through the app rather
	// than straight to the bucket, so the bucket stays private and a link
	// can be made to expire without touching the object.
	Signer *signed.Signer
	// BaseURL is the address users reach, used to build download links.
	BaseURL string
	// TTL is how long a file stays downloadable once it exists.
	TTL time.Duration
	Now func() time.Time
	Log *slog.Logger
}

// New returns a store.
func New(o Options) (*Store, error) {
	switch {
	case o.DB == nil:
		return nil, errors.New("files: store needs a database")
	case o.Disk == nil:
		return nil, errors.New("files: store needs a disk")
	case o.IDs == nil:
		return nil, errors.New("files: store needs an id generator")
	case o.Signer == nil:
		return nil, errors.New("files: store needs a signer for download URLs")
	case o.TTL <= 0:
		return nil, fmt.Errorf("files: lifetime must be positive, got %s", o.TTL)
	}

	if o.Now == nil {
		o.Now = time.Now
	}

	if o.Log == nil {
		o.Log = slog.Default()
	}

	return &Store{
		db:     o.DB,
		disk:   o.Disk,
		ids:    o.IDs,
		signer: o.Signer,
		base:   strings.TrimRight(o.BaseURL, "/"),
		ttl:    o.TTL,
		now:    o.Now,
		log:    o.Log,
	}, nil
}

// Incoming describes a file about to be stored.
type Incoming struct {
	// UserID is the owner. Every read is scoped to it.
	UserID string
	// ConversionID is the conversion this file belongs to, empty for an
	// upload that has not been attached to one yet.
	ConversionID string
	// Role is models.FileInput or models.FileOutput.
	Role string
	// Name is the name the client sent. It is sanitised here, not by the
	// caller, so no path can arrive through it.
	Name string
	// Kind is the type detected from the bytes. The caller must have
	// detected it already: this package does not guess, because the decision
	// to accept a type belongs to whichever tool is going to read it.
	Kind detect.Kind
}

// Put streams r into the bucket and records the file.
//
// The bytes go up first and the row second. The other order would leave a row
// pointing at an object that does not exist, which every reader would then
// have to handle; this order can leave an object with no row, which only the
// cleanup has to handle, and it deletes the object on a failed insert anyway.
//
// The checksum is computed in the same pass as the upload — a second pass
// would mean reading the whole file back out of the bucket.
func (s *Store) Put(ctx context.Context, in Incoming, r io.Reader) (models.File, error) {
	if in.UserID == "" {
		return models.File{}, errors.New("files: a file needs an owner")
	}

	if in.Role != models.FileInput && in.Role != models.FileOutput {
		return models.File{}, fmt.Errorf("files: unknown role %q", in.Role)
	}

	at := s.now().UTC()
	id := s.ids.At(at)
	name := safeName(in.Name, in.Kind)

	key, err := diskKey(in.UserID, in.Role, id, in.Kind.Extension)
	if err != nil {
		return models.File{}, err
	}

	// Counting and hashing as the bytes pass means the size recorded is the
	// size actually stored, not the size the client claimed.
	meter := &countingHasher{hash: sha256.New()}

	if err := s.disk.Put(ctx, key, io.TeeReader(r, meter)); err != nil {
		return models.File{}, fmt.Errorf("files: store %s: %w", key, err)
	}

	f := models.File{
		ID:        id,
		UserID:    in.UserID,
		Role:      in.Role,
		DiskKey:   key,
		Name:      name,
		MIME:      in.Kind.MIME,
		SizeBytes: meter.n,
		ExpiresAt: at.Add(s.ttl),
		CreatedAt: at,
	}

	sum := meter.sum()
	f.Checksum = &sum

	if in.ConversionID != "" {
		f.ConversionID = &in.ConversionID
	}

	if err := orm.Create(ctx, s.db, &f); err != nil {
		// The object is already in the bucket and nothing points at it, so
		// remove it now rather than leave it for the cleanup to find by
		// scanning. A failure here is logged: the insert error is what the
		// caller needs to hear.
		if derr := s.disk.Delete(ctx, key); derr != nil {
			s.log.ErrorContext(ctx, "files: orphan object left in the bucket", "key", key, "err", derr)
		}

		return models.File{}, fmt.Errorf("files: record %s: %w", key, err)
	}

	return f, nil
}

// countingHasher counts and hashes bytes as they stream past.
type countingHasher struct {
	hash interface {
		io.Writer
		Sum([]byte) []byte
	}
	n int64
}

func (c *countingHasher) Write(p []byte) (int, error) {
	c.n += int64(len(p))

	return c.hash.Write(p)
}

func (c *countingHasher) sum() string { return hex.EncodeToString(c.hash.Sum(nil)) }

// safeName returns a name that is safe to store and to put in a
// Content-Disposition header.
//
// upload.SanitizeName removes path separators, control characters and the
// Unicode format characters that can disguise an extension — a right-to-left
// override before "fdp.exe" displays as "exe.pdf". The extension is then
// forced to match the detected type, so a stored file cannot claim to be
// something it is not even if the sanitiser let the name through.
func safeName(name string, kind detect.Kind) string {
	clean := upload.SanitizeName(name)
	if clean == "" {
		clean = "file"
	}

	if kind.Extension == "" {
		return clean
	}

	stem := strings.TrimSuffix(clean, path.Ext(clean))
	if stem == "" {
		stem = "file"
	}

	return stem + kind.Extension
}

// diskKey is where a file's bytes live in the bucket.
//
// The key is readable on purpose: it is never given to a client — a download
// goes through a signed app URL — so there is nothing to hide, and a readable
// key is what makes a bucket listing explicable at three in the morning.
// Every segment is generated here, so nothing from a client reaches it.
func diskKey(userID, role, id, ext string) (string, error) {
	if strings.ContainsAny(userID, "/\\.") {
		// The auth layer hands out a numeric id; anything else is a bug
		// upstream, and a key is not the place to find out.
		return "", fmt.Errorf("files: unusable owner id %q", userID)
	}

	return storage.Clean(path.Join("u", userID, role, id+ext))
}

// Find returns the file with this id belonging to userID.
func (s *Store) Find(ctx context.Context, userID, id string) (models.File, error) {
	// A value that cannot be an id does not need a database round trip, and
	// answering it the same way as a real miss keeps ids unguessable.
	if !ids.Valid(id) {
		return models.File{}, ErrNotFound
	}

	return orm.QueryOf[models.File](s.db).
		Where("user_id", userID).
		Where("id", id).
		First(ctx)
}

// ForConversion returns a conversion's files with this role, oldest first.
// It is not scoped to a user: the worker runs on nobody's behalf, and every
// path that serves a request reaches a conversion through its own scoped
// lookup first.
func (s *Store) ForConversion(ctx context.Context, conversionID, role string) ([]models.File, error) {
	if !ids.Valid(conversionID) {
		return nil, ErrNotFound
	}

	return orm.QueryOf[models.File](s.db).
		Where("conversion_id", conversionID).
		Where("role", role).
		OrderBy("id", orm.Asc).
		Get(ctx)
}

// Attach links an uploaded input to a conversion, and reports whether it did.
//
// It is conditional on the file being the caller's, being an input and not
// already belonging to a conversion. That last condition is what stops the
// same upload being submitted twice: the second attempt finds nothing to
// attach rather than quietly sharing one file between two conversions whose
// lifetimes then disagree.
func (s *Store) Attach(ctx context.Context, userID, fileID, conversionID string) (bool, error) {
	if !ids.Valid(fileID) || !ids.Valid(conversionID) {
		return false, ErrNotFound
	}

	n, err := orm.QueryOf[models.File](s.db).
		Where("user_id", userID).
		Where("id", fileID).
		Where("role", models.FileInput).
		WhereNull("conversion_id").
		Update(ctx, orm.Set{"conversion_id": conversionID})
	if err != nil {
		return false, fmt.Errorf("files: attach %s: %w", fileID, err)
	}

	return n > 0, nil
}

// Open streams a file's bytes out of the bucket. The caller closes the
// reader.
func (s *Store) Open(ctx context.Context, f models.File) (io.ReadCloser, error) {
	rc, err := s.disk.Open(ctx, f.DiskKey)
	if err != nil {
		return nil, fmt.Errorf("files: open %s: %w", f.DiskKey, err)
	}

	return rc, nil
}

// DownloadPath is the route a download goes through. It is exported so the
// route and the link cannot disagree about it.
const DownloadPath = "/files/"

// DownloadURL returns a signed, expiring link to f.
//
// The link points at this app, not at the bucket: the bucket stays private,
// the expiry is ours to set, and a leaked link stops working on its own. ttl
// is clamped to what is left of the file's own lifetime, so a link can never
// outlive the object it points at — a link that resolves to a deleted file is
// a worse answer than a link that has expired.
func (s *Store) DownloadURL(f models.File, ttl time.Duration) (string, error) {
	if left := f.ExpiresAt.Sub(s.now()); ttl <= 0 || ttl > left {
		ttl = left
	}

	if ttl <= 0 {
		return "", ErrExpired
	}

	raw := s.base + DownloadPath + url.PathEscape(f.ID)

	signedURL, err := s.signer.Sign(raw, ttl)
	if err != nil {
		return "", fmt.Errorf("files: sign a download link: %w", err)
	}

	return signedURL, nil
}

// ErrExpired means the file's lifetime has already run out, so no link can be
// made for it.
var ErrExpired = errors.New("files: the file has expired")

// Expired returns files past their lifetime, oldest first, for the cleanup.
func (s *Store) Expired(ctx context.Context, limit int) ([]models.File, error) {
	if limit <= 0 || limit > maxSweep {
		limit = maxSweep
	}

	return orm.QueryOf[models.File](s.db).
		Where("expires_at", "<", s.now().UTC()).
		OrderBy("expires_at", orm.Asc).
		Limit(limit).
		Get(ctx)
}

// maxSweep caps one cleanup pass, so a backlog cannot hold the scheduler.
const maxSweep = 200

// Delete removes a file's bytes and then its row.
//
// That order is what makes the cleanup idempotent. A failure between the two
// leaves a row pointing at an object that is already gone, and the next sweep
// deletes it again — object storage treats deleting a missing key as success,
// so the retry is harmless. The other order would leave an object nothing
// points at, which nothing would ever find again.
func (s *Store) Delete(ctx context.Context, f models.File) error {
	if err := s.disk.Delete(ctx, f.DiskKey); err != nil && !errors.Is(err, storage.ErrNotFound) {
		return fmt.Errorf("files: delete %s: %w", f.DiskKey, err)
	}

	if err := orm.Delete(ctx, s.db, &f); err != nil {
		return fmt.Errorf("files: forget %s: %w", f.ID, err)
	}

	return nil
}

// Sweep deletes every expired file it finds, up to limit, and returns how
// many it deleted. One failure does not stop the pass: the rest are just as
// expired, and the next sweep retries this one.
func (s *Store) Sweep(ctx context.Context, limit int) (int, error) {
	expired, err := s.Expired(ctx, limit)
	if err != nil {
		return 0, err
	}

	var deleted int

	for i := range expired {
		f := &expired[i]

		if err := s.Delete(ctx, *f); err != nil {
			s.log.ErrorContext(ctx, "files: could not delete an expired file",
				"file", f.ID, "key", f.DiskKey, "err", err)

			continue
		}

		deleted++
	}

	return deleted, nil
}
