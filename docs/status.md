# Status

Where the project is, and what is next. Updated at the end of every completed task group, per
[`.claude/rules/framework-feedback.md`](../.claude/rules/framework-feedback.md).

## Now

**Version:** `v0.1.0` released; `main` is ahead of it.
**Next milestone:** `v0.5.0`, which is what [ROADMAP.md](../ROADMAP.md) names for the sprite
sheets — now built, so the tag is a release task rather than a development one.

**30 tools registered.**

## Shipped on 2026-10-19

- **Archives (S8).** `archive-create` and `archive-extract`: ZIP, TAR, TAR.GZ and GZ in both
  directions, pure Go, no dependency added. XZ and ZSTD dropped with the reason recorded.
- **Image compression (S4.2).** `image-compress` on libvips alone, trying every setting worth
  trying and keeping the smallest file. The specialist optimiser chain the roadmap planned was
  not taken; the figures that decision has to be judged against are in
  [benchmarks.md](benchmarks.md).
- **Sprite sheets (S7.3–S7.5).** Five atlas metadata writers with goldens parsed back by
  independent readers, both tools with their API, and two pages — a grid overlay that redraws in
  the browser, and the packed sheet shown as the result.
- **The header.** Two layouts instead of one that shrinks, a 44px floor on every control, an
  account menu behind a face icon, and the three touch defaults the stylesheet was missing. Then
  trimmed: one sign-in button rather than two, the theme control down to the two themes, and the
  account menu down to three destinations — the account, the API reference and the way out.
- **`/sitemap.xml`.** Absolute URLs from `APP_URL`, and only pages an anonymous visitor can
  actually read, which leaves the tool pages out because every one of them needs an account. A
  gate in `./tests` fails when a public page is neither advertised nor given a written reason for
  being left out, so the list cannot go stale beside a router that changes.

### Bugs found and fixed on the way

Each has a regression test; the detail is in the commit and in
[benchmarks.md](benchmarks.md).

1. **Every PNG this service wrote was unfiltered** — libvips's `pngsave` defaults to no row
   filtering, making the project's own fixture **74× larger** than necessary.
2. **The engine probe resolved its path twice**, so the whole of `./tests/` failed at app boot on
   any machine with libvips installed. Green in CI, because the image sets an absolute path.
3. **The engine probe shared one scratch file**, so parallel tests overwrote each other's probe
   image.
4. **`jobs.Refuse`'s specific message never reached a client that reloaded the page** — it was
   written to the progress trail and the view derived its text from the error code alone.
5. **A conversion had nowhere to say what it did**, so a compression that changed nothing looked
   like a broken tool to any client whose event stream connected after the work finished.
6. **No compression-ratio cap**, so a kilobyte of gzip could write half a gigabyte before the
   size cap caught it. Found by fuzzing, not by review.
7. **The CSP refused `blob:` images**, so no page could preview a file somebody had just chosen.
8. **The `scale` icon was drawn as a waste bin** and was used by all three compression tools.
9. **The job working directory was relative in production**, so every engine resolved its own
   arguments twice and every tool failed. `WORK_DIR` is unset there and its default is the
   relative `storage/work`, which a tool hands to its engine as the working directory *and* uses
   to build the paths in the engine's arguments — so qpdf asked for
   `storage/work/<id>/storage/work/<id>/input-0.pdf` and the owner got "The converter could not
   process that PDF." on all nineteen PDF tools. It is fault 2 above, in the other half of the
   app: that fix went into the capability probe and not into `jobs.NewPipeline`, and no test
   caught it because every one of them passes a `t.TempDir`, which is already absolute.

## Framework feedback

**No open findings against vukem v0.30.0.** All five `workaround(vukem)` markers were verified
against the pinned source and all five were stale; the markers are gone and the workarounds with
them. See [vukem-audit.md](vukem-audit.md).

## Next

- **CI runs no engine, so every engine-backed test skips there.** Installing libvips in the App
  job was tried and taken back out: with the engine present, `./tests` goes green — which is the
  first real proof `image-compress` works end to end on a clean machine — but every test in
  `internal/image` fails with `glib: Error creating thread: Resource temporarily unavailable`.
  The cause is `internal/image/fixture_test.go`, which caps the engine at `Processes: 64`.
  `RLIMIT_NPROC` counts every task the real UID owns anywhere on the machine, not the ones this
  process started, so 64 is generous in a container where the test binary is nearly alone and
  instantly fatal on a runner whose user already owns hundreds of threads. The fix is to size that
  cap from what the UID already uses rather than from an absolute number — the same caveat
  `app/services/engines.go` already states for production. It is not in this PR because it cannot
  be verified from a root shell: root holds `CAP_SYS_RESOURCE` and ignores `RLIMIT_NPROC`
  entirely, so the failure does not reproduce there and neither would the fix.

- Cut the release the sprite phase was planned for.
- The two migrations this repository owes itself, both recorded in
  [vukem-audit.md](vukem-audit.md): body examples onto `MediaType.Example`, and both security
  schemes declared through `vukem.WithOpenAPI`.
- The remaining planned tools need engines that are not in the image — LibreOffice for
  Office → PDF, Tesseract for OCR — which is an image-size and licence decision for the owner,
  not a coding task.
