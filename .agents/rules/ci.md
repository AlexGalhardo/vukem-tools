# CI

The rule is `.claude/rules/ci.md`: every third-party action pinned to a full commit SHA with the
version in a trailing comment, `persist-credentials: false` unless a step pushes, the narrowest
`permissions:`, pinned installs, and `bun audit --audit-level=high` as a gate with advisories
ignored only by GHSA id and a written reason. Apply it before moving any workflow template into
`.github/workflows/`. Do not copy the rule here; change it there.
