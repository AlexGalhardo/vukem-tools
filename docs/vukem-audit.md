# vukem audit

What this project has learned about the [vukem](https://vukem.dev) framework, and what the
framework's backlog should take from it.

This file lives in the repository rather than in `../framework-feedback/`, which
`.claude/rules/framework-feedback.md` names: that directory is a sibling checkout on the owner's
machine and does not exist in a cloud session, so anything written there is lost when the
container is reclaimed. Copy these entries across when working locally.

## How an entry gets written

The rule is short and it is the whole point of this file:

> **Verify the gap against the framework at the version `go.mod` pins** — read the package in the
> module cache, not this repository.

A `workaround(vukem)` comment is a *claim about the framework*. Confirming the workaround is still
in our code proves nothing about whether the gap is still open. The rule records that four agents
in one session re-reported `queue.ErrPermanent` as missing when it had shipped in the very version
they pinned.

## 2026-10-19 — the audit that found no open findings

Pinned version: **vukem v0.30.0**, read from
`~/go/pkg/mod/github.com/!alex!galhardo/vukem@v0.30.0`.

Every `workaround(vukem)` marker in the repository was checked against that source. **All five
were stale, and two of them were wrong in a way that was costing the project something.** There
are now zero markers in the tree.

| # | The claim in our code | Verified against v0.30.0 | Outcome |
| --- | --- | --- | --- |
| A1 | `internal/jobs/reaper.go`: "a queue handler cannot report a permanent failure" | `queue.ErrPermanent` is declared in `queue/attempt.go:20`, and `internal/jobs/pipeline.go` **already uses it**. `queue.Observer` also reports `JobFailed` with `Final` set when the queue gives up | **Closed.** The Reaper stays; its real justification is a worker that dies before it can settle a row, which no queue feature can fix. The comment now says that |
| A2 | `apiv1/tokens.go` and `apiv1/conversions.go`: "path parameters ignore the `doc` tag" | `openapi.pathParams` sets `Description` and `Example` from the matching field. Probed empirically on a parameter the app does not override: the tag **is** honoured | **Closed, and it was costing us.** The tags moved back onto the types and `parameterNotes` was deleted. Three path parameters that had no description now have one |
| A3 | `routes/apidocs.go`: "`openapi.MediaType` carries a schema and nothing else, and no struct tag feeds an example" | `MediaType` has `Example` and `Examples`; `Field.ExampleValue` reads an `example:"…"` tag | **Claim closed, migration outstanding.** See the follow-up below |
| A4 | `routes/apidocs.go`: "`App.OpenAPI()` has no hook to add a security scheme" | `vukem.WithOpenAPI(openapi.Info{…})` takes `SecuritySchemes`. This app has simply never passed it | **Claim closed, migration outstanding.** See the follow-up below |
| A5 | `tests/bot_test.go`: "`vukemtest.Response` has no decode-into-struct helper" | `(*Response).JSON(v any)` exists, and its own doc comment recommends it *instead of* `json.Unmarshal([]byte(res.Body()), &v)` | **Closed.** Both call sites now use it |

One more wrong statement about the framework, carrying no marker:
`internal/jobs/pipeline.go` claimed vukem's `AttemptFrom` doc comment "claims the opposite" about
the zero `Attempt`. It does not — v0.30.0 documents exactly the behaviour the code relies on. The
comment is corrected.

### What the framework backlog takes from this

**Nothing.** No open bug, no missing feature and no wrong documentation page was found. Every gap
this project had recorded against vukem has been closed by the framework, and in two cases the
replacement is better than the workaround.

That is the finding, and it is worth stating plainly rather than padding the table: a
battle-testing project whose entire feedback list has been closed should say so, and should stop
carrying the workarounds.

### Follow-up work this repository owes itself

Neither is a framework gap; both are this app's code predating a framework feature.

- **A3 — move body examples onto `MediaType.Example`.** They currently go into the schema's JSON
  Schema `examples` keyword. Moving them means changing `hasExample` in `tests/openapi_test.go`
  and every example site in the same commit, which is a repo-wide sweep. The present shape is
  correct OpenAPI and the document is valid either way.
- **A4 — declare both security schemes through `vukem.WithOpenAPI`.** Every `api.Op` would then
  name `apiKey` directly instead of having it attached to the finished document by
  `describeSecurity`. It touches a security-relevant surface for a cosmetic gain, so it wants its
  own change rather than a drive-by.

### Things that worked, recorded so they are not re-litigated

Checked while building the archive, compression and sprite tools, and the mobile header:

- `vukem.DefaultMiddlewareWith` with `MiddlewareOptions.SecureHeaders` is the supported way to add
  a CSP source, and `CSPSources` accepts `blob:` in `img-src` — which `docs/security.md` and
  `site/content/docs/security/security-headers.mdx` both already document. No finding.
- `schema.Table` alters a table and `DropColumn` reverses it; two migrations landed on it.
- `z.FieldError` produces the 422-on-a-named-field shape every submit route needs.
- `api.Register`'s typed handlers derive their schema from the Go type, which is why each tool has
  its own request struct. Already documented in `app/controllers/apiv1/pdf.go`; still true.
- `vukemtest` builds the whole app per test, including the engine probe. Two real bugs in **this**
  repository surfaced through it; see `docs/benchmarks.md` and the commits of 2026-10-19.
