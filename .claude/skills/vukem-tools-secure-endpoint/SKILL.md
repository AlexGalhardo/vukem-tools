---
name: vukem-tools-secure-endpoint
description: Security checklist for adding or changing an HTTP route, API key ability, upload handler, conversion job or engine invocation in vukem-tools. Use whenever routes, middleware, uploads, storage, queue jobs, yt-dlp options or ffmpeg/libvips/Ghostscript invocations change.
---

# Secure endpoint in vukem-tools

This app accepts untrusted files from the internet and feeds them to C programs. Treat every input
as hostile. Reference: `docs/web-research.md` (engine traps and CVEs), `.claude/rules/engines.md`,
and vukem's own <https://vukem.dev/docs/security/overview>.

## Authentication and authorization

1. **No anonymous use.** Every tool route requires an authenticated account with a verified email
   (`auth.Verified`). There is no guest path; do not add one.
2. **API keys** carry abilities per tool (`image:convert`, `video:download`, ...). A route checks
   its own ability with `auth.Token.Can`; a missing ability answers `403` and names the ability.
   Both `X-API-Key` and `Authorization: Bearer` are accepted.
3. **Browser routes** keep CSRF; API routes are token-authenticated and CSRF-exempt. Never exempt
   a browser route to make a form work.
4. **Fail closed.** A new route is deny-by-default: if you cannot name who may call it, it is not
   finished.

## Input

1. **Type detection from magic bytes** (`gabriel-vasile/mimetype`). The extension and the client's
   `Content-Type` are hints for the UI, never the basis of a decision. Office formats keep their
   signature near the end of the file, so set the sniff limit accordingly.
2. **Size and dimension caps before decoding.** 500 MB per file; a pixel cap for images
   (`upload.MaxPixels`) so a 100 KB PNG cannot claim 60000x60000. Check the declared dimensions
   before the decoder allocates.
3. **Typed, validated options.** Every conversion option goes through a `z` schema with explicit
   bounds. No option is interpolated into a command line without validation.
4. **Filenames** pass `upload.SanitizeName`. Job IDs and indexes are validated against a pattern
   before they reach a path.
5. **Archives**: cap the compression ratio, total uncompressed size, entry count and nesting
   depth; refuse symlinks and any entry resolving outside the extraction root (zip slip).

## Engines

1. **Argument lists, never a shell.** `exec.CommandContext` with an explicit `[]string`. No
   `sh -c`, no string interpolation of user input.
2. **Timeout and process-group kill** on every invocation, and `setrlimit` before `exec`
   (`RLIMIT_AS`, `RLIMIT_CPU`, `RLIMIT_FSIZE`, `RLIMIT_NPROC`).
3. **No network** for conversion subprocesses. Only the downloader gets it.
4. **ffmpeg input hardening**: restrict protocols (`-protocol_whitelist file`) and check the
   demuxer, so an HLS or concat playlist cannot read local files.
5. **ImageMagick**, if used at all, is pinned at or above 7.1.2-34 with a restrictive `policy.xml`
   that includes `<policy domain="path" rights="none" pattern="fd:*"/>` and
   `<policy domain="path" rights="none" pattern="*../*"/>`. Three policy bypasses landed in 2026;
   prefer libvips.
6. **Ghostscript** is AGPL and has a long CVE history. Pin it, keep it current, and never hand it
   a file that has not passed type detection and the size cap.
7. **One working directory per job**, removed when the job ends, including on panic. A job never
   reads or writes outside it.

## SSRF, when a URL is the input

1. URLs are matched against an allowlist by **hostname**, not by `netloc` or a substring.
2. yt-dlp runs with a restricted extractor set. Never enable the `generic` extractor: it will
   fetch anything, including link-local metadata endpoints.
3. Any other outbound fetch goes through `httpclient.Safe`, which refuses private and link-local
   address space.

## Output

1. Results are served through **signed URLs with a short TTL** (`vukem/signed`), never by handing
   out a path.
2. Downloads carry a sanitised filename, `filename*` in UTF-8 and `Cache-Control: no-store`.
3. Results expire after one hour and the cleanup is idempotent. A full disk takes the service down.
4. Stripping metadata is the default for images; say so in the UI, and convert to sRGB before
   stripping so the colour does not shift.

## Errors and secrets

1. Errors are `problem+json` on API routes, with a stable `error_type`. They never echo user input,
   internal paths or engine stderr verbatim.
2. Never log an API key, a signed URL, a cookie file, a user's email or an engine's full argument
   list when it contains a path with user content.
3. A new `error_type` is added in one place and surfaced consistently in the UI and in the OpenAPI
   document.

## Before you finish

- Add a test for the malicious case, not only the happy path: wrong magic bytes, oversized input,
  path traversal in a filename, a cancelled job, a timeout.
- Confirm the engine is on the `/licenses` page with its exact licence.
- Run the `vukem-tools-verification` skill.
- If vukem itself got in the way, append an entry to
  `../framework-feedback/vukem-tools-feedback.md` and mark the workaround in code with
  `// workaround(vukem): <entry title>`.
