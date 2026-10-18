package pixelsvg

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// The cgroup files that expose the CPU quota of a container.
const (
	cgroupV2CPUMax    = "/sys/fs/cgroup/cpu.max"
	cgroupV1CPUQuota  = "/sys/fs/cgroup/cpu/cpu.cfs_quota_us"
	cgroupV1CPUPeriod = "/sys/fs/cgroup/cpu/cpu.cfs_period_us"
)

// ctxCheckRows is how many rows a worker processes between cancellation
// checks. Checking every row would cost an atomic load per row for no benefit;
// 64 rows of a 2048-wide image is about 128k pixels, a sub-millisecond slice.
const ctxCheckRows = 64

// Workers reports the worker count a conversion uses when [Options.Workers] is
// zero.
//
// It is the smaller of GOMAXPROCS and the container's own CPU quota, and never
// below one. Go 1.25 and later already derive GOMAXPROCS from the cgroup CPU
// limit on Linux, but the quota is read here as well: the value is what bounds
// this package's goroutines even when GOMAXPROCS has been raised by hand or
// the program runs on a Go release that does not do it.
func Workers() int {
	n := runtime.GOMAXPROCS(0)
	if q := cpuQuota(); q > 0 && q < n {
		n = q
	}

	return max(n, 1)
}

// cpuQuota returns the container's CPU limit rounded up to whole CPUs, or zero
// when there is no quota or it cannot be read.
func cpuQuota() int {
	if b, err := os.ReadFile(cgroupV2CPUMax); err == nil {
		if n, ok := parseCPUMax(string(b)); ok {
			return n
		}
	}

	quota, err := os.ReadFile(cgroupV1CPUQuota)
	if err != nil {
		return 0
	}

	period, err := os.ReadFile(cgroupV1CPUPeriod)
	if err != nil {
		return 0
	}

	if n, ok := parseCPUQuotaPeriod(string(quota), string(period)); ok {
		return n
	}

	return 0
}

// parseCPUMax reads a cgroup v2 cpu.max value, which is either "max <period>"
// for no limit or "<quota> <period>" microseconds.
func parseCPUMax(s string) (int, bool) {
	quota, period, ok := strings.Cut(strings.TrimSpace(s), " ")
	if !ok {
		return 0, false
	}

	return parseCPUQuotaPeriod(quota, period)
}

// parseCPUQuotaPeriod turns a quota and a period in microseconds into a whole
// number of CPUs, rounded up. It reports false when there is no limit.
func parseCPUQuotaPeriod(quota, period string) (int, bool) {
	q, err := strconv.Atoi(strings.TrimSpace(quota))
	if err != nil || q <= 0 {
		return 0, false
	}

	p, err := strconv.Atoi(strings.TrimSpace(period))
	if err != nil || p <= 0 {
		return 0, false
	}

	return max((q+p-1)/p, 1), true
}

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
