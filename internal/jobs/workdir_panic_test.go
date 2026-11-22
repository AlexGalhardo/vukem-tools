package jobs_test

import (
	"context"
	"os"
	"testing"

	"github.com/AlexGalhardo/vukem-tools/internal/jobs"
)

// A tool that panics must not leave its input on disk: the engines rule is one
// working directory per job, removed even on panic.
func TestThePanickingToolsWorkingDirectoryIsRemoved(t *testing.T) {
	t.Parallel()

	var seen string

	tl := newTool(func(_ context.Context, w jobs.Work) (jobs.Outcome, error) {
		seen = w.Dir

		panic("the tool blew up")
	})
	r := newRig(t, tl)

	r.submit(t, jobs.Submission{})

	// Whether the queue or the pipeline absorbs the panic is not what is under
	// test; what the panic leaves on disk is.
	func() {
		defer func() { _ = recover() }()

		r.work(t)
	}()

	if seen == "" {
		t.Fatal("the tool never ran")
	}

	if _, err := os.Stat(seen); !os.IsNotExist(err) {
		t.Fatalf("%s survived the panic (stat error: %v)", seen, err)
	}
}
