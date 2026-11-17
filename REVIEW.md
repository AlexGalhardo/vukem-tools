# Review rules

What a reviewer (a person or an agent) checks in a pull request of Vukem Tools. The author owns
this file. It is read from the default branch, never from the pull request under review, and a
change to it needs the author's review (`.github/CODEOWNERS`).

## Severity

Every finding has one severity, cites `file:line`, and is fixed or answered with a reason before
merge.

- **Important:** a bug, a regression, a security issue, a broken rule in `AGENTS.md`, or a missing
  test for a behaviour change.
- **Nit:** naming, wording, or small style points the linters do not catch. At most five per
  review. Skip them when there are Important findings.
- **Pre-existing:** a problem the change did not introduce. Report it. It does not block the merge.

## What is a failure here

1. A conversion that buffers a whole file, trusts the extension or the client's `Content-Type`, or
   calls an engine through cgo instead of a subprocess with a timeout, a process-group kill and
   `setrlimit`.
2. A new route, upload, job or engine invocation that skips login, an API-key ability, or magic-byte
   detection.
3. A new dependency, or a version that is not exact, without the author's approval.
4. A skipped test, a silenced error, or a `//nolint` added to make a check pass.
5. `CHANGELOG.md` edited by hand. git-cliff writes it from Conventional Commits.
6. A commit header that is not a Conventional Commit of at most 100 characters, or a scope outside
   `.claude/rules/commits.md`.
7. A change to the checks themselves made in the same pull request as the code they judge. That
   covers this file, `.github/workflows/`, `.github/CODEOWNERS` and `.golangci.yml`. Split it, and
   have the author review the checks.
8. The launcher installing an artifact whose checksum was not checked, or moving an install to an
   older release.
9. The desktop shell reimplementing a tool instead of opening the same web UI.
10. No `go test -race -count=1 ./...` evidence for a Go behaviour change, or no `bun run typecheck`
    for a UI change.

## Paths to skip

Generated clients under `clients/` and Vite output under `resources/public/build/`: check only that
they match their generator.

## Who approves

- An agent's approval never counts as a person's.
- An approval covers the commit it was given on.
- Changes to auth, the update feed, CI or release workflows, or the checks listed above need the
  author's review.
