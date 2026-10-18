# CI and workflow rules

- Pin every third-party GitHub Action to a full 40-character commit SHA, with the human-readable
  version in a trailing comment: `uses: owner/action@<sha> # v1.2.3`. Tags are mutable, so a tag
  pin is a supply-chain hole. Actions published by GitHub itself (`actions/*`) may use a major tag.
- `actions/checkout` sets `persist-credentials: false` unless a step genuinely pushes.
- Grant the narrowest `permissions:` block a job needs. Never leave `id-token: write` on a job
  that does not use OIDC.
- Pin any package a workflow installs (`npx pkg@version`), and pin an external skills or script
  source to a commit, never to a branch.
- Workflow templates under `.claude/skills/**/assets/` are **not** executed by GitHub Actions, but
  they are copied by hand. Apply every rule above before moving one into `.github/workflows/`.
- `bun audit --audit-level=high` gates the build. When an advisory has **no fixed release**, do not
  lower the level and do not drop the step: ignore that one advisory by its GHSA id, with a comment
  saying what it is, why it is acceptable here (what reaches it, and whether it ships), and when to
  remove the ignore. Re-check on every dependency bump.
