package pixelsvg

import (
	"cmp"
	"image/color"
	"math"
	"slices"
)

// ColorDistance returns the distance between two colours on the same 0 to 255
// scale as [Options.Tolerance].
//
// The metric is the weighted Euclidean distance in sRGB with the classic
// 2:4:3 coefficients for red, green and blue, normalised by their sum:
//
//	d = sqrt((2*dR² + 4*dG² + 3*dB²) / 9)
//
// Green carries the largest weight because human luminance sensitivity peaks
// there, so an equal step in green reads as a bigger change than the same step
// in red or blue. The division by nine makes the result comparable with a
// single-channel difference: two colours that differ by n on every channel are
// exactly n apart, which is what makes a tolerance expressed in 0 to 255 mean
// something to a caller.
//
// CIELAB with ΔE2000 would be more faithful, and it was rejected on purpose:
// it needs a gamma decode, a matrix multiply and a cube root per comparison,
// and the tolerance pass runs it over the whole palette. For the small
// distances that decide whether two pixel-art colours are "the same colour
// with a different anti-aliasing step", the weighted sRGB metric orders pairs
// the same way at a fraction of the cost.
//
// Alpha is deliberately left out of the distance. It is compared separately,
// and two colours only merge when their alpha difference is within the
// tolerance as well, so a solid colour can never be merged into a ghost of
// itself.
func ColorDistance(a, b color.NRGBA) int {
	return int(math.Sqrt(float64(weightedSquare(a, b)) / 9))
}

// weightedSquare returns the squared weighted distance, scaled by nine so it
// stays in integers. It is nine times the square of [ColorDistance].
func weightedSquare(a, b color.NRGBA) int {
	dr := int(a.R) - int(b.R)
	dg := int(a.G) - int(b.G)
	db := int(a.B) - int(b.B)

	return 2*dr*dr + 4*dg*dg + 3*db*db
}

// alphaDistance returns the absolute alpha difference.
func alphaDistance(a, b color.NRGBA) int {
	d := int(a.A) - int(b.A)
	if d < 0 {
		return -d
	}

	return d
}

// withinTolerance reports whether a and b are close enough to merge.
func withinTolerance(a, b color.NRGBA, tolerance int) bool {
	if alphaDistance(a, b) > tolerance {
		return false
	}

	return weightedSquare(a, b) <= 9*tolerance*tolerance
}

// mergeTolerance merges every entry of s into a near-identical neighbour and
// returns, for every entry, the original palette index of the entry that
// represents it.
//
// Representatives are chosen greedily from the most used colour down, each one
// accepted only when it is farther than the tolerance from every
// representative already accepted; every remaining colour then joins its
// nearest representative within the tolerance, or keeps itself when none is
// close enough. Choosing the nearest rather than the first match avoids the
// artefact of a colour landing in a cluster it is barely inside while a better
// one sits next to it.
//
// At most [ColorCap] representatives are considered, which bounds the pass at
// O(len(s) * ColorCap) distance evaluations however large the palette is.
func mergeTolerance(s subset, tolerance int) []int32 {
	n := len(s.ids)
	rep := make([]int32, n)

	for i := range rep {
		rep[i] = -1
	}

	order := make([]int, n)
	for i := range order {
		order[i] = i
	}

	slices.SortFunc(order, func(a, b int) int {
		if c := cmp.Compare(s.counts[b], s.counts[a]); c != 0 {
			return c
		}

		return compareNRGBA(s.colors[a], s.colors[b])
	})

	reps := make([]int, 0, min(n, ColorCap))

	for _, p := range order {
		if len(reps) == ColorCap {
			break
		}

		apart := true

		for _, r := range reps {
			if withinTolerance(s.colors[p], s.colors[r], tolerance) {
				apart = false

				break
			}
		}

		if apart {
			reps = append(reps, p)
			rep[p] = s.ids[p]
		}
	}

	for _, p := range order {
		if rep[p] >= 0 {
			continue
		}

		best, bestDist := -1, 0

		for _, r := range reps {
			if !withinTolerance(s.colors[p], s.colors[r], tolerance) {
				continue
			}

			d := weightedSquare(s.colors[p], s.colors[r])
			switch {
			case best < 0, d < bestDist:
				best, bestDist = r, d
			case d == bestDist && compareNRGBA(s.colors[r], s.colors[best]) < 0:
				best = r
			}
		}

		if best < 0 {
			rep[p] = s.ids[p]
		} else {
			rep[p] = s.ids[best]
		}
	}

	return rep
}
