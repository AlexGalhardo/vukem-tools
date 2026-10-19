package archive_test

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/AlexGalhardo/vukem-tools/internal/archive"
)

// The figures these record go in docs/benchmarks.md. What matters is not the
// wall clock — stdlib deflate over a few megabytes is not where this service
// spends its time — but that *live* memory does not grow with the member
// count. That is the property the streaming design exists for, and the only
// one a regression here would quietly take away.
//
// -benchmem's B/op cannot show it: that is every byte ever allocated during
// the operation, which does grow with the member count, because each ZIP
// member gets its own flate reader. That churn is fine and is the GC's
// business. The question is how much is live at once, which
// TestPeakMemoryDoesNotGrowWithMemberCount answers.

// benchMembers writes n files of size bytes each into a temporary directory.
func benchMembers(b *testing.B, n, size int) []archive.Member {
	b.Helper()

	dir := b.TempDir()
	// Compressible but not trivially so, which is what a real file looks like
	// to deflate. All zeros would measure the fast path and nothing else.
	body := make([]byte, size)
	for i := range body {
		body[i] = byte(i % 251)
	}

	out := make([]archive.Member, 0, n)

	for i := range n {
		name := "member-" + string(rune('a'+i%26)) + ".bin"
		path := filepath.Join(dir, name)

		if err := os.WriteFile(path, body, 0o600); err != nil {
			b.Fatal(err)
		}

		out = append(out, archive.Member{Name: name, Path: path})
	}

	return out
}

func BenchmarkCreateZip(b *testing.B) {
	members := benchMembers(b, 32, 64<<10)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if _, err := archive.Create(
			context.Background(), io.Discard, members, archive.CreateOptions{Format: archive.FormatZip},
		); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCreateTarGz(b *testing.B) {
	members := benchMembers(b, 32, 64<<10)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if _, err := archive.Create(
			context.Background(), io.Discard, members, archive.CreateOptions{Format: archive.FormatTarGz},
		); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkExtractZip(b *testing.B) {
	members := benchMembers(b, 32, 64<<10)

	var buf bytes.Buffer
	if _, err := archive.Create(
		context.Background(), &buf, members, archive.CreateOptions{Format: archive.FormatZip},
	); err != nil {
		b.Fatal(err)
	}

	src := filepath.Join(b.TempDir(), "bench.zip")
	if err := os.WriteFile(src, buf.Bytes(), 0o600); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if _, err := archive.Extract(context.Background(), b.TempDir(), src); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkExtractByMemberCount splits the same total bytes across 8, 32 and
// 64 members, so the cost of the member count itself is visible separately
// from the cost of the bytes.
func BenchmarkExtractByMemberCount(b *testing.B) {
	const total = 2 << 20

	for _, n := range []int{8, 32, 64} {
		b.Run("members="+itoa(n), func(b *testing.B) {
			members := benchMembers(b, n, total/n)

			var buf bytes.Buffer
			if _, err := archive.Create(
				context.Background(), &buf, members, archive.CreateOptions{Format: archive.FormatZip},
			); err != nil {
				b.Fatal(err)
			}

			src := filepath.Join(b.TempDir(), "bench.zip")
			if err := os.WriteFile(src, buf.Bytes(), 0o600); err != nil {
				b.Fatal(err)
			}

			b.ReportAllocs()
			b.ResetTimer()

			for b.Loop() {
				if _, err := archive.Extract(context.Background(), b.TempDir(), src); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// itoa avoids importing strconv for one call in a sub-benchmark name.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}

	var digits []byte

	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}

	return string(digits)
}

// A ZIP reader must not be measured against a writer that shares its buffer,
// which this keeps honest by proving the fixture really is a readable archive.
func TestTheBenchmarkFixtureIsARealArchive(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	body := bytes.Repeat([]byte("fixture"), 64)
	path := filepath.Join(dir, "a.bin")

	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if _, err := archive.Create(
		context.Background(), &buf,
		[]archive.Member{{Name: "a.bin", Path: path}},
		archive.CreateOptions{Format: archive.FormatZip},
	); err != nil {
		t.Fatal(err)
	}

	if _, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len())); err != nil {
		t.Fatalf("the fixture is not a readable ZIP: %v", err)
	}
}

// Live memory must stay bounded however an archive is shaped: it must never
// hold an appreciable fraction of the archive, and it must not grow at all
// with the size of the largest member.
//
// Tests rather than benchmarks because they assert a bound rather than report
// a number, which is the call internal/spritesheet made for the same property.
// A regression — collecting members into a slice, or reading one whole into
// memory before writing it — is the kind that passes every other test while
// making the tool unrunnable on a small container.
//
// Both measure Go's own peak live heap rather than the process's RSS: this
// package's allocations are all the runtime's, and an RSS number from one host
// is not comparable with the container's. docs/benchmarks.md records that
// limitation rather than presenting the one as the other.
//
// The peak is sampled during the operation rather than read after it.
// Reading MemStats afterwards measures whatever the sweeper has not reclaimed
// yet, which for this package is the per-member flate reader churn — that
// does grow with the member count, and it is not the question. What is live
// at once is.

// peakDuring runs fn and returns the highest HeapAlloc seen while it ran.
//
// Sampled from a goroutine rather than computed from a before-and-after pair,
// because a peak in the middle of a streaming operation is invisible to a
// reading taken at either end — which is exactly the shape of the bug these
// tests exist to catch.
func peakDuring(t *testing.T, fn func()) uint64 {
	t.Helper()

	runtime.GC()

	var peak atomic.Uint64

	done := make(chan struct{})
	stopped := make(chan struct{})

	go func() {
		defer close(stopped)

		var stats runtime.MemStats

		for {
			select {
			case <-done:
				return
			default:
			}

			runtime.ReadMemStats(&stats)

			for {
				was := peak.Load()
				if stats.HeapAlloc <= was || peak.CompareAndSwap(was, stats.HeapAlloc) {
					break
				}
			}
		}
	}()

	fn()
	close(done)
	<-stopped

	return peak.Load()
}

// Extracting an archive must never hold an appreciable fraction of it in
// memory, however many members it is split into.
//
// The bound is stated against the archive's total rather than as "flat",
// because the peak is not flat and it is worth being exact about why. Each
// ZIP member is inflated through its own flate reader, whose 32 KiB window
// and Huffman tables are about 33 KiB of short-lived allocation; sixty-four
// members churn roughly 2 MiB through the heap, and Go's HeapAlloc counts
// allocated-but-not-yet-swept bytes, so a peak sampled between collections
// sees some of that churn. It is churn, not accumulation: the companion test
// below fixes the member count and grows the member size by 128x, and the
// peak there does not move at all, which is what proves a member's bytes are
// streamed rather than buffered.
//
// What this test catches is the regression that actually matters — members
// collected into a slice, or the whole archive inflated before anything is
// written — because either would put the total, not a fraction of it, on the
// heap.
func TestExtractionNeverHoldsMuchOfTheArchive(t *testing.T) {
	if testing.Short() {
		t.Skip("writes a few megabytes of fixtures")
	}

	const total = 8 << 20

	// A fixed allowance per member for its decompressor, plus a floor for the
	// runtime's own baseline. Generous — the measured cost is about 33 KiB —
	// and still far below what keeping a member would cost.
	const (
		perMember = 64 << 10
		baseline  = 2 << 20
	)

	for _, members := range []int{4, 16, 64} {
		src := zipFixture(t, members, total/members)
		dest := t.TempDir()

		peak := peakDuring(t, func() {
			if _, err := archive.Extract(context.Background(), dest, src); err != nil {
				t.Errorf("Extract of %d members: %v", members, err)
			}
		})

		t.Logf("%d members of %d bytes: peak live heap %d bytes (%.1f MiB)",
			members, total/members, peak, float64(peak)/(1<<20))

		if ceiling := uint64(baseline + perMember*members); peak > ceiling {
			t.Errorf("%d members: peak live heap %d bytes is over the ceiling of %d "+
				"(%d baseline plus %d per member): members are being kept, not streamed",
				members, peak, ceiling, baseline, perMember)
		}

		// The claim that matters on its own terms: never an appreciable
		// fraction of the archive, whatever the shape.
		if peak > total/2 {
			t.Errorf("%d members: peak live heap %d bytes is more than half the %d-byte archive",
				members, peak, total)
		}
	}
}

// A bigger member must not cost more live heap.
//
// This is the experiment that detects a member being buffered whole, and it
// is the opposite shape to the one above: the member count is fixed and the
// member size grows, so a design that reads a member into memory before
// writing it grows with the size while a streaming one does not. Varying the
// count with the total fixed cannot show it — there, buffering one member
// would make the peak *smaller* as members got smaller.
func TestPeakMemoryDoesNotGrowWithMemberSize(t *testing.T) {
	if testing.Short() {
		t.Skip("writes a few megabytes of fixtures")
	}

	// A fixed ceiling rather than a ratio against the smallest sample. The
	// smallest sample is below the runtime's own noise floor — under -race it
	// has measured anywhere from 0.4 to 1.4 MiB for the same work — so a
	// ratio against it is a coin toss, while the claim itself is absolute:
	// the peak does not follow the member size. Measured at about 1.45 MiB
	// for every size below; four mebibytes is generous slack and still half
	// of what buffering the largest member alone would cost.
	const ceiling = 4 << 20

	for _, size := range []int{64 << 10, 1 << 20, 8 << 20} {
		src := zipFixture(t, 2, size)
		dest := t.TempDir()

		peak := peakDuring(t, func() {
			if _, err := archive.Extract(context.Background(), dest, src); err != nil {
				t.Errorf("Extract of %d-byte members: %v", size, err)
			}
		})

		t.Logf("2 members of %d bytes: peak live heap %d bytes (%.1f MiB)",
			size, peak, float64(peak)/(1<<20))

		if peak > ceiling {
			t.Errorf("2 members of %d bytes: peak live heap %d bytes is over the ceiling of %d: "+
				"a member is being read whole rather than streamed", size, peak, ceiling)
		}
	}
}

// zipFixture writes a ZIP of n members of this size and returns its path.
func zipFixture(t *testing.T, n, size int) string {
	t.Helper()

	dir := t.TempDir()

	body := make([]byte, size)
	for i := range body {
		body[i] = byte(i % 251)
	}

	members := make([]archive.Member, 0, n)

	for i := range n {
		name := "member-" + itoa(i) + ".bin"
		path := filepath.Join(dir, name)

		if err := os.WriteFile(path, body, 0o600); err != nil {
			t.Fatal(err)
		}

		members = append(members, archive.Member{Name: name, Path: path})
	}

	src := filepath.Join(dir, "fixture.zip")

	dst, err := os.Create(src)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := archive.Create(
		context.Background(), dst, members, archive.CreateOptions{Format: archive.FormatZip},
	); err != nil {
		t.Fatal(err)
	}

	if err := dst.Close(); err != nil {
		t.Fatal(err)
	}

	return src
}
