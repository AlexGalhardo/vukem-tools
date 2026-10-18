package editor_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AlexGalhardo/vukem-tools/internal/editor"
	"github.com/AlexGalhardo/vukem-tools/internal/engine"
)

func TestProgressIsReadFromTheProgressPipe(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		line string
		ok   bool
		out  time.Duration
		done bool
	}{
		{name: "microseconds", line: "out_time_us=1500000", ok: true, out: 1500 * time.Millisecond},
		{
			// out_time_ms is a long-standing misnomer in ffmpeg: it holds
			// microseconds. Reading it as milliseconds would put the bar a
			// thousand times too far along.
			name: "the misnamed millisecond key is also microseconds",
			line: "out_time_ms=2000000",
			ok:   true,
			out:  2 * time.Second,
		},
		{name: "the end", line: "progress=end", ok: true, done: true},
		{name: "a continuation is not an update", line: "progress=continue", ok: false},
		{name: "a key nobody reads", line: "bitrate=1234.5kbits/s", ok: false},
		{name: "a status line ffmpeg would have printed to stderr", line: "frame= 100 fps=25", ok: false},
		{name: "no separator", line: "nonsense", ok: false},
		{name: "a negative position", line: "out_time_us=-1", ok: false},
		{name: "not a number", line: "out_time_us=N/A", ok: false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			got, ok := editor.ParseProgress(c.line)
			if ok != c.ok {
				t.Fatalf("parsed=%v, want %v (line %q)", ok, c.ok, c.line)
			}

			if !c.ok {
				return
			}

			if got.OutTime != c.out {
				t.Errorf("out time %s, want %s", got.OutTime, c.out)
			}

			if got.Done != c.done {
				t.Errorf("done=%v, want %v", got.Done, c.done)
			}
		})
	}
}

func TestThePercentageIsRelativeToTheRequestedDuration(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{handle: func(cmd engine.Command) (engine.Result, error) {
		if cmd.Path == ffprobePath {
			_, err := cmd.Stdout.Write([]byte(probeJSON(1280, 720, 60)))

			return engine.Result{}, err
		}

		// Half of a ten-second output, then the end.
		cmd.Progress("out_time_us=5000000")
		cmd.Progress("progress=end")

		return engine.Result{}, os.WriteFile(filepath.Join(cmd.Dir, "out.mp4"), make([]byte, 64), 0o600)
	}}

	e := newEditor(t, runner, 0)
	dir := t.TempDir()

	var seen []int

	if _, err := e.Trim(t.Context(), editor.TrimRequest{
		Dir:      dir,
		Input:    writeInput(t, dir),
		Output:   "out.mp4",
		Duration: 10 * time.Second,
		Progress: func(p editor.Progress) { seen = append(seen, p.Percent) },
	}); err != nil {
		t.Fatalf("trim: %v", err)
	}

	want := []int{50, 100}
	if len(seen) != len(want) {
		t.Fatalf("progress %v, want %v", seen, want)
	}

	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("progress %v, want %v", seen, want)
		}
	}
}

func TestAnEditWithoutAProgressHandlerDoesNotOpenAProgressPipe(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{handle: probeAnd(probeJSON(640, 360, 10), keyframeJSON(0), "out.mp4", 64)}
	e := newEditor(t, runner, 0)
	dir := t.TempDir()

	if _, err := e.Trim(t.Context(), editor.TrimRequest{
		Dir:    dir,
		Input:  writeInput(t, dir),
		Output: "out.mp4",
	}); err != nil {
		t.Fatalf("trim: %v", err)
	}

	for _, cmd := range runner.ffmpegCalls() {
		if cmd.Progress != nil {
			t.Error("a progress reader was started with nobody listening")
		}
	}
}
