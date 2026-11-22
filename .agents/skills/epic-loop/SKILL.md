---
name: epic-loop
description: Work one epic of docs/plans/ from its first task to the hand-off, as the senior that owns it - one task, its Verify command, fix until it passes, next task - and stop at a STOP condition instead of improvising.
---

# Epic loop

For the senior that received an epic. The rule is `rules/plans.md`; this is the procedure.

## 1. Before the first task

1. Check the directory and branch the briefing names: `git rev-parse --show-toplevel` and
   `git branch --show-current`.
2. Read the whole epic, `AGENTS.md`, `.claude/rules/go.md` and every document the epic links.
3. **Check that the epic is still true.** Open the files, functions and commands it names. If one
   no longer matches the code, stop and report; do not adapt the plan silently.
4. Load the skills the briefing names (`golang-how-to`, `tdd`, `vukem-tools-verification`).
5. Record the starting point: `git rev-parse HEAD` and `git status --short`.

## 2. One task

```text
write the failing test first, and show it failing for the expected reason
do the task, and nothing beyond it
run its "Verify:" command, exactly as written
while it fails:
    read the output, find the cause, fix the cause, run it again
    after three honest attempts at the cause: STOP
run the narrow check of the package you changed (go test -race -count=1 ./<pkg>/...)
tick the task in the epic file only if the orchestrator's briefing says you may
```

- Fix the root cause. Never add `//nolint`, skip a test, silence an error or edit a check to make it
  pass. A flaky test is a defect: find the cause.
- No new dependency. If one seems necessary, STOP and report why.
- Stay inside the packages the epic names. A change outside them is the minimal edit that keeps the
  build compiling, and the hand-off says so.
- Leave no process running between tasks: no dev server, preview or watcher. A server you start for
  a check runs on a port you choose and is stopped by process id when the check ends.
- Do not commit, push, stash or reset unless the briefing explicitly allows commits.

## 3. After the last task

1. Run **every** acceptance criterion of the epic, in order, and keep the command and its result.
2. Run the wide validation: `go build ./...`, `go vet ./...`, `golangci-lint run ./...`,
   `go test -race -count=1 ./...`, plus `bun run typecheck`, `bun run build` and `bun run lint:md`
   when the change touched the frontend or a document.
3. Check your diff: `git diff --stat`. Revert anything outside the epic's scope.
4. Write the hand-off: files changed, tests added and their results, the evidence per acceptance
   criterion, what you could not verify, and what you found that is outside the epic.

## 4. STOP conditions

Stop and report, with the command and its output, when: the epic no longer matches the code; the
cause is not found after three attempts; the task needs a new dependency, a licence decision, a
secret, a production change or an architecture change; or a rule of this repository contradicts the
epic.
