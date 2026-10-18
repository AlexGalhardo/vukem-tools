package pixelsvg

import (
	"cmp"
	"image/color"
	"slices"
)

// channel names the four components median cut may split on.
type channel int

// The channels, in the order ties between equal ranges are broken.
const (
	channelR channel = iota
	channelG
	channelB
	channelA
)

// value returns the component of c named by ch.
func (ch channel) value(c color.NRGBA) uint8 {
	switch ch {
	case channelR:
		return c.R
	case channelG:
		return c.G
	case channelB:
		return c.B
	case channelA:
		return c.A
	default:
		return c.R
	}
}

// cutBox is a half-open range of the working order, together with the number
// of pixels the colours in it cover and the widest channel spread inside it.
// The spread is cached because it is what decides whether the box can be cut
// at all, and recomputing it for every box on every round would make the
// quantiser quadratic in the palette size.
type cutBox struct {
	lo, hi int
	count  int
	wide   int
}

// medianCut reduces s to at most target representatives and returns, for every
// entry of s, the original palette index of the entry that represents it.
//
// The partition is textbook median cut: the box covering the most pixels is
// split along its widest channel at the weighted median, until there are
// target boxes or nothing left to split. The representative of a box is not
// its average but the colour in it that covers the most pixels, so the output
// palette is always a subset of the input palette. That matters for pixel art,
// where an averaged colour is a colour the artist never chose, and it also
// keeps the result exactly reproducible without any floating point.
func medianCut(s subset, target int) []int32 {
	n := len(s.ids)

	rep := make([]int32, n)
	if n <= target || target < 1 {
		for i := range rep {
			rep[i] = s.ids[i]
		}

		return rep
	}

	order := make([]int, n)
	total := 0

	for i := range order {
		order[i] = i
		total += s.counts[i]
	}

	boxes := make([]cutBox, 0, target)
	boxes = append(boxes, cutBox{lo: 0, hi: n, count: total, wide: widestRange(s, order)})

	for len(boxes) < target {
		bi := pickBox(boxes)
		if bi < 0 {
			break
		}

		b := boxes[bi]
		ch := widestChannel(s, order[b.lo:b.hi])
		sortBox(s, order[b.lo:b.hi], ch)

		cut := medianSplit(s, order[b.lo:b.hi], b.count)
		if cut <= 0 || cut >= b.hi-b.lo {
			// Unsplittable in practice; take it out of contention.
			boxes[bi].wide = 0

			continue
		}

		lo, hi := order[b.lo:b.lo+cut], order[b.lo+cut:b.hi]
		left := cutBox{lo: b.lo, hi: b.lo + cut, count: countOf(s, lo), wide: widestRange(s, lo)}
		boxes[bi] = left
		boxes = append(boxes, cutBox{
			lo:    b.lo + cut,
			hi:    b.hi,
			count: b.count - left.count,
			wide:  widestRange(s, hi),
		})
	}

	for _, b := range boxes {
		r := modeOf(s, order[b.lo:b.hi])
		for _, p := range order[b.lo:b.hi] {
			rep[p] = s.ids[r]
		}
	}

	return rep
}

// pickBox returns the index of the box to split next: the one covering the
// most pixels that still holds more than one colour. Ties go to the box that
// starts earliest, so the choice never depends on the order boxes were
// appended in.
func pickBox(boxes []cutBox) int {
	best, bestCount := -1, 0

	for i, b := range boxes {
		if b.hi-b.lo < 2 || b.wide == 0 {
			continue
		}

		if best < 0 || b.count > bestCount || (b.count == bestCount && b.lo < boxes[best].lo) {
			best, bestCount = i, b.count
		}
	}

	return best
}

// widestChannel returns the channel with the greatest spread over the box.
func widestChannel(s subset, box []int) channel {
	best, bestSpread := channelR, -1

	for _, ch := range []channel{channelR, channelG, channelB, channelA} {
		lo, hi := uint8(0xff), uint8(0)

		for _, p := range box {
			v := ch.value(s.colors[p])
			lo = min(lo, v)
			hi = max(hi, v)
		}

		if spread := int(hi) - int(lo); spread > bestSpread {
			best, bestSpread = ch, spread
		}
	}

	return best
}

// widestRange returns the greatest spread of any channel over the box, so a
// box of identical colours can be told apart from one that can still be cut.
func widestRange(s subset, box []int) int {
	widest := 0

	for _, ch := range []channel{channelR, channelG, channelB, channelA} {
		lo, hi := uint8(0xff), uint8(0)

		for _, p := range box {
			v := ch.value(s.colors[p])
			lo = min(lo, v)
			hi = max(hi, v)
		}

		widest = max(widest, int(hi)-int(lo))
	}

	return widest
}

// sortBox orders the box by the given channel, breaking ties by the palette
// order so the arrangement is unique.
func sortBox(s subset, box []int, ch channel) {
	slices.SortFunc(box, func(a, b int) int {
		if c := cmp.Compare(ch.value(s.colors[a]), ch.value(s.colors[b])); c != 0 {
			return c
		}

		return compareNRGBA(s.colors[a], s.colors[b])
	})
}

// medianSplit returns the number of leading entries of the sorted box that
// together cover about half of its pixels, clamped so both halves are
// non-empty.
func medianSplit(s subset, box []int, total int) int {
	half := total / 2
	acc := 0

	for i, p := range box {
		acc += s.counts[p]
		if acc > half {
			return min(max(i+1, 1), len(box)-1)
		}
	}

	return len(box) - 1
}

// countOf returns the number of pixels the colours in the box cover.
func countOf(s subset, box []int) int {
	n := 0
	for _, p := range box {
		n += s.counts[p]
	}

	return n
}

// modeOf returns the entry of the box covering the most pixels, breaking ties
// by the palette order.
func modeOf(s subset, box []int) int {
	best := box[0]

	for _, p := range box[1:] {
		switch {
		case s.counts[p] > s.counts[best]:
			best = p
		case s.counts[p] == s.counts[best] && compareNRGBA(s.colors[p], s.colors[best]) < 0:
			best = p
		}
	}

	return best
}
