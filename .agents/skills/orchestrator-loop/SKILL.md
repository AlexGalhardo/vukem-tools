---
name: orchestrator-loop
description: Run the work session as the orchestrator - pick the next epic, brief a senior, work the non-overlapping epics yourself, review what comes back, keep the roadmap true and close the day with the wide validation.
---

# Orchestrator loop

For the main session. Rules: `rules/orchestration.md` and `rules/plans.md`.

## 1. Start of the session

1. Read `docs/plans/00-roadmap.md` and `docs/status.md`. The roadmap is the state; after a context
   compaction, continue from it and never restart an epic from memory.
2. `git status --short` and `git fetch origin && git rev-list --left-right --count main...origin/main`.
3. Confirm with the owner what is not decided: scope, parallelism, commit policy.

## 2. The loop

```text
pick the first epic whose dependencies are DONE
if it needs product code:
    write the briefing (goal, files, acceptance, ruled out, constraints, validation)
    dispatch one named senior (senior-d, senior-e, ...) in the background
    meanwhile work an epic that does not overlap the senior's packages
else:
    do it yourself
when a senior returns:
    run integrate-agent-work
    update the status column and docs/status.md
    stop the agent and anything it left running
repeat until every epic is DONE or BLOCKED
then run qa-campaign
```

## 3. Briefing checklist

- The goal in one sentence and the path of the epic.
- The packages the senior may touch, and the ones it must not.
- What was already found, so it does not re-derive it, and what was ruled out.
- Constraints: no new dependency, no cgo, no commit, no push, no process left running.
- The validation commands, narrow then wide, and the evidence to return.
- The hand-off format.

## 4. Closing the day

1. `go build ./...`, `go vet ./...`, `golangci-lint run ./...`, `go test -race -count=1 ./...`,
   `bun run typecheck`, `bun run build`, `bun run lint:md`.
2. `docs/status.md`: what shipped, what was verified, what was not, and what is next.
3. `AGENTS.md` and `CLAUDE.md` updated if a command, a convention or the architecture changed.
4. `questions-to-dev.md` lists what only the owner can do.
5. Commits and pushes happen only when the owner asks.
