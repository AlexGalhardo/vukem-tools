package ffmpeg

import (
	"strconv"
	"testing"
)

// QA-C7: on a host with many cores and no CPU quota, x264 at 1080p asked for
// more address space than the engine's RLIMIT_AS allows ("x264 [error]:
// malloc of size 7088704 failed") and every 1080p compression failed. Measured
// in the image under a 3 GiB limit: 16 threads fail, 8 pass.
func TestThreadCountsAreCappedOnManyCoreHosts(t *testing.T) {
	t.Parallel()

	for _, cores := range []int{1, 2, MaxThreads, 16, 64} {
		if got := threadsFor(cores); got < 1 || got > MaxThreads || got > cores {
			t.Errorf("threadsFor(%d) = %d, want 1..%d and no more than the cores", cores, got, MaxThreads)
		}
	}

	for _, args := range [][]string{Base(), Threads()} {
		for i, a := range args {
			if a != "-threads" && a != "-filter_threads" && a != "-filter_complex_threads" {
				continue
			}

			if n, err := strconv.Atoi(args[i+1]); err != nil || n > MaxThreads {
				t.Errorf("%s %s: want at most %d (%v)", a, args[i+1], MaxThreads, err)
			}
		}
	}
}
