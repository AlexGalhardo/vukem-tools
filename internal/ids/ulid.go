// Package ids generates ULIDs: 128-bit identifiers whose text form sorts by
// creation time.
//
// Conversion ids are handed to clients, appear in URLs and are the key of
// every progress event, so they must not be guessable and must not leak how
// many conversions the service has run. A sequential integer does both.
// A UUIDv4 does neither, but it also destroys index locality and makes
// "newest first" need a separate column. A ULID keeps the ordering in the
// key itself: the first 48 bits are a millisecond timestamp, the remaining
// 80 come from crypto/rand.
//
// There is deliberately no package-level generator: monotonicity needs
// state, and state at package level is state nobody owns. Callers hold a
// *Generator (bootstrap wires one into the job store), which also makes the
// clock injectable in tests.
package ids

import (
	"crypto/rand"
	"sync"
	"time"
)

// Crockford base32: no I, L, O or U, so an id cannot be misread and cannot
// accidentally spell a word.
const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// Length is the number of characters in the text form. 26 characters of 5
// bits hold 130, of which the top 2 are always zero.
const Length = 26

// randomBits is the entropy, in bytes, after the 48-bit timestamp.
const randomBits = 10

// decodeTable maps a byte to its base32 value, or to 0xFF when the byte is
// not part of the alphabet. Built once at package load from alphabet, so the
// two can never drift apart.
var decodeTable = func() [256]byte {
	var t [256]byte
	for i := range t {
		t[i] = 0xFF
	}

	for i := range len(alphabet) {
		t[alphabet[i]] = byte(i)
	}

	return t
}()

// Generator hands out ULIDs that are unique and strictly increasing, even
// for several ids made within the same millisecond. It is safe for
// concurrent use.
type Generator struct {
	mu     sync.Mutex
	lastMS uint64
	last   [randomBits]byte
	// seeded is false until the first id, so the zero millisecond is not
	// mistaken for "already generated at time 0".
	seeded bool
}

// NewGenerator returns a generator ready for use.
func NewGenerator() *Generator { return &Generator{} }

// New returns an id stamped with the current time.
func (g *Generator) New() string { return g.At(time.Now()) }

// At returns an id stamped with t.
//
// When t lands in a millisecond already used, the random part is incremented
// instead of redrawn, which keeps the ids strictly increasing. The same
// happens when t is before the last id: a clock that steps backwards must
// not produce an id that sorts before one already handed out.
func (g *Generator) At(t time.Time) string {
	ms := millis(t)

	g.mu.Lock()
	defer g.mu.Unlock()

	switch {
	case !g.seeded || ms > g.lastMS:
		g.lastMS, g.seeded = ms, true
		g.fill()
	case increment(&g.last):
		ms = g.lastMS
	default:
		// The 80-bit counter wrapped inside one millisecond, which takes
		// 2^80 ids. Moving to the next millisecond keeps the ordering.
		g.lastMS++
		ms = g.lastMS

		g.fill()
	}

	return encode(ms, g.last)
}

// millis is t as Unix milliseconds, clamped at the epoch: a negative
// timestamp would not fit the 48 unsigned bits the format gives it.
func millis(t time.Time) uint64 {
	if v := t.UnixMilli(); v > 0 {
		return uint64(v)
	}

	return 0
}

// fill draws a fresh random tail. crypto/rand.Read is documented never to
// fail as of Go 1.24: it terminates the program rather than return an error,
// so there is no error path to handle here.
func (g *Generator) fill() { _, _ = rand.Read(g.last[:]) }

// increment adds one to the big-endian counter in b and reports whether it
// did so without wrapping.
func increment(b *[randomBits]byte) bool {
	for i := len(b) - 1; i >= 0; i-- {
		b[i]++
		if b[i] != 0 {
			return true
		}
	}

	return false
}

// encode renders the 48-bit timestamp and the random tail as Length
// characters. The 128 bits are read as a 130-bit number with two zero bits
// in front, five bits per character, most significant first.
func encode(ms uint64, tail [randomBits]byte) string {
	hi := ms<<16 | uint64(tail[0])<<8 | uint64(tail[1])

	var lo uint64
	for _, b := range tail[2:] {
		lo = lo<<8 | uint64(b)
	}

	var out [Length]byte
	for i := range Length {
		out[i] = alphabet[bits5(hi, lo, uint(125-5*i))]
	}

	return string(out[:])
}

// bits5 returns the five bits of the 128-bit value (hi, lo) starting at
// shift. A shift of 125 reaches past the top, which yields the three real
// bits of the first character.
func bits5(hi, lo uint64, shift uint) uint64 {
	switch {
	case shift >= 64:
		return hi >> (shift - 64) & 31
	case shift <= 59:
		return lo >> shift & 31
	default:
		return (lo>>shift | hi<<(64-shift)) & 31
	}
}

// Valid reports whether s is a well-formed id. Use it before putting a
// client-supplied id in a query: a value that cannot be an id does not need
// a database round trip to be refused.
func Valid(s string) bool {
	_, ok := decode(s)
	return ok
}

// TimeOf returns the millisecond the id was stamped with, and whether s was
// a well-formed id. The time is advisory: it is the clock of whichever
// process made the id, not a verified fact.
func TimeOf(s string) (time.Time, bool) {
	hi, ok := decode(s)
	if !ok {
		return time.Time{}, false
	}

	return time.UnixMilli(int64(hi >> 16)).UTC(), true
}

// decode parses s and returns the high 64 bits of the 128-bit value, which
// hold the timestamp. The low half is read to validate every character but
// carries only entropy, so no caller needs it.
func decode(s string) (hi uint64, ok bool) {
	if len(s) != Length {
		return 0, false
	}

	var lo uint64

	for i := range Length {
		v := decodeTable[s[i]]
		if v == 0xFF {
			return 0, false
		}

		// The first character carries only three bits; a larger value would
		// not fit the 128 bits and so cannot have been produced by encode.
		if i == 0 && v > 7 {
			return 0, false
		}

		hi = hi<<5 | lo>>59
		lo = lo<<5 | uint64(v)
	}

	return hi, true
}
