package cpu

import (
	"runtime"
	"testing"
)

// The cgroup v2 parser, including the unlimited form. These cases moved here
// with the code they test; they were written for internal/pixelsvg, which now
// asks this package.
func TestParseCPUMax(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		in   string
		want int
		ok   bool
	}{
		"two cpus":          {"200000 100000", 2, true},
		"one cpu":           {"100000 100000", 1, true},
		"half a cpu":        {"50000 100000", 1, true},
		"one and a half":    {"150000 100000", 2, true},
		"trailing newline":  {"400000 100000\n", 4, true},
		"no limit":          {"max 100000", 0, false},
		"no separator":      {"100000", 0, false},
		"empty":             {"", 0, false},
		"zero quota":        {"0 100000", 0, false},
		"zero period":       {"100000 0", 0, false},
		"negative quota":    {"-1 100000", 0, false},
		"not a number":      {"lots 100000", 0, false},
		"period not number": {"100000 often", 0, false},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, ok := parseCPUMax(tc.in)
			if got != tc.want || ok != tc.ok {
				t.Errorf("parseCPUMax(%q) = %d, %v; want %d, %v", tc.in, got, ok, tc.want, tc.ok)
			}
		})
	}
}

// Rounding up is what stops a fractional-CPU container reading as unlimited.
// Half a CPU must be one, not zero.
func TestParseCPUQuotaPeriodRoundsUp(t *testing.T) {
	t.Parallel()

	if got, ok := parseCPUQuotaPeriod("10000", "100000"); got != 1 || !ok {
		t.Fatalf("a tenth of a CPU = %d, %v; want 1, true", got, ok)
	}
}

// Limit must be usable as a pool size on any platform: at least one, and
// never above GOMAXPROCS.
func TestLimitIsBounded(t *testing.T) {
	t.Parallel()

	got := Limit()
	if got < 1 {
		t.Errorf("Limit = %d, want at least 1", got)
	}

	if procs := runtime.GOMAXPROCS(0); got > procs {
		t.Errorf("Limit = %d, over GOMAXPROCS %d", got, procs)
	}
}

// Outside a container there is no quota, and zero must mean "no limit"
// rather than "no CPUs": Limit falls back to GOMAXPROCS.
func TestQuotaIsNeverNegative(t *testing.T) {
	t.Parallel()

	if got := Quota(); got < 0 {
		t.Errorf("Quota = %d, want zero or more", got)
	}
}
