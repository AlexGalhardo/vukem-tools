# Commit rules

- Conventional Commits 1.0, English: `type(scope): description`.
- Types: feat, fix, perf, refactor, docs, test, build, ci, chore, style, revert.
- Scope = area, lowercase: `auth`, `apikeys`, `jobs`, `upload`, `storage`, `image`, `video`,
  `audio`, `pdf`, `pixelsvg`, `sprite`, `archive`, `telemetry`, `downloader`, `editor`, `bot`,
  `api`, `ui`, `infra`, `ci`, `deps`, `repo`, `docs`, `release`.
- Description: imperative, lowercase start, no trailing period, header at most 100 characters.
- Breaking change: `!` after type/scope plus a `BREAKING CHANGE:` footer.
- Reference the ROADMAP task in the body: `Refs: S1.6`.
- One logical change per commit. Validated by the `commit-msg` hook (`bunx commitlint --edit`) and
  by CI on pull requests.
