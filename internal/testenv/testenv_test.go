package testenv_test

import (
	"fmt"
	"testing"

	"github.com/AlexGalhardo/vukem-tools/internal/testenv"
)

type fake struct{ skipped, failed string }

func (*fake) Helper() {}

func (f *fake) Skipf(format string, args ...any) { f.skipped = fmt.Sprintf(format, args...) }

func (f *fake) Fatalf(format string, args ...any) { f.failed = fmt.Sprintf(format, args...) }

func TestAMissingEngineSkipsByDefault(t *testing.T) {
	t.Setenv(testenv.EnvVar, "")

	f := &fake{}
	testenv.MissingEngine(f, "ffmpeg is not installed")

	if f.skipped != "ffmpeg is not installed" || f.failed != "" {
		t.Errorf("skipped %q, failed %q", f.skipped, f.failed)
	}
}

func TestAMissingEngineFailsWhenRequired(t *testing.T) {
	t.Setenv(testenv.EnvVar, "1")

	f := &fake{}
	testenv.MissingEnginef(f, "%s is not installed", "ffmpeg")

	if f.skipped != "" || f.failed == "" {
		t.Errorf("skipped %q, failed %q: REQUIRE_ENGINES=1 must fail", f.skipped, f.failed)
	}
}

func TestOnlyOneEnablesIt(t *testing.T) {
	for value, want := range map[string]bool{"1": true, "": false, "0": false, "true": false} {
		t.Setenv(testenv.EnvVar, value)

		if got := testenv.Required(); got != want {
			t.Errorf("REQUIRE_ENGINES=%q: Required() = %v, want %v", value, got, want)
		}
	}
}
