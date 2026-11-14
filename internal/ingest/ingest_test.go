package ingest_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AlexGalhardo/vukem/database"
	"github.com/AlexGalhardo/vukem/schema"
	"github.com/AlexGalhardo/vukem/signed"
	"github.com/AlexGalhardo/vukem/storage"
	"github.com/AlexGalhardo/vukem/ulid"
	"github.com/AlexGalhardo/vukem/upload"

	"github.com/AlexGalhardo/vukem-tools/database/migrations"
	"github.com/AlexGalhardo/vukem-tools/internal/detect"
	"github.com/AlexGalhardo/vukem-tools/internal/files"
	"github.com/AlexGalhardo/vukem-tools/internal/ingest"
	"github.com/AlexGalhardo/vukem-tools/internal/jobs"
)

const owner = "7"

// pngBytes is a PNG signature, which is all the detector reads.
var pngBytes = append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 64)...)

// scanner is a fake upload.Scanner.
type scanner struct {
	err   error
	calls int
}

func (s *scanner) Scan(_ context.Context, r io.Reader) error {
	s.calls++

	_, _ = io.Copy(io.Discard, r)

	return s.err
}

// store is a real file store over SQLite and an in-memory bucket.
func store(t *testing.T, maxStored int64) *files.Store {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "db")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("temp dir: %v", err)
	}

	db, err := database.Open(t.Context(), "sqlite:"+filepath.Join(dir, "ingest.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	t.Cleanup(func() {
		_ = db.Close()

		for range 20 {
			if os.RemoveAll(dir) == nil {
				return
			}

			time.Sleep(25 * time.Millisecond)
		}
	})

	m, err := schema.NewMigrator(db, migrations.All())
	if err != nil {
		t.Fatalf("migrator: %v", err)
	}

	if _, err := m.Up(t.Context()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	s, err := files.New(files.Options{
		DB:             db,
		Disk:           storage.NewMemory(""),
		IDs:            ulid.NewGenerator(),
		Signer:         signed.New(bytes.Repeat([]byte{7}, 32)),
		BaseURL:        "https://tools.example.com",
		TTL:            time.Hour,
		MaxStoredBytes: maxStored,
	})
	if err != nil {
		t.Fatalf("files: %v", err)
	}

	return s
}

func ingester(t *testing.T, s *files.Store, sc upload.Scanner, maxBytes int64) *ingest.Ingester {
	t.Helper()

	ing, err := ingest.New(ingest.Options{
		Files:    s,
		Scanner:  sc,
		Accepted: detect.Types(),
		MaxBytes: maxBytes,
	})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}

	return ing
}

func open(data []byte) ingest.Opener {
	return func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil }
}

func info(name string, length int) upload.TusInfo {
	return upload.TusInfo{Length: int64(length), Metadata: map[string]string{"filename": name}}
}

func errType(t *testing.T, err error) jobs.ErrorType {
	t.Helper()

	ce, ok := errors.AsType[*jobs.ConversionError](err)
	if !ok {
		t.Fatalf("error %v is not a *jobs.ConversionError", err)
	}

	return ce.Type
}

func TestAcceptStoresACleanFile(t *testing.T) {
	t.Parallel()

	sc := &scanner{}
	ing := ingester(t, store(t, 0), sc, 1<<20)

	f, err := ing.Accept(t.Context(), owner, info("a.png", len(pngBytes)), open(pngBytes))
	if err != nil {
		t.Fatalf("accept: %v", err)
	}

	if f.MIME != "image/png" || f.SizeBytes != int64(len(pngBytes)) {
		t.Errorf("stored %s, %d bytes", f.MIME, f.SizeBytes)
	}

	if sc.calls != 1 {
		t.Errorf("the scanner ran %d times, want 1", sc.calls)
	}
}

func TestAcceptRefusals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		sc      upload.Scanner
		info    upload.TusInfo
		body    []byte
		want    jobs.ErrorType
		scanned bool
	}{
		{
			name: "an infected file",
			sc:   &scanner{err: upload.ErrInfected},
			info: info("a.png", len(pngBytes)), body: pngBytes,
			want: jobs.Infected,
		},
		{
			// Failing open here would make "knock clamd over" the way to get
			// an infected file through.
			name: "a scanner that cannot be reached",
			sc:   &scanner{err: errors.New("dial tcp: connection refused")},
			info: info("a.png", len(pngBytes)), body: pngBytes,
			want: jobs.Internal,
		},
		{
			name: "a name that disagrees with the bytes",
			sc:   &scanner{},
			info: info("a.mp4", len(pngBytes)), body: pngBytes,
			want: jobs.UnsupportedInput,
		},
		{
			name: "bytes that are no known type",
			sc:   &scanner{},
			info: info("a.png", 5), body: []byte("hello"),
			want: jobs.UnsupportedInput,
		},
		{
			name: "a declared length past the limit",
			sc:   &scanner{},
			info: info("a.png", 1<<21), body: pngBytes,
			want: jobs.InputTooLarge,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s := store(t, 0)
			ing := ingester(t, s, tc.sc, 1<<20)

			if _, err := ing.Accept(t.Context(), owner, tc.info, open(tc.body)); err == nil {
				t.Fatal("the upload was accepted")
			} else if got := errType(t, err); got != tc.want {
				t.Errorf("refused as %s, want %s (%v)", got, tc.want, err)
			}

			if stored, err := s.StoredBytes(t.Context(), owner); err != nil || stored != 0 {
				t.Errorf("a refused upload left %d bytes stored (%v)", stored, err)
			}
		})
	}
}

func TestAcceptRefusesPastTheStoredBytesCap(t *testing.T) {
	t.Parallel()

	limit := int64(len(pngBytes)) * 2
	ing := ingester(t, store(t, limit), &scanner{}, 1<<20)

	for i := range 2 {
		if _, err := ing.Accept(t.Context(), owner, info("a.png", len(pngBytes)), open(pngBytes)); err != nil {
			t.Fatalf("upload %d under the cap: %v", i, err)
		}
	}

	_, err := ing.Accept(t.Context(), owner, info("a.png", len(pngBytes)), open(pngBytes))
	if err == nil || errType(t, err) != jobs.QuotaExceeded {
		t.Fatalf("a third upload over the cap: %v", err)
	}

	if err := ing.Room(t.Context(), "another-account", limit); err != nil {
		t.Errorf("the cap leaked across accounts: %v", err)
	}
}

func TestNewValidatesItsOptions(t *testing.T) {
	t.Parallel()

	s := store(t, 0)

	tests := map[string]ingest.Options{
		"no store":      {MaxBytes: 1},
		"no size limit": {Files: s},
		"unknown type":  {Files: s, MaxBytes: 1, Accepted: []string{"application/x-nonsense"}},
	}

	for name, o := range tests {
		if _, err := ingest.New(o); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}

	if _, err := ingest.New(ingest.Options{Files: s, MaxBytes: 1}); err != nil {
		t.Errorf("no scanner is allowed outside production: %v", err)
	}
}
