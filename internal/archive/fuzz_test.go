package archive_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AlexGalhardo/vukem-tools/internal/archive"
)

// FuzzExtract feeds arbitrary bytes to the extractor.
//
// Extraction is the hostile direction: the archive comes from the internet,
// and every one of its names, sizes and type flags is attacker-controlled.
// What this asserts is not that extraction succeeds — almost every input is
// not an archive — but that it never panics and never writes outside the
// directory it was given. The second half is the one worth fuzzing: a name is
// the only thing in an archive that reaches a path, and the whole defence is
// that only its basename survives.
func FuzzExtract(f *testing.F) {
	f.Add([]byte("not an archive at all"))
	f.Add(zipWith(f, "../../etc/passwd", "x"))
	f.Add(zipWith(f, `..\..\windows\win.ini`, "x"))
	f.Add(zipWith(f, "", "x"))
	f.Add(zipWith(f, ".", "x"))
	f.Add(zipWith(f, "..", "x"))
	f.Add(zipWith(f, strings.Repeat("n", 4096), "x"))
	f.Add(zipWith(f, "\x00\x01\x02", "x"))
	f.Add(zipWith(f, "ok.txt", strings.Repeat("y", 4096)))
	f.Add(tarWith(f, "../escape", tar.TypeReg))
	f.Add(tarWith(f, "link", tar.TypeSymlink))
	f.Add(tarWith(f, "dev", tar.TypeChar))
	f.Add(gzipOf(f, []byte("a plain compressed file")))
	f.Add(gzipOf(f, tarWith(f, "inside.txt", tar.TypeReg)))

	f.Fuzz(func(t *testing.T, raw []byte) {
		// The destination is a subdirectory of the test's own, so anything
		// written to its parent is visible as an escape.
		root := t.TempDir()

		dest := filepath.Join(root, "dest")
		if err := os.MkdirAll(dest, 0o700); err != nil {
			t.Fatal(err)
		}

		src := filepath.Join(root, "input")
		if err := os.WriteFile(src, raw, 0o600); err != nil {
			t.Fatal(err)
		}

		// A panic here fails the test, which is the first thing this is for.
		result, err := archive.Extract(context.Background(), dest, src)
		if err != nil {
			return
		}

		for _, entry := range result.Entries {
			if strings.ContainsAny(entry.Name, `/\`) {
				t.Fatalf("entry name %q holds a separator", entry.Name)
			}

			abs, err := filepath.Abs(entry.Path)
			if err != nil {
				t.Fatal(err)
			}

			if filepath.Dir(abs) != dest {
				t.Fatalf("entry %q landed in %q, not in %q", entry.Name, filepath.Dir(abs), dest)
			}
		}

		// Nothing may exist in the destination's parent but the destination
		// and the input file, whatever the archive asked for.
		names, err := os.ReadDir(root)
		if err != nil {
			t.Fatal(err)
		}

		for _, name := range names {
			if n := name.Name(); n != "dest" && n != "input" {
				t.Fatalf("extraction created %q outside the destination", n)
			}
		}
	})
}

// zipWith builds a one-entry ZIP under this name.
func zipWith(f *testing.F, name, body string) []byte {
	f.Helper()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	entry, err := zw.Create(name)
	if err != nil {
		// A name zip.Create refuses is not a seed; an empty slice still
		// exercises the "not an archive" path.
		return nil
	}

	if _, err := entry.Write([]byte(body)); err != nil {
		f.Fatal(err)
	}

	if err := zw.Close(); err != nil {
		f.Fatal(err)
	}

	return buf.Bytes()
}

// tarWith builds a one-entry TAR of this type.
func tarWith(f *testing.F, name string, flag byte) []byte {
	f.Helper()

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	header := &tar.Header{Name: name, Mode: 0o644, Typeflag: flag}
	if flag == tar.TypeReg {
		header.Size = 4
	}

	if flag == tar.TypeSymlink {
		header.Linkname = "/etc/passwd"
	}

	if err := tw.WriteHeader(header); err != nil {
		f.Fatal(err)
	}

	if flag == tar.TypeReg {
		if _, err := tw.Write([]byte("body")); err != nil {
			f.Fatal(err)
		}
	}

	if err := tw.Close(); err != nil {
		f.Fatal(err)
	}

	return buf.Bytes()
}

// gzipOf wraps bytes in a gzip stream.
func gzipOf(f *testing.F, raw []byte) []byte {
	f.Helper()

	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)

	if _, err := zw.Write(raw); err != nil {
		f.Fatal(err)
	}

	if err := zw.Close(); err != nil {
		f.Fatal(err)
	}

	return buf.Bytes()
}
