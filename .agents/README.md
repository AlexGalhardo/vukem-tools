# Agent rules and skills

This folder is for the people and agents who work on this repository. It is tool-neutral: Claude
Code reads it through `CLAUDE.md`, other agents through `AGENTS.md`. It adds the workflow (plans,
orchestration, QA). The code rules stay where they are, in `.claude/rules/` (Go, engines, bot,
commits, CI, changelog, markdown), and are not repeated here.

- `rules/`: workflow rules that always apply. Read them before you start.
- `skills/`: step-by-step procedures, one folder each with a `SKILL.md`. Load the one that matches
  the task.

| Rule | What it says |
| --- | --- |
| `rules/plans.md` | The epics of `docs/plans/`, the unit of work, and how an epic is closed |
| `rules/orchestration.md` | How the orchestrator, the seniors and the intern work together |
| `rules/git-and-branches.md` | Branches, commits, what is never done without the owner |
| `rules/commits.md` | Pointer to `.claude/rules/commits.md` |
| `rules/ci.md` | Pointer to `.claude/rules/ci.md` |
| `rules/naming.md` | Names of epics, QA records and findings |

| Skill | Use it when |
| --- | --- |
| `skills/epic-loop/` | You are a senior working one epic from its first task to the hand-off |
| `skills/orchestrator-loop/` | You are the orchestrator running the day: pick, brief, review, close |
| `skills/integrate-agent-work/` | An epic came back and must be checked before it counts as done |
| `skills/qa-campaign/` | The epics are done: three seniors test end to end and fix what they find |
| `skills/verify/` | Before every hand-off: the narrow command, then the wide one |
| `skills/ci-preflight/` | The checks CI runs that a Windows machine does not show by itself |
| `skills/release-pr/` | A release is requested; it routes to `vukem-tools-release` |

The documents that define the product stay in `docs/` and `PRODUCT.md`; this folder never overrides
them. Decisions are final unless `docs/brainstorming.md` is updated first.
