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
  account menu behind a face icon, and the three touch defaults the stylesheet was missing.

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

## Framework feedback

**No open findings against vukem v0.30.0.** All five `workaround(vukem)` markers were verified
against the pinned source and all five were stale; the markers are gone and the workarounds with
them. See [vukem-audit.md](vukem-audit.md).

## Next

- Cut the release the sprite phase was planned for.
- The two migrations this repository owes itself, both recorded in
  [vukem-audit.md](vukem-audit.md): body examples onto `MediaType.Example`, and both security
  schemes declared through `vukem.WithOpenAPI`.
- The remaining planned tools need engines that are not in the image — LibreOffice for
  Office → PDF, Tesseract for OCR — which is an image-size and licence decision for the owner,
  not a coding task.
