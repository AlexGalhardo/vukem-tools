---
name: vukem-tools-release
description: Cut a vukem-tools release: generate the changelog with git-cliff, tag, push one tag at a time and verify the GitHub release actually published. Use when finishing a ROADMAP phase or when the owner asks for a release.
---

# vukem-tools release cycle

Order matters. Never skip a hook, never move a pushed tag, never hand-edit a released
`CHANGELOG.md` section. Rules: `.claude/rules/changelog.md`.

## 1. The gates must be green first

Run the `vukem-tools-verification` skill. A release off a red tree is not a release.

Also confirm, for the phase being closed, that every task in `ROADMAP.md` is `[x]` **and** its
quality asserts actually hold. An unchecked assert blocks the release.

## 2. Pick the version

SemVer, and below 1.0.0 a breaking change is a minor bump:

- `feat` commits since the last tag → minor.
- only `fix`/`perf`/`refactor` → patch.
- a `!` commit or a `BREAKING CHANGE:` footer → minor while below 1.0.0, major after.

The ROADMAP maps phases to versions: S1 → `v0.1.0`, S2 → `v0.2.0`, S3 → `v0.3.0`, S4 → `v0.4.0`,
S9 → `v0.6.0`. Several early phases went out inside `v0.1.0`, so `v0.2.0` to `v0.4.0` were later
added as tags on the commits that finished archives, image compression and sprite sheets; see
`docs/brainstorming.md` § 9.7. Never move a tag to renumber: add tags on the right commits.

## 3. Generate the changelog and commit

```bash
bun run changelog -- --tag vX.Y.Z   # git-cliff, then collapses the blank lines it leaves
git cliff --strip header vA.B.C..vX.Y.Z   # one version's notes, for gh release create
```

Read the diff. If an entry reads badly, the commit message was bad: that is a lesson for next
time, not a reason to edit `CHANGELOG.md`.

Bump `version` in `package.json` to `X.Y.Z` — CI checks that a `vX.Y.Z` tag matches it.

```bash
git switch -c chore/release-vX.Y.Z
git add CHANGELOG.md package.json
git commit -m "chore(release): vX.Y.Z"
git push -u origin chore/release-vX.Y.Z   # then a pull request, merged once CI is green
```

The release commit goes through a pull request like everything else; tag the merge result on
`main`, not the branch.

## 4. Tag, one at a time

```bash
git tag -a vX.Y.Z -m "vX.Y.Z"
git push origin vX.Y.Z
```

**One tag per push.** GitHub creates no push event when more than three tags go up in a single
push, so `release.yml` silently never runs.

## 5. Verify that it published

```bash
gh run list --workflow=release.yml --limit 3
gh release view vX.Y.Z --json tagName,body
```

The body must be the matching `CHANGELOG.md` section, not the `See CHANGELOG.md` fallback. If the
workflow did not start, re-run it with `workflow_dispatch`; if it started and failed, fix the cause
and re-run. Only as a last resort publish by hand:

```bash
gh release create vX.Y.Z --verify-tag --notes-file <section>
```

## 5b. A tag added after the fact

`release.yml` runs on every tag push, builds a container image from the tagged commit and then
looks for a changelog section in that commit's own `CHANGELOG.md`. A back-filled tag points at a
commit that does not have the section, so the run builds the image and then fails at that step.
Push such tags one at a time, accept the red run, and publish the release by hand with the notes
`git cliff` generates for the range, `--latest=false` so the newest version stays "Latest":

```bash
git cliff --strip header vPREV..vTAG > notes.md
gh release create vTAG --verify-tag --title vTAG --notes-file notes.md --latest=false
```

GitHub lists releases newest first by creation date, so create them oldest first and the newest
last; recreate the newest release (the release only, never the tag) if the order came out wrong.
Do not disable the workflow to avoid the red run.

## 6. Close the loop

- Append anything the framework got wrong to `../framework-feedback/vukem-tools-feedback.md`, and
  update `../framework-feedback/audit.md` when it changes the framework backlog.
- Update `../status.md` with what shipped and what is next.
- Update `.claude/skills/` and `.claude/rules/` with what the workflow actually turned out to be.
