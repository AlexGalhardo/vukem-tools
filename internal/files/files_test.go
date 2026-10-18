package files_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AlexGalhardo/vukem/database"
	"github.com/AlexGalhardo/vukem/orm"
	"github.com/AlexGalhardo/vukem/schema"
	"github.com/AlexGalhardo/vukem/signed"
	"github.com/AlexGalhardo/vukem/storage"

	"github.com/AlexGalhardo/vukem-tools/app/models"
	"github.com/AlexGalhardo/vukem-tools/database/migrations"
	"github.com/AlexGalhardo/vukem-tools/internal/detect"
	"github.com/AlexGalhardo/vukem-tools/internal/files"
	"github.com/AlexGalhardo/vukem-tools/internal/ids"
)

const (
	owner      = "7"
	otherOwner = "8"
	fileTTL    = time.Hour
	baseURL    = "https://tools.example.com"
)

var pngKind = detect.Kind{MIME: "image/png", Extension: ".png", Label: "a PNG image"}

// clock is a hand-wound clock: every expiry decision reads it, so the tests
// move time rather than wait for it.
type clock struct {
	mu sync.Mutex
	at time.Time
}

func newClock() *clock {
	return &clock{at: time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)}
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.at
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.at = c.at.Add(d)
}

// failingDisk wraps a disk and fails the next call of one kind, so the tests
// can exercise what happens when the bucket is unavailable at exactly the
// wrong moment.
type failingDisk struct {
	storage.Disk

	mu         sync.Mutex
	failPut    error
	failDelete error
	deletes    []string
}

func (d *failingDisk) Put(ctx context.Context, p string, r io.Reader) error {
	d.mu.Lock()
	err := d.failPut
	d.mu.Unlock()

	if err != nil {
		return err
	}

	return d.Disk.Put(ctx, p, r)
}

func (d *failingDisk) Delete(ctx context.Context, p string) error {
	d.mu.Lock()
	d.deletes = append(d.deletes, p)
	err := d.failDelete
	d.mu.Unlock()

	if err != nil {
		return err
	}

	return d.Disk.Delete(ctx, p)
}

func (d *failingDisk) deleted() []string {
	d.mu.Lock()
	defer d.mu.Unlock()

	return append([]string(nil), d.deletes...)
}

type rig struct {
	store *files.Store
	db    *database.DB
	disk  *failingDisk
	clock *clock
	sign  *signed.Signer
}

func newRig(t *testing.T) *rig {
	t.Helper()

	// The database gets a directory of its own inside the test's, which this
	// cleanup empties with a few retries: on Windows the SQLite file can
	// still be held for a moment after Close, and t.TempDir's own cleanup
	// fails the test when its directory is not empty yet.
	dir := filepath.Join(t.TempDir(), "db")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("temp dir: %v", err)
	}

	db, err := database.Open(t.Context(), "sqlite:"+filepath.Join(dir, "files.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close: %v", err)
		}

		removeWithRetry(t, dir)
	})

	m, err := schema.NewMigrator(db, migrations.All())
	if err != nil {
		t.Fatalf("migrator: %v", err)
	}

	if _, err := m.Up(t.Context()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// A memory disk stands in for the bucket. What is under test here is the
	// bookkeeping — keys, rows, signatures, expiry — and storage.Disk is the
	// seam the real S3 driver plugs into.
	disk := &failingDisk{Disk: storage.NewMemory("")}
	clk := newClock()
	sign := signed.New(bytes.Repeat([]byte{7}, 32)).WithClock(clk.Now)

	store, err := files.New(files.Options{
		DB:      db,
		Disk:    disk,
		IDs:     ids.NewGenerator(),
		Signer:  sign,
		BaseURL: baseURL,
		TTL:     fileTTL,
		Now:     clk.Now,
	})
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	return &rig{store: store, db: db, disk: disk, clock: clk, sign: sign}
}

func removeWithRetry(t *testing.T, dir string) {
	t.Helper()

	var err error
	for range 20 {
		if err = os.RemoveAll(dir); err == nil {
			return
		}

		time.Sleep(25 * time.Millisecond)
	}

	t.Errorf("could not remove %s: %v", dir, err)
}

func (r *rig) put(t *testing.T, in files.Incoming, body string) models.File {
	t.Helper()

	if in.UserID == "" {
		in.UserID = owner
	}

	if in.Role == "" {
		in.Role = models.FileInput
	}

	if in.Kind.MIME == "" {
		in.Kind = pngKind
	}

	f, err := r.store.Put(t.Context(), in, strings.NewReader(body))
	if err != nil {
		t.Fatalf("put: %v", err)
	}

	return f
}

// A stored file records what was actually stored: the size counted as the
// bytes went past, and the SHA-256 of those same bytes. Trusting a
// client-declared size is how a quota gets bypassed.
func TestPutRecordsTheBytesItActuallyStored(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	body := strings.Repeat("pixel", 1000)

	f := r.put(t, files.Incoming{Name: "sprite.png"}, body)

	if f.SizeBytes != int64(len(body)) {
		t.Errorf("size = %d, want %d", f.SizeBytes, len(body))
	}

	want := sha256.Sum256([]byte(body))
	if f.Checksum == nil || *f.Checksum != hex.EncodeToString(want[:]) {
		t.Errorf("checksum = %v, want %s", f.Checksum, hex.EncodeToString(want[:]))
	}

	rc, err := r.store.Open(t.Context(), f)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	defer func() {
		if err := rc.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	}()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}

	if string(got) != body {
		t.Error("the stored bytes differ from what was written")
	}
}

// The name is sanitised here, not by the caller, and the extension is forced
// to match the detected type. A traversal attempt in a filename must not
// reach the key, and a stored file must not claim to be something it is not.
func TestPutSanitisesTheNameAndForcesTheExtension(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	cases := map[string]string{
		"../../etc/passwd":  "passwd.png",
		"holiday.mp4":       "holiday.png",
		`C:\Users\me\a.png`: "a.png",
		"":                  "file.png",
		".":                 "file.png",
	}

	for given, want := range cases {
		f := r.put(t, files.Incoming{Name: given}, "x")
		if f.Name != want {
			t.Errorf("Put(%q) stored the name %q, want %q", given, f.Name, want)
		}

		if strings.ContainsAny(f.DiskKey, `\`) || strings.Contains(f.DiskKey, "..") {
			t.Errorf("Put(%q) produced the key %q", given, f.DiskKey)
		}
	}
}

// The key is generated, never taken from the client, and it is scoped by
// owner and role so a bucket listing is explicable.
func TestTheDiskKeyIsGeneratedAndScoped(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	f := r.put(t, files.Incoming{Name: "sprite.png", Role: models.FileOutput}, "x")

	if !strings.HasPrefix(f.DiskKey, "u/"+owner+"/"+models.FileOutput+"/") {
		t.Fatalf("key = %q, want it scoped by owner and role", f.DiskKey)
	}

	if !strings.Contains(f.DiskKey, f.ID) {
		t.Fatalf("key = %q, want it to carry the file id %q", f.DiskKey, f.ID)
	}
}

// When the row cannot be written the object must not be left behind: nothing
// would ever point at it, so nothing would ever find it.
func TestAFailedRecordRemovesTheObject(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	// Writing the same id twice is the cheapest way to make the insert fail:
	// the id column is unique.
	first := r.put(t, files.Incoming{Name: "a.png"}, "x")

	// Close the database so the second insert cannot succeed.
	if err := r.db.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := r.store.Put(t.Context(), files.Incoming{
		UserID: owner, Role: models.FileInput, Name: "b.png", Kind: pngKind,
	}, strings.NewReader("y")); err == nil {
		t.Fatal("Put succeeded with a closed database")
	}

	deleted := r.disk.deleted()
	if len(deleted) == 0 {
		t.Fatal("the object was not removed after the row failed")
	}

	if deleted[len(deleted)-1] == first.DiskKey {
		t.Fatal("the wrong object was removed")
	}
}

// One user must not reach another's file, and a file that is not theirs is
// answered exactly like one that does not exist.
func TestAFileIsInvisibleToAnotherUser(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	f := r.put(t, files.Incoming{Name: "private.png"}, "secret")

	if _, err := r.store.Find(t.Context(), otherOwner, f.ID); !errors.Is(err, files.ErrNotFound) {
		t.Fatalf("Find by another user = %v, want ErrNotFound", err)
	}

	if _, err := r.store.Find(t.Context(), owner, f.ID); err != nil {
		t.Fatalf("Find by the owner: %v", err)
	}
}

// A value that cannot be an id is refused without a database round trip, and
// answered like a miss.
func TestAMalformedIDIsAPlainMiss(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	for _, id := range []string{"", "1", "../../etc/passwd", "' OR 1=1 --"} {
		if _, err := r.store.Find(t.Context(), owner, id); !errors.Is(err, files.ErrNotFound) {
			t.Errorf("Find(%q) = %v, want ErrNotFound", id, err)
		}
	}
}

// Attaching an input to a conversion must happen once. The second attempt
// finds nothing to attach rather than sharing one upload between two
// conversions whose lifetimes then disagree.
func TestAnInputAttachesToExactlyOneConversion(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	f := r.put(t, files.Incoming{Name: "sprite.png"}, "x")

	gen := ids.NewGenerator()
	first, second := gen.New(), gen.New()

	attached, err := r.store.Attach(t.Context(), owner, f.ID, first)
	if err != nil {
		t.Fatal(err)
	}

	if !attached {
		t.Fatal("the first attach did nothing")
	}

	attached, err = r.store.Attach(t.Context(), owner, f.ID, second)
	if err != nil {
		t.Fatal(err)
	}

	if attached {
		t.Fatal("the same upload was attached to a second conversion")
	}

	rows, err := r.store.ForConversion(t.Context(), first, models.FileInput)
	if err != nil {
		t.Fatal(err)
	}

	if len(rows) != 1 || rows[0].ID != f.ID {
		t.Fatalf("ForConversion returned %d rows: %+v", len(rows), rows)
	}
}

// Another user cannot attach somebody else's upload to their own conversion.
func TestAttachIsScopedToTheOwner(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	f := r.put(t, files.Incoming{Name: "sprite.png"}, "x")

	attached, err := r.store.Attach(t.Context(), otherOwner, f.ID, ids.NewGenerator().New())
	if err != nil {
		t.Fatal(err)
	}

	if attached {
		t.Fatal("another user attached a file that is not theirs")
	}
}

// A download link is signed and expiring, points at this app rather than at
// the bucket, and verifies.
func TestDownloadURLIsSignedAndVerifies(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	f := r.put(t, files.Incoming{Name: "sprite.png", Role: models.FileOutput}, "x")

	link, err := r.store.DownloadURL(f, 5*time.Minute)
	if err != nil {
		t.Fatalf("DownloadURL: %v", err)
	}

	if !strings.HasPrefix(link, baseURL+files.DownloadPath) {
		t.Fatalf("link = %q, want it to point at this app", link)
	}

	if strings.Contains(link, f.DiskKey) {
		t.Fatalf("the link leaks the bucket key: %q", link)
	}

	req := httptest.NewRequest(http.MethodGet, link, http.NoBody)
	if err := r.sign.Verify(req); err != nil {
		t.Fatalf("the signed link does not verify: %v", err)
	}

	// Tampering with the id must break the signature, or anyone could read
	// any file by editing a link.
	tampered := strings.Replace(link, f.ID, ids.NewGenerator().New(), 1)
	if err := r.sign.Verify(httptest.NewRequest(http.MethodGet, tampered, http.NoBody)); err == nil {
		t.Fatal("a tampered link verified")
	}
}

// A link must stop working once its time is up — the ROADMAP's own assert.
func TestADownloadLinkStopsWorkingAfterItsTTL(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	f := r.put(t, files.Incoming{Name: "sprite.png", Role: models.FileOutput}, "x")

	link, err := r.store.DownloadURL(f, time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	r.clock.Advance(2 * time.Minute)

	if err := r.sign.Verify(httptest.NewRequest(http.MethodGet, link, http.NoBody)); err == nil {
		t.Fatal("an expired link still verified")
	}
}

// A link can never outlive the object it points at: a link that resolves to
// a deleted file is a worse answer than a link that has expired.
func TestALinkCannotOutliveTheFile(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	f := r.put(t, files.Incoming{Name: "sprite.png", Role: models.FileOutput}, "x")

	// Ask for a day; the file lives for an hour.
	link, err := r.store.DownloadURL(f, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	r.clock.Advance(fileTTL + time.Minute)

	if err := r.sign.Verify(httptest.NewRequest(http.MethodGet, link, http.NoBody)); err == nil {
		t.Fatal("a link outlived the file it points at")
	}
}

// Once a file has expired there is no link to make for it.
func TestNoLinkIsMadeForAnExpiredFile(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	f := r.put(t, files.Incoming{Name: "sprite.png", Role: models.FileOutput}, "x")

	r.clock.Advance(fileTTL + time.Second)

	if _, err := r.store.DownloadURL(f, time.Minute); !errors.Is(err, files.ErrExpired) {
		t.Fatalf("err = %v, want ErrExpired", err)
	}
}

// The ROADMAP's assert: the cleanup run twice in a row is idempotent, and the
// object is really gone from the bucket.
func TestTheSweepIsIdempotentAndRemovesTheObject(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	kept := r.put(t, files.Incoming{Name: "fresh.png"}, "x")
	r.clock.Advance(fileTTL + time.Minute)
	stale := r.put(t, files.Incoming{Name: "stale.png"}, "y")
	// stale was created after the jump, so push time on again to expire it
	// and leave kept expired too.
	r.clock.Advance(fileTTL + time.Minute)

	deleted, err := r.store.Sweep(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}

	if deleted != 2 {
		t.Fatalf("the first sweep deleted %d files, want 2", deleted)
	}

	// The objects must be gone, not just the rows.
	for _, f := range []models.File{kept, stale} {
		if _, err := r.store.Open(t.Context(), f); err == nil {
			t.Fatalf("the object for %s is still in the bucket", f.ID)
		}
	}

	deleted, err = r.store.Sweep(t.Context(), 0)
	if err != nil {
		t.Fatalf("the second sweep failed: %v", err)
	}

	if deleted != 0 {
		t.Fatalf("the second sweep deleted %d files, want 0", deleted)
	}

	count, err := orm.QueryOf[models.File](r.db).Count(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if count != 0 {
		t.Fatalf("%d file rows survived the sweep", count)
	}
}

// Deleting a row whose object is already gone must succeed, because that is
// exactly the state a sweep interrupted halfway leaves behind.
func TestDeleteToleratesAMissingObject(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	f := r.put(t, files.Incoming{Name: "sprite.png"}, "x")

	if err := r.disk.Disk.Delete(t.Context(), f.DiskKey); err != nil {
		t.Fatal(err)
	}

	if err := r.store.Delete(t.Context(), f); err != nil {
		t.Fatalf("Delete with the object already gone: %v", err)
	}
}

// A bucket that is down must not leave a row pointing at nothing: the row
// stays, and the next sweep tries again.
func TestASweepKeepsTheRowWhenTheBucketFails(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	f := r.put(t, files.Incoming{Name: "sprite.png"}, "x")
	r.clock.Advance(fileTTL + time.Minute)

	r.disk.mu.Lock()
	r.disk.failDelete = errors.New("the bucket is unreachable")
	r.disk.mu.Unlock()

	deleted, err := r.store.Sweep(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}

	if deleted != 0 {
		t.Fatalf("the sweep reported %d deletions while the bucket was down", deleted)
	}

	if _, err := r.store.Find(t.Context(), owner, f.ID); err != nil {
		t.Fatalf("the row was dropped although the object survived: %v", err)
	}
}

// A failed upload records nothing: a row pointing at an object that was never
// written would be a file nobody can download and nobody can explain.
func TestAFailedUploadRecordsNothing(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	r.disk.mu.Lock()
	r.disk.failPut = errors.New("the bucket is unreachable")
	r.disk.mu.Unlock()

	if _, err := r.store.Put(t.Context(), files.Incoming{
		UserID: owner, Role: models.FileInput, Name: "a.png", Kind: pngKind,
	}, strings.NewReader("x")); err == nil {
		t.Fatal("Put succeeded with the bucket down")
	}

	count, err := orm.QueryOf[models.File](r.db).Count(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if count != 0 {
		t.Fatalf("%d rows were recorded for a failed upload", count)
	}
}

func TestPutRefusesAnUnknownRole(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	if _, err := r.store.Put(t.Context(), files.Incoming{
		UserID: owner, Role: "sideways", Name: "a.png", Kind: pngKind,
	}, strings.NewReader("x")); err == nil {
		t.Fatal("an unknown role was accepted")
	}
}

func TestPutRefusesAFileWithNoOwner(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	if _, err := r.store.Put(t.Context(), files.Incoming{
		Role: models.FileInput, Name: "a.png", Kind: pngKind,
	}, strings.NewReader("x")); err == nil {
		t.Fatal("a file with no owner was accepted")
	}
}

func TestNewValidatesItsOptions(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	gen := ids.NewGenerator()
	sign := signed.New(bytes.Repeat([]byte{7}, 32))
	disk := storage.NewMemory("")

	cases := map[string]files.Options{
		"no database":  {Disk: disk, IDs: gen, Signer: sign, TTL: time.Hour},
		"no disk":      {DB: r.db, IDs: gen, Signer: sign, TTL: time.Hour},
		"no generator": {DB: r.db, Disk: disk, Signer: sign, TTL: time.Hour},
		"no signer":    {DB: r.db, Disk: disk, IDs: gen, TTL: time.Hour},
		"no lifetime":  {DB: r.db, Disk: disk, IDs: gen, Signer: sign},
		"negative":     {DB: r.db, Disk: disk, IDs: gen, Signer: sign, TTL: -time.Hour},
	}

	for name, o := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := files.New(o); err == nil {
				t.Fatal("the options were accepted")
			}
		})
	}
}
