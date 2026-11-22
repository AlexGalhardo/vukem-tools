package tools

import (
	"context"
	"errors"
	"testing"

	"github.com/AlexGalhardo/vukem-tools/internal/engine"
)

type fakeMediaEngines map[string]string

func (f fakeMediaEngines) Path(name string) (string, bool) {
	path, ok := f[name]
	return path, ok
}

func (f fakeMediaEngines) Has(name string) bool {
	_, ok := f[name]
	return ok
}

type failingRunner struct{ err error }

func (r failingRunner) Run(context.Context, engine.Command) (engine.Result, error) {
	return engine.Result{}, r.err
}

func TestABrokenEngineStopsTheBootWithoutAHandler(t *testing.T) {
	engines := fakeMediaEngines{engineFFmpeg: "ffmpeg", engineFFprobe: "ffprobe"}

	if _, err := ResolveMedia(t.Context(), engines, failingRunner{errors.New("exit")}, t.TempDir(), nil); err == nil {
		t.Fatal("a broken engine must fail the boot of a deploy")
	}
}

func TestAHandlerKeepsTheAppUpAndHidesTheBrokenEngine(t *testing.T) {
	engines := fakeMediaEngines{engineFFmpeg: "ffmpeg", engineFFprobe: "ffprobe"}

	var reported []string

	media, err := ResolveMedia(t.Context(), engines, failingRunner{errors.New("exit")}, t.TempDir(),
		func(name string, _ error) { reported = append(reported, name) })
	if err != nil {
		t.Fatalf("the app must start without the engine: %v", err)
	}

	if len(reported) != 1 || reported[0] != engineFFmpeg {
		t.Fatalf("reported %v, want only %s", reported, engineFFmpeg)
	}

	if media.HasFFmpeg() {
		t.Fatal("a tool must not be offered on an engine that failed its probe")
	}

	if media.Engines.Has(engineFFprobe) {
		t.Fatal("ffprobe belongs to the broken engine and must be hidden with it")
	}
}
