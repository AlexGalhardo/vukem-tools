# Naming

- **Epics:** `docs/plans/NN-<slug>.md`, two digits, lowercase kebab slug, numbered in the order of
  dispatch. The roadmap is `00-roadmap.md`. A number is never reused.
- **QA findings:** `QA-<area letter><n>` in `development/feedbacks/vukem-tools.md`, for example
  `QA-A3` (A web and accounts, B desktop and offline, C CLI, API and MCP). The id never changes
  after it is written; a fixed finding keeps its id and gets a status and the regression test.
- **Framework friction:** `VF-NNN` in `development/feedbacks/vukem.md`, one entry when the friction
  appears, never in a batch at the end.
- **Roadmap tasks:** the `S<phase>.<task>` ids of `ROADMAP.md`, as they are.
- **Decisions:** numbered sections of `docs/brainstorming.md`; cite the section, not a copy.
- **Go packages:** short, lowercase, no underscores, under `internal/` unless an adapter needs to
  import them from `app/`.
- **Everything is English:** code, comments, docs, UI and commits. There is no i18n layer; the
  Brazilian Portuguese pages of the documentation site are the only translation.
