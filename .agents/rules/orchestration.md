# Orchestration

The team is one orchestrator (the main session), seniors and interns. The agent definitions are the
named copies `senior-a` to `senior-e` (Sonnet) and `estagiario-1`, `estagiario-2` (Haiku), plus the
review agents of `.claude/agents/` (`code-reviewer` is the specialist, `security-auditor`,
`test-engineer`).

1. A delegated engineer's report is a claim, not evidence. The orchestrator reads the diff and
   re-runs the acceptance criteria that carry the risk before an epic counts as done.
2. A senior owns one epic at a time and only the packages it names. A change outside them is
   reverted or sent back, unless it is the minimal edit that keeps the build compiling, and the
   hand-off says so. Run `git diff --stat` after every agent.
3. One senior implements at a time. The orchestrator works the documentation and verification
   epics in packages that do not overlap. Three seniors run in parallel only in the QA campaign,
   in the same directory, with disjoint areas. No worktrees without an explicit yes from the owner
   for that task.
4. Dispatch by the named type, never the generic `senior` or `estagiario`, and never repeat a name
   among living agents.
5. The briefing carries the goal, the files, the acceptance criteria, what was already ruled out,
   the constraints (no new dependency, no cgo, no commit, no push) and the validation commands.
   Repeat it in every briefing: no process may be left running.
6. Stop an agent as soon as its delivery is reviewed, together with the processes it left
   (`TaskStop`; dev servers, previews, watchers).
7. Use the intern for mechanical, checkable work: running a suite and reporting, reproducing a bug,
   fixtures, lookups. It writes no product code.
8. Act on every security finding before an epic counts as done. If one does not apply, write down
   why.
9. A decision only the owner can take (money, licence, secret, architecture, scope, production) is
   asked with one question at a time, with a recommended option first, and recorded in
   `docs/brainstorming.md` in the same change as the work it unblocks.
10. Never wait idle. While a senior works, the orchestrator prepares the next briefing, keeps
    `docs/status.md` and the roadmap true, and checks the build of what was merged.
11. Context past 75%: update the roadmap and `docs/status.md` with the current state, then
    `/compact`. After a compaction, read those first and continue from them.
12. Only the orchestrator commits, on the release branch of the open draft pull request
    (`.agents/skills/release-pr/`). Merging, tagging, publishing and deploying wait for the owner,
    and production changes and destructive actions are confirmed first. The git hooks run on the
    whole tree (`go vet ./...` before a commit, the race tests and both builds before a push), so
    commit only when the tree is green, and never with `--no-verify`.
13. Every vukem friction goes into `development/feedbacks/vukem.md` (`VF-NNN`, format in the file),
    when it appears, after checking the gap against the module cache at the version `go.mod`
    pins. QA bugs go into `development/feedbacks/vukem-tools.md`. The owner chose these two files
    on 2026-11-22; they replace the workspace-root `vukem-feedback.md` of
    `.claude/rules/framework-feedback.md` for this work. Repeat it in the briefing.
