//go:build race

package spritesheet

// raceEnabled reports whether this test binary was built with the race
// detector. The memory assertions read it because the detector keeps shadow
// state for every allocation, which inflates the live heap well beyond what
// the same code uses in production; the figure recorded in docs/benchmarks.md
// comes from a build without it.
const raceEnabled = true
