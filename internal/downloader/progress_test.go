package downloader_test

import (
	"testing"

	"github.com/AlexGalhardo/vukem-tools/internal/downloader"
)

func TestProgressIsReadFromTheStructuredStream(t *testing.T) {
	t.Parallel()

	// These are the lines yt-dlp emits for the --progress-template this
	// package passes, including the "NA" an unknown size renders as.
	cases := []struct {
		name     string
		line     string
		ok       bool
		status   string
		download int64
		total    int64
		percent  int
	}{
		{
			name:     "a known size",
			line:     `{"status":"downloading","downloaded":"524288","total":"1048576","estimate":"NA"}`,
			ok:       true,
			status:   "downloading",
			download: 524288,
			total:    1048576,
			percent:  50,
		},
		{
			name:     "only an estimate",
			line:     `{"status":"downloading","downloaded":"250","total":"NA","estimate":"1000"}`,
			ok:       true,
			status:   "downloading",
			download: 250,
			total:    1000,
			percent:  25,
		},
		{
			name:     "a float estimate",
			line:     `{"status":"downloading","downloaded":"100","total":"NA","estimate":"1000.5"}`,
			ok:       true,
			status:   "downloading",
			download: 100,
			total:    1000,
			percent:  10,
		},
		{
			name:     "no size at all, which is unknown rather than zero",
			line:     `{"status":"downloading","downloaded":"4096","total":"NA","estimate":"NA"}`,
			ok:       true,
			status:   "downloading",
			download: 4096,
			total:    0,
			percent:  -1,
		},
		{
			name:     "finished",
			line:     `{"status":"finished","downloaded":"1048576","total":"1048576","estimate":"NA"}`,
			ok:       true,
			status:   "finished",
			download: 1048576,
			total:    1048576,
			percent:  100,
		},
		{
			name: "a post-processor notice is not progress",
			line: `[ExtractAudio] Destination: download.mp3`,
			ok:   false,
		},
		{
			name: "an empty line",
			line: "   ",
			ok:   false,
		},
		{
			name: "malformed JSON is dropped rather than failing the download",
			line: `{"status":"downloading","downloaded":`,
			ok:   false,
		},
		{
			name: "JSON without a status is not a progress line",
			line: `{"downloaded":"1"}`,
			ok:   false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			got, ok := downloader.ParseProgress(c.line)
			if ok != c.ok {
				t.Fatalf("parsed=%v, want %v (line %q)", ok, c.ok, c.line)
			}

			if !c.ok {
				return
			}

			if got.Status != c.status || got.Downloaded != c.download || got.Total != c.total {
				t.Errorf("got %+v, want status=%q downloaded=%d total=%d",
					got, c.status, c.download, c.total)
			}

			if got.Percent() != c.percent {
				t.Errorf("percent %d, want %d", got.Percent(), c.percent)
			}
		})
	}
}

func TestProgressDoneOnlyOnFinished(t *testing.T) {
	t.Parallel()

	if (downloader.Progress{Status: "downloading"}).Done() {
		t.Error("a download in flight reported itself finished")
	}

	if !(downloader.Progress{Status: "finished"}).Done() {
		t.Error("a finished download did not report itself finished")
	}
}

func TestProgressReachesTheCallerDuringADownload(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{
		files: map[string]int{"download.mp4": 1024},
		lines: []string{
			`{"status":"downloading","downloaded":"0","total":"1024","estimate":"NA"}`,
			`[info] something that is not progress`,
			`{"status":"downloading","downloaded":"512","total":"1024","estimate":"NA"}`,
			`{"status":"finished","downloaded":"1024","total":"1024","estimate":"NA"}`,
		},
	}

	d := newDownloader(t, runner, downloader.Options{})

	var seen []int

	if _, err := d.Download(t.Context(), downloader.Request{
		URL:      "https://youtu.be/abc",
		Dir:      t.TempDir(),
		Kind:     downloader.KindVideo,
		Progress: func(p downloader.Progress) { seen = append(seen, p.Percent()) },
	}); err != nil {
		t.Fatalf("download: %v", err)
	}

	// The notice between the progress lines is dropped, not reported.
	want := []int{-1, 50, 100}
	if len(seen) != len(want) {
		t.Fatalf("progress %v, want %v", seen, want)
	}

	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("progress %v, want %v", seen, want)
		}
	}
}

func TestADownloadWithoutAProgressHandlerDoesNotOpenAProgressPipe(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{files: map[string]int{"download.mp4": 16}}
	d := newDownloader(t, runner, downloader.Options{})

	if _, err := d.Download(t.Context(), downloader.Request{
		URL:  "https://youtu.be/abc",
		Dir:  t.TempDir(),
		Kind: downloader.KindVideo,
	}); err != nil {
		t.Fatalf("download: %v", err)
	}

	if runner.calls[0].Progress != nil {
		t.Error("a progress reader was started with nobody listening")
	}
}
