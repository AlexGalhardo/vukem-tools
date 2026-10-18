package pixelsvg

import (
	"image/color"
	"reflect"
	"runtime"
	"slices"
	"testing"
)

// TestColorDistanceScale checks the metric is on the 0 to 255 scale it claims:
// an equal step on every channel is exactly that step.
func TestColorDistanceScale(t *testing.T) {
	cases := []struct {
		name string
		a, b color.NRGBA
		want int
	}{
		{"identical", color.NRGBA{R: 10, G: 20, B: 30, A: 0xff}, color.NRGBA{R: 10, G: 20, B: 30, A: 0xff}, 0},
		{"equal-step", color.NRGBA{A: 0xff}, color.NRGBA{R: 40, G: 40, B: 40, A: 0xff}, 40},
		{"black-to-white", color.NRGBA{A: 0xff}, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, 255},
		{"green-weighted", color.NRGBA{A: 0xff}, color.NRGBA{G: 30, A: 0xff}, 20},
		{"red-weighted", color.NRGBA{A: 0xff}, color.NRGBA{R: 30, A: 0xff}, 14},
		{"alpha-ignored", color.NRGBA{R: 10, A: 0xff}, color.NRGBA{R: 10}, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ColorDistance(tc.a, tc.b); got != tc.want {
				t.Errorf("ColorDistance = %d, want %d", got, tc.want)
			}

			if got := ColorDistance(tc.b, tc.a); got != tc.want {
				t.Errorf("ColorDistance is not symmetric: %d one way, %d the other", got, tc.want)
			}
		})
	}
}

// TestColorDistanceGreenDominates checks the weighting: the same numeric step
// in green has to read as a bigger change than in red or blue.
func TestColorDistanceGreenDominates(t *testing.T) {
	black := color.NRGBA{A: 0xff}
	step := 60

	green := ColorDistance(black, color.NRGBA{G: uint8(step), A: 0xff})
	blue := ColorDistance(black, color.NRGBA{B: uint8(step), A: 0xff})
	red := ColorDistance(black, color.NRGBA{R: uint8(step), A: 0xff})

	if green <= blue || blue <= red {
		t.Errorf("distances are green %d, blue %d, red %d; want green > blue > red", green, blue, red)
	}
}

// TestWithinToleranceRespectsAlpha checks a solid colour never merges into a
// near-transparent one with the same hue.
func TestWithinToleranceRespectsAlpha(t *testing.T) {
	solid := color.NRGBA{R: 0xe9, G: 0x45, B: 0x60, A: 0xff}
	ghost := color.NRGBA{R: 0xe9, G: 0x45, B: 0x60, A: 0x10}

	if withinTolerance(solid, ghost, 64) {
		t.Error("colours 239 apart in alpha merged at tolerance 64")
	}

	if !withinTolerance(solid, color.NRGBA{R: 0xe9, G: 0x45, B: 0x60, A: 0xf0}, 64) {
		t.Error("colours 15 apart in alpha did not merge at tolerance 64")
	}
}

// TestToleranceMergesNeighbours checks the tolerance pass collapses an
// anti-aliasing ramp into the shade that covers the most pixels, and leaves a
// genuinely different colour alone.
func TestToleranceMergesNeighbours(t *testing.T) {
	colors := []color.NRGBA{
		{R: 0x10, G: 0x10, B: 0x10, A: 0xff},
		{R: 0x12, G: 0x11, B: 0x10, A: 0xff},
		{R: 0x14, G: 0x12, B: 0x11, A: 0xff},
		{R: 0xf0, G: 0xf0, B: 0xf0, A: 0xff},
	}
	counts := []int{1, 100, 1, 50}

	rep := mergeTolerance(fullSubset(colors, counts), 8)

	for i := range 3 {
		if rep[i] != 1 {
			t.Errorf("colour %d is represented by %d, want 1 (the most used of the ramp)", i, rep[i])
		}
	}

	if rep[3] != 3 {
		t.Errorf("the distant colour is represented by %d, want itself", rep[3])
	}
}

// TestMedianCutKeepsSourceColors checks the quantiser hits the target and
// never invents a colour the image did not contain.
func TestMedianCutKeepsSourceColors(t *testing.T) {
	colors := testPalette(200)
	counts := make([]int, len(colors))

	for i := range counts {
		counts[i] = i + 1
	}

	for _, target := range []int{1, 2, 16, 64, 199, 200, 400} {
		rep := medianCut(fullSubset(colors, counts), target)

		seen := slices.Clone(rep)
		slices.Sort(seen)
		seen = slices.Compact(seen)

		if want := min(target, len(colors)); len(seen) > want {
			t.Errorf("target %d produced %d representatives", target, len(seen))
		}

		for _, id := range seen {
			if id < 0 || int(id) >= len(colors) {
				t.Fatalf("target %d produced representative %d, out of the palette", target, id)
			}
		}
	}
}

// TestMedianCutDeterministic checks repeated runs agree, which the whole
// determinism guarantee rests on.
func TestMedianCutDeterministic(t *testing.T) {
	colors := testPalette(256)
	counts := make([]int, len(colors))

	for i := range counts {
		counts[i] = (i*7919)%97 + 1
	}

	first := medianCut(fullSubset(colors, counts), 32)

	for range 5 {
		again := medianCut(fullSubset(colors, counts), 32)
		if !slices.Equal(first, again) {
			t.Fatal("median cut is not deterministic")
		}
	}
}

// TestMergeRuns checks the vertical merge on shapes whose answer can be
// written down by hand.
func TestMergeRuns(t *testing.T) {
	cases := []struct {
		name string
		runs []run
		want []rect
	}{
		{
			name: "single",
			runs: []run{{y: 0, x0: 0, x1: 1}},
			want: []rect{{x: 0, y: 0, w: 1, h: 1}},
		},
		{
			name: "column",
			runs: []run{{y: 0, x0: 2, x1: 3}, {y: 1, x0: 2, x1: 3}, {y: 2, x0: 2, x1: 3}},
			want: []rect{{x: 2, y: 0, w: 1, h: 3}},
		},
		{
			name: "block",
			runs: []run{{y: 0, x0: 0, x1: 4}, {y: 1, x0: 0, x1: 4}},
			want: []rect{{x: 0, y: 0, w: 4, h: 2}},
		},
		{
			name: "offset-breaks-merge",
			runs: []run{{y: 0, x0: 0, x1: 4}, {y: 1, x0: 1, x1: 5}},
			want: []rect{{x: 0, y: 0, w: 4, h: 1}, {x: 1, y: 1, w: 4, h: 1}},
		},
		{
			name: "gap-row-breaks-merge",
			runs: []run{{y: 0, x0: 0, x1: 2}, {y: 2, x0: 0, x1: 2}},
			want: []rect{{x: 0, y: 0, w: 2, h: 1}, {x: 0, y: 2, w: 2, h: 1}},
		},
		{
			name: "two-columns-one-row",
			runs: []run{
				{y: 0, x0: 0, x1: 1},
				{y: 0, x0: 3, x1: 4},
				{y: 1, x0: 0, x1: 1},
				{y: 1, x0: 3, x1: 4},
			},
			want: []rect{{x: 0, y: 0, w: 1, h: 2}, {x: 3, y: 0, w: 1, h: 2}},
		},
		{
			name: "staircase",
			runs: []run{
				{y: 0, x0: 0, x1: 3},
				{y: 1, x0: 0, x1: 3},
				{y: 1, x0: 5, x1: 6},
				{y: 2, x0: 5, x1: 6},
			},
			want: []rect{
				{x: 0, y: 0, w: 3, h: 2},
				{x: 5, y: 1, w: 1, h: 2},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mergeRuns(tc.runs)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("mergeRuns = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestMergeRunsCoversExactly checks the merge is lossless on a generated grid:
// the rectangles cover every pixel of the runs once and nothing else.
func TestMergeRunsCoversExactly(t *testing.T) {
	src := generatedImage(61, 43, 6, 3)

	g, err := newGrid(t.Context(), src, mustNormalize(t, Options{Workers: 2}))
	if err != nil {
		t.Fatalf("grid: %v", err)
	}

	runs, err := extractRuns(t.Context(), g, mustNormalize(t, Options{Workers: 2}))
	if err != nil {
		t.Fatalf("runs: %v", err)
	}

	for ci := range g.pal {
		fromRuns := make(map[point]bool)

		for _, r := range runs[ci] {
			for x := r.x0; x < r.x1; x++ {
				fromRuns[point{x: x, y: r.y}] = true
			}
		}

		fromRects := make(map[point]bool)

		for _, r := range mergeRuns(runs[ci]) {
			for y := r.y; y < r.y+r.h; y++ {
				for x := r.x; x < r.x+r.w; x++ {
					p := point{x: x, y: y}
					if fromRects[p] {
						t.Fatalf("colour %d covers (%d,%d) twice", ci, x, y)
					}

					fromRects[p] = true
				}
			}
		}

		if !reflect.DeepEqual(fromRuns, fromRects) {
			t.Errorf("colour %d: rectangles cover %d pixels, runs cover %d",
				ci, len(fromRects), len(fromRuns))
		}
	}
}

// mustNormalize applies the Options defaults, failing the test if they are
// rejected.
func mustNormalize(t *testing.T, opts Options) Options {
	t.Helper()

	eff, err := opts.normalize()
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}

	return eff
}

// TestLabelRuns checks 4-connected labelling, including that a diagonal touch
// is not a connection.
func TestLabelRuns(t *testing.T) {
	cases := []struct {
		name string
		runs []run
		want []int
	}{
		{
			name: "overlap-joins",
			runs: []run{{y: 0, x0: 0, x1: 3}, {y: 1, x0: 2, x1: 5}},
			want: []int{0, 0},
		},
		{
			name: "diagonal-does-not-join",
			runs: []run{{y: 0, x0: 0, x1: 1}, {y: 1, x0: 1, x1: 2}},
			want: []int{0, 1},
		},
		{
			name: "gap-row-does-not-join",
			runs: []run{{y: 0, x0: 0, x1: 2}, {y: 2, x0: 0, x1: 2}},
			want: []int{0, 1},
		},
		{
			name: "u-shape-joins-through-the-bottom",
			runs: []run{
				{y: 0, x0: 0, x1: 1},
				{y: 0, x0: 3, x1: 4},
				{y: 1, x0: 0, x1: 4},
			},
			want: []int{0, 0, 0},
		},
		{
			name: "two-separate-columns",
			runs: []run{
				{y: 0, x0: 0, x1: 1},
				{y: 0, x0: 3, x1: 4},
				{y: 1, x0: 0, x1: 1},
				{y: 1, x0: 3, x1: 4},
			},
			want: []int{0, 1, 0, 1},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, count := labelRuns(tc.runs)
			if !slices.Equal(got, tc.want) {
				t.Errorf("labelRuns = %v, want %v", got, tc.want)
			}

			if want := len(slices.Compact(slices.Sorted(slices.Values(tc.want)))); count != want {
				t.Errorf("region count = %d, want %d", count, want)
			}
		})
	}
}

// TestCollapseCollinear checks the exact reduction of a straight edge,
// including across the point where the loop closes.
func TestCollapseCollinear(t *testing.T) {
	loop := []point{
		{0, 0},
		{1, 0},
		{2, 0},
		{3, 0},
		{3, 1},
		{2, 1},
		{1, 1},
		{0, 1},
	}

	got := collapseCollinear(loop)
	want := []point{{0, 0}, {3, 0}, {3, 1}, {0, 1}}

	if !slices.Equal(got, want) {
		t.Errorf("collapseCollinear = %v, want %v", got, want)
	}

	if a, b := area2(loop), area2(got); a != b {
		t.Errorf("collapsing changed the area from %d to %d", a, b)
	}
}

// TestDouglasPeuckerKeepsEndsAndCorners checks the simplifier drops what sits
// on a chord and keeps what does not.
func TestDouglasPeuckerKeepsEndsAndCorners(t *testing.T) {
	line := []point{{0, 0}, {1, 0}, {2, 0}, {3, 0}, {4, 0}}
	if got := douglasPeucker(line, 0.5); !slices.Equal(got, []point{{0, 0}, {4, 0}}) {
		t.Errorf("a straight line simplified to %v, want its two ends", got)
	}

	spike := []point{{0, 0}, {2, 5}, {4, 0}}
	if got := douglasPeucker(spike, 0.5); !slices.Equal(got, spike) {
		t.Errorf("a 5-pixel spike simplified to %v at epsilon 0.5", got)
	}

	if got := douglasPeucker(spike, 6); !slices.Equal(got, []point{{0, 0}, {4, 0}}) {
		t.Errorf("a 5-pixel spike survived epsilon 6 as %v", got)
	}
}

// TestBandsFor checks the row split: every row covered exactly once, never an
// empty band, never more bands than workers.
func TestBandsFor(t *testing.T) {
	for _, h := range []int{1, 2, 3, 7, 8, 100, 2048} {
		for _, workers := range []int{1, 2, 3, 5, 8, 64} {
			bands := bandsFor(h, workers)

			if len(bands) > workers || len(bands) > h {
				t.Fatalf("h=%d workers=%d gave %d bands", h, workers, len(bands))
			}

			at := 0
			for _, b := range bands {
				if b.y0 != at {
					t.Fatalf("h=%d workers=%d: band starts at %d, want %d", h, workers, b.y0, at)
				}

				if b.y1 <= b.y0 {
					t.Fatalf("h=%d workers=%d: empty band %+v", h, workers, b)
				}

				at = b.y1
			}

			if at != h {
				t.Fatalf("h=%d workers=%d: bands cover %d rows", h, workers, at)
			}
		}
	}
}

// TestParseCPUMax checks the cgroup v2 parser, including the unlimited form.
func TestParseCPUMax(t *testing.T) {
	cases := []struct {
		in   string
		want int
		ok   bool
	}{
		{"max 100000\n", 0, false},
		{"100000 100000\n", 1, true},
		{"200000 100000", 2, true},
		{"50000 100000\n", 1, true},
		{"250000 100000\n", 3, true},
		{"", 0, false},
		{"garbage", 0, false},
		{"0 100000", 0, false},
		{"100000 0", 0, false},
	}

	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, ok := parseCPUMax(tc.in)
			if ok != tc.ok || got != tc.want {
				t.Errorf("parseCPUMax(%q) = %d, %v; want %d, %v", tc.in, got, ok, tc.want, tc.ok)
			}
		})
	}
}

// TestWorkersBounded checks the worker count never exceeds GOMAXPROCS and is
// never below one, whatever the host reports.
func TestWorkersBounded(t *testing.T) {
	got := Workers()

	if got < 1 {
		t.Errorf("Workers = %d, want at least 1", got)
	}

	if got > runtime.GOMAXPROCS(0) {
		t.Errorf("Workers = %d, over GOMAXPROCS %d", got, runtime.GOMAXPROCS(0))
	}
}

// TestPaletteOrder checks the palette is sorted by R, G, B then A, which is
// the order the colour groups come out in.
func TestPaletteOrder(t *testing.T) {
	g, err := newGrid(t.Context(), spriteImage(t), mustNormalize(t, Options{}))
	if err != nil {
		t.Fatalf("grid: %v", err)
	}

	want := []color.NRGBA{
		spritePalette['K'],
		spritePalette['H'],
		spritePalette['R'],
		spritePalette['Y'],
	}

	if !slices.Equal(g.pal, want) {
		t.Errorf("palette = %v, want %v", g.pal, want)
	}

	total := 0
	for _, n := range g.counts {
		total += n
	}

	if total != 48 {
		t.Errorf("palette counts total %d pixels, want 48", total)
	}
}
