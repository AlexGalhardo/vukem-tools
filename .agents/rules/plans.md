# Plans

The unit of work is an epic: one file of `docs/plans/` named `NN-<slug>.md`. `docs/plans/00-roadmap.md`
lists them, their order, their dependencies and their status. `ROADMAP.md` (the long list of every
tool) is the product backlog; the epics are how a given day or phase is closed.

## 1. Format of an epic

Sections, in this order: Goal, Context and related decisions, Scope, Out of scope, Dependencies,
Technical design, Tasks, Test strategy, Acceptance criteria, Risks and mitigation, Size.

- **Tasks** are checkboxes. Each ends with `Verify:` and a command or an observation, never an
  opinion. A task whose `Verify:` names a test that does not exist yet is asking you to write it.
- **Acceptance criteria** are numbered, each checkable with a command. They are the definition of
  done for the epic.
- **STOP** conditions live under Risks: a situation in which the senior stops and reports instead
  of improvising.
- Size is S (under one hour), M (one to two hours), L (two to four hours). Larger is split.

## 2. Status

`TODO`, `IN PROGRESS (<agent>, <time>)`, `BLOCKED (<reason>)`, `DONE (<time>, <commits>)`. Only the
orchestrator writes the status column of `00-roadmap.md`.

## 3. Closing an epic

1. The senior runs every acceptance criterion and keeps the command and its result.
2. The senior runs the narrow validation, then `go test -race -count=1 ./...` (the wide command;
   `./tests/` holds cross-cutting gates the narrow one never reaches).
3. The orchestrator reads the diff, runs `git diff --stat` and reverts what is outside the epic's
   scope, then re-runs at least the acceptance criteria that carry the risk.
4. A report is a claim, not evidence. The epic is `DONE` only after step 3.
5. `docs/status.md` is updated with what shipped and what is next.

## 4. Before starting

Check that the epic is still true: open the files, functions and commands it names. If one no
longer matches the code, stop and report; the orchestrator refreshes the epic first. Never adapt
the plan silently.

## 5. Where plans live

Plans go only in a versioned path: `docs/plans/`. The root `plans/` folder is the output of the
`improve` skill and is excluded by `.git/info/exclude` (the pattern is anchored as `/plans/` so it
does not hide `docs/plans/`). Check both before writing a plan anywhere else.
