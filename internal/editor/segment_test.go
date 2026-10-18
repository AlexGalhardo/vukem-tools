package editor_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/AlexGalhardo/vukem-tools/internal/editor"
	"github.com/AlexGalhardo/vukem-tools/internal/engine"
)

// segmenter is a fake ffmpeg that behaves like the segment muxer: the copying
// pass cuts at the keyframes it is given, and the forced-keyframe pass cuts
// exactly on the boundary.
//
// It exists because the thing worth testing is the decision — measure the
// clips, redo the split when one overran — and that decision cannot be
// reached without a muxer that overruns.
type segmenter struct {
	// copied is the clip durations the copying pass produces.
	copied []float64
	// encoded is what the forced-keyframe pass produces.
	encoded []float64
	// passes counts how many ffmpeg invocations there were.
	passes int
	// forced records whether each pass forced keyframes.
	forced []bool
}

func (s *segmenter) handle(cmd engine.Command) (engine.Result, error) {
	if cmd.Path == ffprobePath {
		return s.probe(cmd)
	}

	force := slices.Contains(cmd.Args, "-force_key_frames")

	s.passes++
	s.forced = append(s.forced, force)

	durations := s.copied
	if force {
		durations = s.encoded
	}

	for i := range durations {
		name := filepath.Join(cmd.Dir, clipName(i))
		if err := os.WriteFile(name, make([]byte, 1024+i), 0o600); err != nil {
			return engine.Result{}, err
		}
	}

	return engine.Result{}, nil
}

// probe answers ffprobe for the input and for each clip.
func (s *segmenter) probe(cmd engine.Command) (engine.Result, error) {
	target := filepath.Base(inputPath(cmd.Args))

	durations := s.copied
	if len(s.forced) > 0 && s.forced[len(s.forced)-1] {
		durations = s.encoded
	}

	body := probeJSON(1280, 720, 150)

	for i := range durations {
		if target == clipName(i) {
			body = probeJSON(1280, 720, durations[i])
			break
		}
	}

	_, err := cmd.Stdout.Write([]byte(body))

	return engine.Result{}, err
}

// clipName is the name the segment muxer's pattern produces for index i.
func clipName(i int) string {
	return "clip" + string(rune('0'+i/100)) + string(rune('0'+i/10%10)) + string(rune('0'+i%10)) + ".mp4"
}

// inputPath returns the file a command was pointed at.
func inputPath(args []string) string {
	for i, a := range args {
		if a == "-i" && i+1 < len(args) {
			return args[i+1]
		}
	}

	return ""
}

func TestACopiedSplitIsKeptWhenEveryClipIsWithinTheLimit(t *testing.T) {
	t.Parallel()

	fake := &segmenter{copied: []float64{60, 60, 30}}
	runner := &fakeRunner{handle: fake.handle}
	e := newEditor(t, runner, 0)

	dir := t.TempDir()

	clips, err := e.Segment(t.Context(), editor.SegmentRequest{Dir: dir, Input: writeInput(t, dir)})
	if err != nil {
		t.Fatalf("segment: %v", err)
	}

	if len(clips) != 3 {
		t.Fatalf("%d clips, want 3", len(clips))
	}

	if fake.passes != 1 {
		t.Errorf("%d ffmpeg passes, want 1: a split that fits must not re-encode", fake.passes)
	}

	for i := range clips {
		if clips[i].Index != i {
			t.Errorf("clip %d has index %d", i, clips[i].Index)
		}

		if clips[i].Duration > editor.MaxClip {
			t.Errorf("clip %d is %s, past the %s limit", i, clips[i].Duration, editor.MaxClip)
		}

		if clips[i].Bytes == 0 {
			t.Errorf("clip %d has no size", i)
		}
	}
}

func TestAnOverrunningCopiedSplitIsRedoneWithForcedKeyframes(t *testing.T) {
	t.Parallel()

	// A source with a keyframe every 75 seconds: the segment muxer cuts at
	// the first keyframe at or after each boundary, so copying produces
	// clips longer than the limit.
	fake := &segmenter{
		copied:  []float64{75, 75},
		encoded: []float64{60, 60, 30},
	}
	runner := &fakeRunner{handle: fake.handle}
	e := newEditor(t, runner, 0)

	dir := t.TempDir()

	clips, err := e.Segment(t.Context(), editor.SegmentRequest{Dir: dir, Input: writeInput(t, dir)})
	if err != nil {
		t.Fatalf("segment: %v", err)
	}

	if fake.passes != 2 {
		t.Fatalf("%d ffmpeg passes, want 2", fake.passes)
	}

	if fake.forced[0] || !fake.forced[1] {
		t.Errorf("passes forced keyframes %v, want [false true]", fake.forced)
	}

	// The ROADMAP's assert: every clip is at most 60 seconds.
	for i := range clips {
		if clips[i].Duration > editor.MaxClip {
			t.Errorf("clip %d is %s, past the %s limit", i, clips[i].Duration, editor.MaxClip)
		}
	}

	if len(clips) != 3 {
		t.Fatalf("%d clips, want 3", len(clips))
	}

	// The discarded pass's files must be gone, or they would be collected as
	// clips of the second pass.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	var found int

	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "clip") {
			found++
		}
	}

	if found != len(clips) {
		t.Errorf("%d clip files on disk, want %d", found, len(clips))
	}
}

func TestASegmentLongerThanTheLimitIsClamped(t *testing.T) {
	t.Parallel()

	fake := &segmenter{copied: []float64{60}}
	runner := &fakeRunner{handle: fake.handle}
	e := newEditor(t, runner, 0)

	dir := t.TempDir()

	if _, err := e.Segment(t.Context(), editor.SegmentRequest{
		Dir:     dir,
		Input:   writeInput(t, dir),
		Segment: 10 * time.Minute,
	}); err != nil {
		t.Fatalf("segment: %v", err)
	}

	args := runner.ffmpegCalls()[0].Args

	var segmentTime string

	for i, a := range args {
		if a == "-segment_time" && i+1 < len(args) {
			segmentTime = args[i+1]
		}
	}

	if segmentTime != "60.000" {
		t.Errorf("-segment_time %q, want 60.000: a longer request must be clamped to MaxClip", segmentTime)
	}
}

func TestASplitThatWroteNoClipIsAFailure(t *testing.T) {
	t.Parallel()

	fake := &segmenter{}
	runner := &fakeRunner{handle: fake.handle}
	e := newEditor(t, runner, 0)

	dir := t.TempDir()

	_, err := e.Segment(t.Context(), editor.SegmentRequest{Dir: dir, Input: writeInput(t, dir)})

	var editErr *editor.EditError
	if !errors.As(err, &editErr) || editErr.Reason != editor.ReasonEngine {
		t.Fatalf("error %v, want an engine EditError", err)
	}
}

func TestClipsAreReturnedInOrder(t *testing.T) {
	t.Parallel()

	// Twelve clips, so the zero padding is what keeps clip002 before clip010
	// rather than after it.
	durations := make([]float64, 12)
	for i := range durations {
		durations[i] = 30
	}

	fake := &segmenter{copied: durations}
	runner := &fakeRunner{handle: fake.handle}
	e := newEditor(t, runner, 0)

	dir := t.TempDir()

	clips, err := e.Segment(t.Context(), editor.SegmentRequest{Dir: dir, Input: writeInput(t, dir)})
	if err != nil {
		t.Fatalf("segment: %v", err)
	}

	if len(clips) != len(durations) {
		t.Fatalf("%d clips, want %d", len(clips), len(durations))
	}

	for i := range clips {
		if clips[i].Name != clipName(i) {
			t.Fatalf("clip %d is %q, want %q", i, clips[i].Name, clipName(i))
		}
	}
}
