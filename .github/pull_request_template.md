## What

<!-- One paragraph. Link the issue. -->

## How to review

Findings are **Important**, **Nit** (at most five) or **Pre-existing**. An Important finding cites
`file:line` and is fixed or answered before merge.

## Checklist

- [ ] Conventional Commit, English, scope from `.claude/rules/commits.md`, header at most 100 characters
- [ ] `CHANGELOG.md` was not edited by hand
- [ ] Behaviour changes have a test that calls the shipped function, and `go test -race -count=1` of the affected packages passed
- [ ] UI changes passed `bun run typecheck`
- [ ] New routes, uploads, jobs and engines keep login, abilities and magic-byte detection
- [ ] No new dependency, and no version that is not exact, without the author's approval
- [ ] Framework friction is recorded in `development/feedbacks/vukem.md` (git-ignored), not in this repo
- [ ] A release change publishes the notes, the container image, and the `.exe`, `.AppImage` and `.dmg` artifacts from the tag workflow

## Evidence

<!-- Paste the command and the pass line. A description without the command is not evidence. -->
