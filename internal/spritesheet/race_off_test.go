//go:build !race

package spritesheet

// raceEnabled reports whether this test binary was built with the race
// detector. See the race-enabled counterpart for why the memory assertions
// care.
const raceEnabled = false
