package ids_test

import (
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AlexGalhardo/vukem-tools/internal/ids"
)

var crockford = regexp.MustCompile(`^[0-9ABCDEFGHJKMNPQRSTVWXYZ]{26}$`)

func TestNewShape(t *testing.T) {
	t.Parallel()

	id := ids.NewGenerator().New()
	if !crockford.MatchString(id) {
		t.Fatalf("New() = %q, want 26 Crockford base32 characters", id)
	}
}

func TestAtIsSortableByTime(t *testing.T) {
	t.Parallel()

	g := ids.NewGenerator()
	base := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

	var got []string
	for i := range 5 {
		got = append(got, g.At(base.Add(time.Duration(i)*time.Second)))
	}

	want := slices.Clone(got)
	slices.Sort(want)

	if !slices.Equal(got, want) {
		t.Fatalf("ids from increasing times are not lexicographically sorted:\n%v", got)
	}
}

// Two ids made in the same millisecond must still differ and must still sort
// in creation order: a list ordered by id would otherwise shuffle rows
// created in the same tick.
func TestAtSameMillisecondIsMonotonic(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	g := ids.NewGenerator()

	var got []string
	for range 100 {
		got = append(got, g.At(at))
	}

	for i := 1; i < len(got); i++ {
		if got[i] <= got[i-1] {
			t.Fatalf("id %d (%q) does not sort after %q", i, got[i], got[i-1])
		}
	}
}

// A clock that steps backwards (NTP correction, a container resuming) must
// not produce an id that sorts before one already handed out.
func TestAtKeepsOrderWhenTheClockGoesBackwards(t *testing.T) {
	t.Parallel()

	g := ids.NewGenerator()
	at := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

	first := g.At(at)
	second := g.At(at.Add(-time.Hour))

	if second <= first {
		t.Fatalf("after the clock went back, %q does not sort after %q", second, first)
	}
}

func TestNewIsUnique(t *testing.T) {
	t.Parallel()

	const n = 10_000

	g := ids.NewGenerator()
	seen := make(map[string]struct{}, n)

	for range n {
		id := g.New()
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate id %q", id)
		}

		seen[id] = struct{}{}
	}
}

func TestNewIsSafeForConcurrentUse(t *testing.T) {
	t.Parallel()

	const workers, each = 8, 500

	var (
		mu  sync.Mutex
		all = make(map[string]struct{}, workers*each)
		wg  sync.WaitGroup
	)

	g := ids.NewGenerator()

	for range workers {
		wg.Go(func() {
			local := make([]string, 0, each)
			for range each {
				local = append(local, g.New())
			}

			mu.Lock()
			defer mu.Unlock()

			for _, id := range local {
				all[id] = struct{}{}
			}
		})
	}

	wg.Wait()

	if len(all) != workers*each {
		t.Fatalf("got %d unique ids, want %d", len(all), workers*each)
	}
}

func TestValid(t *testing.T) {
	t.Parallel()

	g := ids.NewGenerator()
	generated := g.New()

	tests := map[string]struct {
		in   string
		want bool
	}{
		"generated":           {generated, true},
		"empty":               {"", false},
		"too short":           {strings.Repeat("0", 25), false},
		"too long":            {strings.Repeat("0", 27), false},
		"letter I":            {strings.Repeat("0", 25) + "I", false},
		"letter L":            {strings.Repeat("0", 25) + "L", false},
		"letter O":            {strings.Repeat("0", 25) + "O", false},
		"letter U":            {strings.Repeat("0", 25) + "U", false},
		"lower case":          {strings.ToLower(generated), false},
		"overflows 128 bits":  {"8" + strings.Repeat("0", 25), false},
		"sql injection shape": {"' OR 1=1 --              ", false},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := ids.Valid(tc.in); got != tc.want {
				t.Fatalf("Valid(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestTimeOf(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 10, 18, 12, 34, 56, 789_000_000, time.UTC)

	got, ok := ids.TimeOf(ids.NewGenerator().At(at))
	if !ok {
		t.Fatal("TimeOf reported an invalid id")
	}

	if !got.Equal(at) {
		t.Fatalf("TimeOf = %s, want %s", got, at)
	}
}

func TestTimeOfRejectsAnInvalidID(t *testing.T) {
	t.Parallel()

	if _, ok := ids.TimeOf("not-an-id"); ok {
		t.Fatal("TimeOf accepted an invalid id")
	}
}
