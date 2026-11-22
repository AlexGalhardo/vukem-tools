package cli

import (
	"flag"
	"testing"

	"github.com/AlexGalhardo/vukem-tools/internal/toolkit"
)

// An option named like a flag every tool has would be shadowed. This pins
// that the flags defined for a tool match its options, so a collision shows
// up as a missing flag here rather than as a wrong call.
func TestEveryScalarOptionGetsAFlag(t *testing.T) {
	t.Parallel()

	for _, tool := range toolkit.All() {
		fs := flag.NewFlagSet(tool.Name, flag.ContinueOnError)
		flags := optionFlags(fs, tool)

		defined := 0

		fs.VisitAll(func(*flag.Flag) { defined++ })

		if defined != len(flags) {
			t.Errorf("%s: %d flags defined for %d options", tool.Name, defined, len(flags))
		}

		for _, f := range flags {
			for _, r := range reserved {
				if f.name == r {
					t.Errorf("%s: option %q collides with the reserved flag --%s", tool.Name, f.name, r)
				}
			}
		}
	}
}
