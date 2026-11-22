---
name: qa-campaign
description: Run the end-to-end QA campaign after the epics - three seniors test the whole product in parallel as a human QA would, each on its own area, every problem becomes a finding, the bugs are fixed with a regression test, and a new round runs until one finds nothing.
---

# QA campaign

Starts when every epic of `docs/plans/00-roadmap.md` is `DONE` or `BLOCKED`. Run by the orchestrator.
This is the only place where seniors run in parallel, in the same directory, with disjoint areas.

## 1. The loop

```text
round n:
    build once, from the current tree
    three seniors test in parallel, each on its own area
    each records findings in development/feedbacks/vukem-tools.md and fixes them with a test first
    the orchestrator reviews every diff and re-runs a sample of five findings
    if the round found nothing new of severity high or medium: the campaign is over
round n+1: again, on the new tree, the whole product
```

A fix can break something else, so a round always tests the whole product again, not only what was
fixed.

## 2. The three areas

| Senior | Area | Covers |
| --- | --- | --- |
| `senior-a` | Web and accounts | Sign-up, sign-in, sign-out, keys, quota, every page of the catalogue, the 17 browser tools, phone width, dark mode, accessibility |
| `senior-b` | Desktop and offline | The desktop app with the network off, the data directory, start and stop, two instances, upgrade of an existing database |
| `senior-c` | CLI, API and MCP | Every pipeline tool through the CLI and MCP, bad input, limits, expired results, the OpenAPI reference |

Every line of an inventory belongs to exactly one senior. Adjust the split when an area is much
larger, and write the split used into the round's record.

## 3. What a senior does

- Test like a human: use the product through its real surfaces (a browser, the window, the CLI),
  not by reading the source to explain a defect away.
- Record each finding with an id (`QA-A1`), steps, expected, actual, severity and status.
- Fix a bug with a failing test first, then the cause, in its own change. A fix outside the senior's
  area goes to the orchestrator.
- Do not add features. A request for one is logged, not built.
- Leave no process running when finished.
- Say what was not tested and why.

## 4. Severity

High: data loss, a security hole, the app does not start, a tool produces a wrong or empty result.
Medium: a flow is broken or confusing but has a workaround. Low: cosmetic or copy. The campaign ends
when no high or medium finding is open.

## 5. Orchestrator

1. Brief each senior (area, files, acceptance, constraints, evidence format).
2. After each: `git diff --stat`, revert what is out of scope, re-run a sample, run the wide
   validation.
3. Stop the agents and every process they started. Verify no listener is left on the dev ports.
4. Write the result into `docs/status.md`: what was tested, what was not, the open findings.
