# Status

Where the project is, and what is next. Updated at the end of every completed task group, per
[`.claude/rules/framework-feedback.md`](../.claude/rules/framework-feedback.md).

## Now

**Version:** `v0.6.1` released (`v0.6.0` plus the sidebar's stacked theme options), with `v0.2.0` to `v0.4.0` added as tags on the commits that finished
archives, image compression and sprite sheets (see [brainstorming.md § 9.7](brainstorming.md)).
**Next:** record peak memory and wall-clock for LibreOffice and OCR in
[benchmarks.md](benchmarks.md), rebuild and check the production image, and try the bot against the
real Telegram Bot API.

**54 tools live** in the catalogue and none planned: the conversion pipeline's tools plus seventeen that run in the browser.

## Shipped on 2026-10-24

Released as `v0.6.0`. Decisions and reasons are in [brainstorming.md § 9](brainstorming.md).

- **Left sidebar.** The header is gone; a fixed column on the left with the hamburger drawer on a
  phone. The catalogue no longer shows "Not planned" rows.
- **Seventeen developer tools** in the browser, with `bun test` coverage: JSON format, schema and
  tool-definition validation, JSON/YAML/CSV/XML conversion, diff, hash and HMAC, passwords,
  UUID/ULID, Base64, JWT, CPF/CNPJ, fake people, token estimate, LLM output cleaning, LLM cost
  and chat-format conversion. Two server lookups: `/lookup/cep/{cep}` and `/lookup/cnpj/{cnpj}`.
- **Word, PowerPoint and Excel to PDF, OCR, PDF forms and Sign a PDF.** LibreOffice, OCRmyPDF with
  Tesseract and qpdf, all run against the real engines in a Debian trixie container; signatures are
  verified with `pdfsig`.
- **`/llms.txt` and `/llms-full.txt`**, generated from the catalogue.
- **Telegram bot, rebuilt.** Each person signs in from the chat through a link the server approves
  (`/account/link`), then uses the tools from button menus: images, audio and video, PDF,
  documents to PDF, archives and downloads by link, with the account's budget on the main menu and
  Sign out revoking the key. New API: `POST /api/v1/device-links`, `POST /api/v1/device-links/token`
  and `GET /api/v1/usage`. See [brainstorming.md § 9.8](brainstorming.md).
- **Clients regenerated** for the six new API operations (`codegen:client`).

### Not verified here

- The sidebar was typechecked and built but not looked at in a browser, because no database was
  available to sign in with. Check it at phone width and in dark mode.
- The new Dockerfile pins were checked against the pinned base image with `apt-cache policy`; the
  image itself was not rebuilt.
- Peak memory of LibreOffice and OCR under the production limits was checked once, not benchmarked;
  nothing is recorded in [benchmarks.md](benchmarks.md) for them yet.

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
10. **The header's API link was an Inertia link to a page Inertia does not render.** `/docs` is
    the Scalar reference, served as plain HTML by `vukem/apidocs`, so the visit fetched it by XHR,
    got a response with no `X-Inertia` header, and the client rendered it in its full-screen error
    iframe: a white panel over the home page that never navigated and could not be dismissed.
    Confirmed against production rather than inferred — with a matching `X-Inertia-Version`,
    `GET /docs` returns `200 text/html` and no Inertia header. The entries now live in
    `resources/js/lib/nav.ts` so the rule "a link that leaves the app is a document link" is
    tested without a DOM.

## Framework feedback

**No open findings against vukem v0.30.0.** All five `workaround(vukem)` markers were verified
against the pinned source and all five were stale; the markers are gone and the workarounds with
them. See [vukem-audit.md](vukem-audit.md).

## Next

- **A production deploy still dies at boot and nobody can say why.** Railway deployment
  `77ffd96c` (`d0a4ec9`) failed its `/up` healthcheck in a restart loop on
  `vukem: routes: tools: probe libvips: image: write the capability probe image: other`, and so
  did `db018dd4` sixteen hours earlier on `4fbcf373` — a commit whose redeploy, one minute later,
  succeeded. It is intermittent and it is not a regression: the only runtime file PR #21 changed
  on that path is `routes/api_media.go`, and the failure predates it.

  What is known: `vips -l` runs immediately before and succeeds, so the engine starts; the step
  that fails is the first one that writes, which is also the first one that builds a thread pool.
  The error now carries its cause, which it did not before — `EngineError.Error()` dropped the
  wrapped error whenever the engine had printed nothing, which is exactly the case where the
  engine never got to speak — and on `c66dd0b` it said:

  ```text
  vukem: routes: tools: probe libvips: image: write the capability probe image: other:
  engine: exit status -1
  ```

  **`-1` is Go for "terminated by a signal".** So `vips` is killed from outside, with its stderr
  empty, which fits a SIGKILL losing whatever was buffered. The obvious reading is the cgroup OOM
  killer and **that is ruled out by measurement**: the service's limit is 8 GB and its peak over
  the surrounding two hours was 0.338 GB. Do not repeat "out of memory" as the cause.

  What is still missing is the signal itself, because Go reports every one of them as `-1`.
  `ExitError` now carries the kernel's own description ("signal: killed", "signal: segmentation
  fault"), which separates a kill from outside the process from a crash inside it — opposite ends
  of the system. The next occurrence settles it.

- **`RLIMIT_NPROC` was an absolute number and is now headroom.** `prlimit --nproc` was passed the
  configured figure as-is, but the kernel measures that limit against every task the real UID owns
  anywhere on the machine, so the engine's budget was spent by whatever else shared the UID. The
  sandbox now adds the UID's current task count (`uidTasks` in `internal/engine/sandbox_linux.go`)
  and `Limits.Processes` means headroom. Reproduced and verified against the image's own libvips:
  `prlimit --nproc=4` gives `glib: Error creating thread: Resource temporarily unavailable` and a
  truncated 64-byte output, headroom gives exit 0 and a whole file. `internal/image` also grew the
  `resource_limit` kind `internal/ffmpeg` already had, so that failure names itself instead of
  classifying as `other`.

- **CI still runs no engine, so every engine-backed test skips there.** Installing libvips in the
  App job was tried and taken back out, and the headroom fix above is not enough to put it back.
  Verified by running `./internal/image` as a non-root user with libvips present: conversions
  still fail with `glib: Error creating thread` and `libgomp: Thread creation failed`, because the
  package's tests run in parallel and each engine computes its ceiling from the tasks that exist
  when it starts rather than the ones its peers are about to create. Two threads to pull: size the
  headroom from the engine concurrency the caller permits, and bound libgomp — `internal/image`
  sets `VIPS_CONCURRENCY` from `cpu.Limit()` but nothing sets `OMP_NUM_THREADS`, so OpenMP sizes
  its pool from the host's CPU count, which is the thing `AGENTS.md` forbids.

- `v0.5.0` is cut: it carries S7 (sprite sheets), S8 (archives), image compression, the sitemap
  and the ten faults above. The version follows the ROADMAP's phase mapping rather than the
  SemVer sequence — `v0.2.0` through `v0.4.0` were never used, because the work those phases
  planned went out inside `v0.1.0`.
- The two migrations this repository owes itself, both recorded in
  [vukem-audit.md](vukem-audit.md): body examples onto `MediaType.Example`, and both security
  schemes declared through `vukem.WithOpenAPI`.
- The remaining planned tools need engines that are not in the image — LibreOffice for
  Office → PDF, Tesseract for OCR — which is an image-size and licence decision for the owner,
  not a coding task.
