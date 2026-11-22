---
name: release-pr
description: Release request entry point. This project releases with git-cliff, an annotated tag and a GHCR and Docker Hub image, not with a release train; it routes to vukem-tools-release.
---

# Release

This project does not use a release train or a long-lived release branch. A release is:

1. Preflight: the wide validation of `skills/verify/` is green and `main` is in sync with
   `origin/main`.
2. `git cliff --tag vX.Y.Z -o CHANGELOG.md`, reviewed; a badly written entry means a commit message
   to fix, not a hand edit.
3. `chore(release): vX.Y.Z`, then an annotated tag `vX.Y.Z`.
4. Push the commit and the tag; push version tags one at a time.
5. Verify on the remote: `gh run list --workflow=release.yml`, `gh release view vX.Y.Z`, and the
   image tags `X.Y.Z`, `X.Y` and `latest` on GHCR and Docker Hub.

Load the `vukem-tools-release` skill for the procedure and its traps (it is the source of truth).
Never tag, push or publish without the owner's explicit request, and never move a pushed tag.
