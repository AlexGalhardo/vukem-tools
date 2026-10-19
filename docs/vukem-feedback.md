# vukem feedback

The running log this project owes the framework, in the format
`.claude/rules/framework-feedback.md` describes: one entry per vukem bug, missing feature, awkward
API, security issue or wrong documentation page, each with its reproduction and, for a docs
finding, the page's URL.

It lives here rather than in `../framework-feedback/vukem-tools-feedback.md`, which that rule
names, because the sibling checkout does not exist in a cloud session and is not under version
control from here. Copy entries across when working locally. `docs/vukem-audit.md` is the summary
that drives the framework's backlog; this is the log.

## Open

**None.**

Every entry this project had carried was re-checked against **vukem v0.30.0** on 2026-10-19 and
closed. The table of what was checked, and how, is in
[docs/vukem-audit.md](vukem-audit.md).

An empty list is a result, not an omission: this repository exists partly to push the framework,
and the honest report is that the framework has caught up with everything pushed at it so far.

## Closed on 2026-10-19, against v0.30.0

Kept rather than deleted, because the next session needs to know these were investigated and when,
so they are not re-reported a sixth time.

### queue: a handler cannot report a permanent failure

**Closed — it could, and this app already did.** `queue.ErrPermanent` is declared in
`queue/attempt.go` and `internal/jobs/pipeline.go` wraps its permanent failures in it.
`queue.Observer` additionally reports `JobFailed` with `Final` when the queue gives up on a job,
which is a cleaner signal than the sweep the app uses.

Reproduction of the closure: `grep -rn ErrPermanent` in the module cache.

### openapi: a path parameter loses its `doc` tag

**Closed — it keeps it.** `openapi.pathParams` fills `Description` and `Example` from the input
type's field whose JSON key matches the parameter name.

Reproduction of the closure: add `doc:"…"` to `ConversionIDInput.ID`, request `/openapi.json`,
and the description is in the document. A first attempt to reproduce it on
`RevokeInput.Token` appeared to confirm the bug and did not: this app's own `parameterNotes`
machinery was overwriting the description afterwards. Worth recording, because the confounded
probe is how a closed finding stays open for a year.

### openapi: `MediaType` carries a schema and nothing else, and no tag feeds an example

**Closed on both halves.** `openapi.MediaType` has `Example` and `Examples` (OpenAPI Example
Objects), and `Field.ExampleValue` decodes an `example:"…"` struct tag.

This app still writes its examples into the schema's JSON Schema `examples` keyword. That is now a
migration this repository owes itself, not a limitation; `docs/vukem-audit.md` records it.

### openapi: `App.OpenAPI()` offers no way to declare a security scheme

**Closed.** `vukem.WithOpenAPI(openapi.Info{…})` takes `SecuritySchemes`, and `App.OpenAPI()`
builds from it. The app has never passed the option.

### vukemtest: `Response` has no decode-into-struct helper

**Closed.** `(*Response).JSON(v any) *Response` exists, fails the test when the body does not
decode, and its doc comment recommends it over `json.Unmarshal([]byte(res.Body()), &v)` for
exactly the reason this project had hand-rolled: the error branch is easy to omit, and without it
a body that did not decode reads as a zero value the test then asserts against.

### Not a finding: the default CSP refuses `blob:` images

Raised while building the sprite pages, where previewing a dropped file through
`URL.createObjectURL` was blocked, and **withdrawn after reading the docs**. The strict default is
deliberate, `SecureHeadersConfig.CSPSources` is the supported way to widen it, and `blob:` in
`img-src` is explicitly listed as an accepted scheme source in both
`docs/security.md` and `site/content/docs/security/security-headers.mdx`.

Recorded so it is not raised again.
