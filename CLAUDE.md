# CLAUDE.md

@AGENTS.md

## Claude Code notes

- On any Go task, load `golang-how-to` first; it routes to the right `golang-*` skill.
  `golang-security` for accounts, uploads, API keys and webhooks; `golang-performance` and
  `golang-benchmark` for any conversion path.
- Load `impeccable` and `frontend-design` before any UI work, and `web-design-guidelines` to audit
  it. The target is a modern, minimal, easy interface, in English only.
- Load `karpathy-guidelines` always, and `tdd` before writing a feature (it wins over
  `test-driven-development` when they disagree).
- The project's own skills: `vukem-tools-verification` before calling anything done,
  `vukem-tools-secure-endpoint` when touching routes, uploads, jobs or engines, and
  `vukem-tools-release` when cutting a release.
- Path-scoped rules in `.claude/rules/` load automatically; `framework-feedback.md` applies
  everywhere.
- `.claude/settings.local.json` is owner-local and git-ignored (it disables Claude Code hooks); do
  not edit or commit it.
- Railway MCP and CLI are available for deploy tasks; confirm destructive actions with the owner
  first, and never change production configuration without saying what will change.
- Answer the owner in Portuguese (pt-BR); code, comments, docs, UI and commits stay in English.
- Validate with the narrowest command (one Go package, one spec) before claiming done, and show the
  command and its result.
