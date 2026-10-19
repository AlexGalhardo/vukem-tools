package jobs

import (
	"path/filepath"
	"testing"

	"github.com/AlexGalhardo/vukem/queue"
)

// The job directory must be absolute whatever the configuration says, because
// a tool hands it to its engine as the working directory and also builds the
// paths in the engine's arguments out of it. A relative one is resolved twice
// — by this process against its own working directory, and by the engine
// against the directory it was just given — and the engine asks for a path
// that does not exist.
//
// This is not hypothetical: WORK_DIR is unset in production, its default is
// the relative "storage/work", and every PDF tool failed with
// `qpdf: open storage/work/<id>/input-0.pdf: No such file or directory`.
// Every other test gives the pipeline a t.TempDir, which is already absolute,
// which is why nothing caught it.
func TestTheJobDirectoryIsAbsoluteEvenWhenTheWorkRootIsNot(t *testing.T) {
	t.Chdir(t.TempDir())

	p, err := NewPipeline(PipelineOptions{
		Store:    &Store{},
		Queue:    func() (*queue.Queue, error) { return nil, nil },
		WorkRoot: "storage/work",
	})
	if err != nil {
		t.Fatalf("pipeline: %v", err)
	}

	dir, err := p.workdir("01M5AZGCP5H9HTJ3NYSKY17SAN")
	if err != nil {
		t.Fatalf("workdir: %v", err)
	}

	if !filepath.IsAbs(dir) {
		t.Errorf("the job directory is %q: an engine given that as its working directory would "+
			"resolve its own arguments against it a second time", dir)
	}
}
