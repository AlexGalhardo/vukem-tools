---
name: vukem-tools-verification
description: Validate a change in vukem-tools exactly like the git hooks and CI do (Go build, vet, golangci-lint, race tests, govulncheck, tsc, Vite build, markdownlint) before calling it done. Use after any code, config, infra or dependency change in this repository, and before committing or opening a PR.
---

# Local verification (the same gates as CI)

Nothing is done until these pass from the repository root. Run the narrowest one that covers the
change first, then the full set before committing.

## Go: the backend of record

```bash
go build ./...
go vet ./...
golangci-lint run ./...
go test -race -count=1 ./...
```

One package while iterating: `go test -race -count=1 ./internal/pixelsvg/...`.
Formatting is `golangci-lint fmt ./...` (gofumpt + gci + golines, 120 columns, tabs).

## Frontend

```bash
bun run typecheck
bun run build
```

## Markdown

```bash
bun run lint:md
```

## Everything the hooks run

```bash
bun run check   # typecheck + lint:md (= pre-commit, after the Go steps)
```

`pre-commit` runs `golangci-lint fmt`, `go vet` and `bun run check`.
`pre-push` runs `go test -race`, `bun run build` and `go build`.
Never use `--no-verify`.

## Performance claims need a measurement

Any conversion path, and any change that touches one, needs numbers rather than an opinion:

```bash
go test -bench . -benchmem ./internal/<package>/...
```

Record wall-clock and peak RSS per conversion kind in `docs/benchmarks.md`. A regression against a
recorded figure is a failure, not a note.

## Known traps

- **Engines missing locally.** Tests that drive `ffmpeg`, `ffprobe`, `yt-dlp`, `vips` or `gs` skip
  when the binary is absent. CI installs them; install them locally or you are not testing what CI
  tests. The app logs every engine version at startup — read that line.
- **Postgres.** Database tests use `drivers/postgres/pgtest`. Without a reachable Postgres they
  skip, which looks green and proves nothing.
- **`.env` leaking into tests.** Tests must not read the developer's `.env`. If a test starts
  passing only on your machine, that is the cause.
- **`t.TempDir` cleanup fails intermittently on Windows.** `go test -race ./...` fails every so
  often with `TempDir RemoveAll cleanup: unlinkat ...: The folder is not empty`, in whichever
  engine-backed package happened to be running — `internal/editor` and `app/controllers` have both
  been seen, and it lands on a different test each time (1 run in 3 on `internal/editor` alone).
  Windows refuses to delete a directory while any handle to it is open; **which** handle is open
  has not been pinned, and it is not one cause. A surviving engine child is the obvious suspect —
  the Windows sandbox is `nopSandbox`, which kills the process but not its group — but
  `TestADatabaseWithoutItsSchemaFails` in `app/controllers` has failed the same way and runs no
  subprocess at all, so a SQLite handle or the virus scanner reaches it too. Measure before
  repeating any of these as the cause. What is established is that it is local to Windows and to
  the tests: the same tree is deterministic on Linux, in Docker and in CI. Re-run it (the
  `pre-push` hook runs it too, so a push may need a second attempt) and never reach for
  `--no-verify`. If you want certainty rather than a re-run, run the suite on Linux:

  ```bash
  docker run --rm --network <net> -v "$PWD:/src" -w /src \
    -e TEST_DATABASE_URL="postgres://pg:pg@<pg-host>:5432/postgres?sslmode=disable" \
    golang:1.27.2 go test -race -count=1 ./...
  ```

- **PDF, Office and OCR engines.** `soffice`, `ocrmypdf`, `qpdf`, `gs` and `pdftotext` are not on a
  Windows machine, so `internal/pdf` tests that need them skip. Run them on Linux against the real
  engines: build a Debian trixie image with the packages pinned in the `Dockerfile`
  (`libreoffice-writer`, `-calc`, `-impress`, `ocrmypdf`, `tesseract-ocr-eng`, `-por`, `qpdf`,
  `ghostscript`, `poppler-utils`) and Go, mount the repository and run
  `go test -count=1 ./internal/pdf/...`. Without `gcc` there is no `-race` in that image.
  `TestAScanBecomesSearchable` and the signing tests are only meaningful there; the signing tests
  also need `pdfsig`, which ships with `poppler-utils`.
- **Generated clients.** After any change to a route or to a request or response type, regenerate
  `clients/` (the commands are in `docs/api.md`). `TestGeneratedClientsCoverEveryOperation` and
  `TestEveryRequestAndResponseHasAnExample` fail until both clients and the OpenAPI examples in
  `routes/apidocs.go` know the operation.
- **Looking at the UI.** `bun run typecheck` proves nothing about layout. Start Postgres, run
  `go run ./cmd/app migrate` and `serve` (not on port 3000, which Windows may reserve), and check
  the page at phone and desktop width in both themes. The assets are embedded, so rebuild with
  `bun run build` and restart after every frontend change.
- **CRLF on Windows.** `.gitattributes` forces LF. When generating files from a script, write bytes
  or pass `newline="\n"`; text mode writes CRLF and breaks `.sh` inside Docker.
- **Indentation.** Tabs, width 4, per `.editorconfig`. YAML, JSON and Markdown use spaces.
- **Commit messages.** commitlint enforces Conventional Commits and the scope list in
  `commitlint.config.js`, which mirrors `.claude/rules/commits.md`.
- **Engines are not optional in CI.** The `docker` job builds the image and runs `about`, which
  prints the resolved engine versions. If that job is green but a conversion test skipped locally,
  you have not tested the conversion.
