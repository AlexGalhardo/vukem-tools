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
- **CRLF on Windows.** `.gitattributes` forces LF. When generating files from a script, write bytes
  or pass `newline="\n"`; text mode writes CRLF and breaks `.sh` inside Docker.
- **Indentation.** Tabs, width 4, per `.editorconfig`. YAML, JSON and Markdown use spaces.
- **Commit messages.** commitlint enforces Conventional Commits and the scope list in
  `commitlint.config.js`, which mirrors `.claude/rules/commits.md`.
- **Engines are not optional in CI.** The `docker` job builds the image and runs `about`, which
  prints the resolved engine versions. If that job is green but a conversion test skipped locally,
  you have not tested the conversion.
