package downloader_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/AlexGalhardo/vukem-tools/internal/downloader"
)

// An unvalidated URL reaching yt-dlp is a request forger running inside this
// network with this service's egress: the generic extractor fetches whatever
// it is given, and the response comes back to the caller as metadata or as an
// error message that says what it saw.
//
// Three separate things stop that, and these tests hold all three, because
// they fail differently. A hostname rule can be wrong about a domain; the
// extractor allowlist still refuses to fetch it. The extractor allowlist
// could be mis-spelled; the hostname rule still refuses the URL. And `--`
// stops a URL that starts with a dash from being read as a flag at all.

// ssrfTargets are the addresses an attacker actually tries. They are kept in
// one list so a change to platform matching is checked against the whole set
// rather than against whichever one somebody remembered.
var ssrfTargets = []string{
	// Cloud instance metadata: the single most valuable target, because it
	// hands out credentials.
	"http://169.254.169.254/latest/meta-data/",
	"http://169.254.169.254/computeMetadata/v1/",
	"http://metadata.google.internal/computeMetadata/v1/instance/",
	// Services on the container and on the private network.
	"http://localhost:5432",
	"http://127.0.0.1:6379/",
	"http://[::1]:3000/admin",
	"http://10.0.0.5/internal",
	"http://192.168.1.1/",
	"http://172.16.0.1:8080/",
	// A host that only looks like a platform. The suffix match must require
	// the dot, or every one of these is read as YouTube.
	"https://youtube.com.attacker.example/watch?v=a",
	"https://notyoutube.com/watch?v=a",
	"https://tiktok.com.evil.test/@a/video/1",
	"https://x.com.evil.test/a/status/1",
	// Schemes that are not http at all.
	"file:///etc/passwd",
	"file:///proc/self/environ",
	"gopher://127.0.0.1:6379/_FLUSHALL",
	"ftp://169.254.169.254/",
	"dict://127.0.0.1:11211/stat",
	// Credentials in the authority, which is how a lookalike host is hidden
	// from somebody reading the URL.
	"https://www.youtube.com@attacker.example/watch?v=a",
	"https://user:pass@169.254.169.254/",
}

func TestNoInternalAddressIsEverAPlatform(t *testing.T) {
	t.Parallel()

	for _, target := range ssrfTargets {
		platform, err := downloader.PlatformOf(target)
		if err == nil {
			t.Errorf("%s was accepted as platform %q", target, platform.Name)
			continue
		}

		if !errors.Is(err, downloader.ErrUnsupportedURL) {
			t.Errorf("%s: error %v, want ErrUnsupportedURL", target, err)
		}
	}
}

func TestNoEngineIsStartedForAURLOutsideThePlatformList(t *testing.T) {
	t.Parallel()

	// This is the assertion that actually proves the absence of a request
	// forger: not that the error is right, but that yt-dlp was never run.
	for _, target := range ssrfTargets {
		t.Run(target, func(t *testing.T) {
			t.Parallel()

			runner := &fakeRunner{stdout: `{"title":"should never be reached"}`}
			d := newDownloader(t, runner, downloader.Options{})

			if _, err := d.Metadata(t.Context(), t.TempDir(), target); err == nil {
				t.Fatal("the probe succeeded")
			}

			if len(runner.calls) != 0 {
				t.Fatalf("yt-dlp was started for %s: %v", target, runner.calls[0].Args)
			}

			if _, err := d.Download(t.Context(), downloader.Request{
				URL:  target,
				Dir:  t.TempDir(),
				Kind: downloader.KindVideo,
			}); err == nil {
				t.Fatal("the download succeeded")
			}

			if len(runner.calls) != 0 {
				t.Fatalf("yt-dlp was started for %s: %v", target, runner.calls[0].Args)
			}
		})
	}
}

func TestTheURLIsAlwaysPassedAfterAnOptionTerminator(t *testing.T) {
	t.Parallel()

	// A URL beginning with a dash is read by yt-dlp as a flag unless option
	// parsing has been ended, and yt-dlp has flags that write files and run
	// commands — --exec among them. `--` makes everything after it a
	// positional argument.
	//
	// There is no shell anywhere in this path: internal/engine passes argv
	// directly. That stops shell metacharacters and nothing else; every
	// engine still parses its own arguments, which is what this guards.
	runner := &fakeRunner{
		stdout: `{"title":"t"}`,
		files:  map[string]int{"download.mp4": 64},
	}
	d := newDownloader(t, runner, downloader.Options{})

	if _, err := d.Metadata(t.Context(), t.TempDir(), "https://youtu.be/abc"); err != nil {
		t.Fatalf("metadata: %v", err)
	}

	if _, err := d.Download(t.Context(), downloader.Request{
		URL:  "https://youtu.be/abc",
		Dir:  t.TempDir(),
		Kind: downloader.KindVideo,
	}); err != nil {
		t.Fatalf("download: %v", err)
	}

	if len(runner.calls) != 2 {
		t.Fatalf("%d engine calls, want 2", len(runner.calls))
	}

	for i, cmd := range runner.calls {
		terminator := slices.Index(cmd.Args, "--")
		if terminator < 0 {
			t.Errorf("call %d has no -- terminator: %v", i, cmd.Args)
			continue
		}

		// The URL must be after it, and nothing that could be read as an
		// option may follow it.
		rest := cmd.Args[terminator+1:]

		if len(rest) != 1 {
			t.Errorf("call %d has %d positional arguments after --, want 1: %v", i, len(rest), rest)
			continue
		}

		if rest[0] != "https://youtu.be/abc" {
			t.Errorf("call %d passes %q after --, want the URL", i, rest[0])
		}
	}
}

func TestEveryInvocationRestrictsTheExtractorsToTheMatchedPlatform(t *testing.T) {
	t.Parallel()

	// The extractor allowlist is the layer that holds even when the hostname
	// rule is wrong: yt-dlp is told which extractors it may use, and
	// `generic` — the one that fetches an arbitrary host — is in no set.
	cases := map[string]string{
		"https://youtu.be/abc":                    "youtube.*",
		"https://www.instagram.com/reel/abc/":     "instagram.*",
		"https://x.com/someone/status/1":          "twitter.*",
		"https://www.tiktok.com/@someone/video/1": "tiktok.*",
	}

	for url, want := range cases {
		runner := &fakeRunner{stdout: `{"title":"t"}`}
		d := newDownloader(t, runner, downloader.Options{})

		if _, err := d.Metadata(t.Context(), t.TempDir(), url); err != nil {
			t.Fatalf("%s: %v", url, err)
		}

		args := runner.last(t)

		got, ok := argValue(args, "--use-extractors")
		if !ok {
			t.Fatalf("%s: --use-extractors is absent, so every extractor is enabled: %v", url, args)
		}

		if got != want {
			t.Errorf("%s: --use-extractors %q, want %q", url, got, want)
		}

		if strings.Contains(got, "generic") || got == "all" || got == "default" {
			t.Errorf("%s: --use-extractors %q can reach the generic extractor", url, got)
		}
	}
}
