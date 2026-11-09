# Framework feedback rule

This project is a battle test for the vukem framework. Every vukem bug, missing feature, awkward
API, security issue or wrong/missing docs page you hit gets one entry in the single shared
`vukem-feedback.md` at the workspace root (the folder that holds `vukem/` and every app; include
the docs URL for docs findings). Never keep a copy inside this repository.

In a cloud session the workspace root does not exist: report the finding in the pull request
description instead, and the maintainer copies it to the queue.

Apply a local workaround, mark it in code with `// workaround(vukem): <entry title>`, and keep
going. Never edit the vukem repository from here.

**Before you file anything, verify the gap against the framework at the version `go.mod` pins** —
read the package in the module cache (`~/go/pkg/mod/github.com/!alex!galhardo/vukem@vX.Y.Z`), not
this repository. A `workaround(vukem)` comment is a *claim about the framework*, so confirming the
workaround is still in our code proves nothing about whether the gap is still open. Four agents in
one session re-reported `queue.ErrPermanent` as missing; it had shipped in the very version they
pinned, and one `grep` in the module cache would have shown it.

**The marker carries its own expiry.** Name the audit id in the prose, not only on the marker line:
`// workaround(vukem): A97 — a queue handler cannot report a permanent failure.` A stale marker's
*explanatory paragraph* is what gets believed — the one in `internal/jobs/pipeline.go` was specific,
gave a reason and explained a trade-off, which is exactly why nobody checked it. When a finding
closes, delete the whole comment block, not just the marker line.

At the end of every completed task group, also update the audit when the finding changes the
framework backlog, and [`docs/status.md`](../../docs/status.md) with what shipped and what is
next. See `ROADMAP.md`, section "Framework feedback".

**Closing a finding is as much of the job as opening one.** On 2026-10-19 all five
`workaround(vukem)` markers in the tree were checked against the pinned version and all five were
stale; two were costing the project real things — three path parameters had no description in the
published document, and a table of hand-written parameter notes existed for a gap that had
closed. Re-checking the markers costs one `grep` in the module cache per claim.
