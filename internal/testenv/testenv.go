// Package testenv is what tests that need an external engine call when it is
// missing.
//
// On a laptop `go test ./...` skips them, which keeps the suite runnable
// without the production image. That also means a green run says nothing
// about the engines, so REQUIRE_ENGINES=1 turns each of those skips into a
// failure: CI sets it where the engines are installed, and so does anyone who
// wants the answer to "did the engine tests actually run".
package testenv

import "os"

// EnvVar is the switch. Any value other than "1" leaves skipping on.
const EnvVar = "REQUIRE_ENGINES"

// T is the part of testing.TB the helpers use, so a test can fake it.
type T interface {
	Helper()
	Skipf(format string, args ...any)
	Fatalf(format string, args ...any)
}

// Required reports whether a missing engine must fail the run.
func Required() bool { return os.Getenv(EnvVar) == "1" }

// MissingEngine skips the test because an engine is not installed, or fails
// it when REQUIRE_ENGINES=1.
func MissingEngine(t T, reason string) {
	t.Helper()

	MissingEnginef(t, "%s", reason)
}

// MissingEnginef is MissingEngine with a formatted reason.
func MissingEnginef(t T, format string, args ...any) {
	t.Helper()

	if Required() {
		t.Fatalf("an engine is missing and "+EnvVar+"=1: "+format, args...)

		return
	}

	t.Skipf(format, args...)
}
