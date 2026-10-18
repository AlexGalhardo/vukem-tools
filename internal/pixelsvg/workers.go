package pixelsvg

import (
	"sync"
	"sync/atomic"

	"github.com/AlexGalhardo/vukem-tools/internal/cpu"
)

// ctxCheckRows is how many rows a worker processes between cancellation
// checks. Checking every row would cost an atomic load per row for no benefit;
// 64 rows of a 2048-wide image is about 128k pixels, a sub-millisecond slice.
const ctxCheckRows = 64

// Workers reports the worker count a conversion uses when [Options.Workers] is
// zero: the CPUs this process may actually use, which internal/cpu derives
// from GOMAXPROCS and the container's own quota.
func Workers() int { return cpu.Limit() }

// band is a contiguous range of rows, y0 inclusive and y1 exclusive.
type band struct {
	y0, y1 int
}

// bandsFor splits h rows into at most workers contiguous bands of nearly equal
// height. There is never an empty band, so the number of bands is also the
// number of goroutines a band pass starts.
func bandsFor(h, workers int) []band {
	n := min(max(workers, 1), h)
	if n <= 0 {
		return nil
	}

	out := make([]band, 0, n)
	y := 0

	for i := range n {
		rows := (h - y) / (n - i)
		out = append(out, band{y0: y, y1: y + rows})
		y += rows
	}

	return out
}

// runBands runs fn once per band, concurrently, and returns when every band is
// done. A single band runs inline, so a one-worker conversion starts no
// goroutine at all.
func runBands(bands []band, fn func(i int, b band)) {
	if len(bands) == 1 {
		fn(0, bands[0])

		return
	}

	var wg sync.WaitGroup

	for i, b := range bands {
		wg.Go(func() { fn(i, b) })
	}

	wg.Wait()
}

// forEachIndex runs fn for every index below n, with at most workers
// goroutines sharing the range. fn must only touch the slot belonging to the
// index it is given, which is what keeps the result independent of the
// scheduling order.
func forEachIndex(n, workers int, fn func(i int)) {
	if workers <= 1 || n <= 1 {
		for i := range n {
			fn(i)
		}

		return
	}

	var (
		next atomic.Int32
		wg   sync.WaitGroup
	)

	for range min(workers, n) {
		wg.Go(func() {
			for {
				i := int(next.Add(1)) - 1
				if i >= n {
					return
				}

				fn(i)
			}
		})
	}

	wg.Wait()
}

// firstErr returns the first non-nil error of errs.
func firstErr(errs []error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}

	return nil
}
