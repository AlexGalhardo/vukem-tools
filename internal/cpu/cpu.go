// Package cpu reports how many CPUs this process may actually use.
//
// Every concurrency bound in the app is sized from this, not from
// runtime.NumCPU. The app runs in a container with a CPU quota, and the host
// underneath it has far more cores than the quota allows: sizing a worker pool
// from the host's core count buys no throughput and spends the whole quota on
// context switches, which shows up as every conversion getting slower at once.
package cpu

import (
	"os"
	"runtime"
	"strconv"
	"strings"
)

// The cgroup files that expose the CPU quota of a container.
const (
	cgroupV2CPUMax    = "/sys/fs/cgroup/cpu.max"
	cgroupV1CPUQuota  = "/sys/fs/cgroup/cpu/cpu.cfs_quota_us"
	cgroupV1CPUPeriod = "/sys/fs/cgroup/cpu/cpu.cfs_period_us"
)

// Limit reports the number of CPUs available to this process: the smaller of
// GOMAXPROCS and the container's quota, and never below one.
//
// Go 1.25 and later already derive GOMAXPROCS from the cgroup CPU limit on
// Linux. The quota is still read here, because GOMAXPROCS can be raised by
// hand through the environment and the value returned here is what bounds the
// app's own pools either way.
func Limit() int {
	n := runtime.GOMAXPROCS(0)
	if q := Quota(); q > 0 && q < n {
		n = q
	}

	return max(n, 1)
}

// Quota returns the container's CPU limit rounded up to whole CPUs, or zero
// when there is no quota or it cannot be read. Zero is the normal answer
// outside a container, and on Windows and macOS.
func Quota() int {
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
//
// Rounding up rather than down matters at the small end: a 0.5-CPU container
// would otherwise get a limit of zero, which reads as "no limit" and would
// uncap every pool in the app.
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
