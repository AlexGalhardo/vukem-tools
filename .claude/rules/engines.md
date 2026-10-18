# External engine rules

Every conversion engine runs as a subprocess, never through cgo.

- Always `exec.CommandContext` with a per-tool timeout, and kill the whole process group on
  cancellation or timeout.
- Always `setrlimit` before `exec`: `RLIMIT_AS`, `RLIMIT_CPU`, `RLIMIT_FSIZE`, `RLIMIT_NPROC`.
- One working directory per job, removed when the job ends, including on panic.
- No network for conversion subprocesses; only the downloader gets it.
- Parse progress from a structured stream (`ffmpeg -progress pipe:1`, yt-dlp JSON lines), never by
  scraping stderr.
- Pin every engine to an exact version in the Dockerfile. No `latest`, no distro default.
- Resolve every engine path from configuration, and log each engine's version at startup.
- Detect input type from magic bytes (`gabriel-vasile/mimetype`). Never trust the file extension or
  the client's `Content-Type`.
- Add the engine to the `/licenses` page with its exact licence before the feature ships. GPL and
  AGPL engines must be declared; see `docs/web-research.md`.
