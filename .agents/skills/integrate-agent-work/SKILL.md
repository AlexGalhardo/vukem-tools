---
name: integrate-agent-work
description: Check an epic a senior handed back - the diff, the scope, the acceptance criteria and the wide validation - before it counts as done, and fix the last bugs yourself.
---

# Integrate agent work

For the orchestrator, when a senior returns. A report is a claim, not evidence.

## 1. Scope

1. `git status --short` and `git diff --stat`. List every changed path.
2. Compare the list with the epic's Scope. Revert a change outside it, unless it is the minimal edit
   that keeps the build compiling. A reverted file is named in the review note.
3. Look for what an agent leaves behind: stray files, a changed `go.mod` or `go.sum`, a loosened
   lint or test configuration, new `//nolint`, skipped tests, `t.Skip` without a reason, debug
   output, running processes.

## 2. Evidence

1. Run the acceptance criteria that carry the risk, yourself, from the commands in the epic. Do not
   copy the senior's output.
2. Run the wide validation: `go build ./...`, `go vet ./...`, `golangci-lint run ./...`,
   `go test -race -count=1 ./...`.
3. Read the new tests. A test that cannot fail is not a test: break the code once and see it fail.
4. For anything security relevant (accounts, uploads, keys, engines, the local server), load
   `golang-security` and `vukem-tools-secure-endpoint`, or ask the `security-auditor` agent.

## 3. Decide

- All criteria hold and the wide suite is green: mark the epic `DONE` in the roadmap with the time.
- A small defect: fix it yourself, with a test, and say so in the status.
- A large defect: send it back with the failing command and its output.
- Not verifiable here (a real window, a real network, a real account): say so plainly in
  `docs/status.md`; never write "verified".

## 4. Afterwards

Stop the senior and any process it left (`TaskStop`), update `docs/status.md`, and tell the owner
what shipped and what did not.
