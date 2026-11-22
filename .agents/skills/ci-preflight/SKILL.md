---
name: ci-preflight
description: Catch before CI what a Windows machine does not show - Linux-only behaviour, the container image, pinned actions, audit gates and commit lint - so a push does not fail for something knowable.
---

# CI preflight

CI runs on Linux; the owner's machine is Windows 11. Things that pass here and fail there, and the
other way round:

| Difference | Check |
| --- | --- |
| Engines (ffmpeg, qpdf, libvips, LibreOffice) absent on Windows | Engine tests skip with a visible reason; run them in the project container before a release |
| Path separators, case-sensitive file names, CRLF | Use `filepath`, never concatenate paths; keep `.editorconfig` line endings |
| Process groups and `setrlimit` | Linux only; tests for them carry a build tag and a visible skip |
| Network namespaces for engines | Not testable on Windows; the documentation says so |
| Pinned actions | Every third-party action is a 40-character SHA (`.claude/rules/ci.md`) |
| `bun audit --audit-level=high` | Run it; an advisory with no fix is ignored by GHSA id with a reason |
| Commit lint | `echo "<message>" \| bunx commitlint` before committing |
| Image build | `docker build` of the pinned image when the Dockerfile changed; pins checked with `apt-cache policy` |

## Procedure

1. List what changed: `git diff --stat`.
2. For each row above that applies, run its check or say it was not run and why.
3. Run the workflow's own commands locally where possible (`go build ./...`, `go vet ./...`,
   `golangci-lint run ./...`, `go test -race -count=1 ./...`, `bun run build`, `bun run lint:md`).
4. After a push (only when the owner asked), watch the run with `gh run list` and `gh run view`;
   do not call a change done before CI is green.
