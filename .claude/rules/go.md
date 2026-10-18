# Go rules

- Load the `golang-*` skills for Go work; `golang-security` for accounts, payments, uploads,
  webhooks and API keys; `golang-performance` and `golang-benchmark` for any conversion path.
- Stream, never buffer a whole file: `io.Copy` to disk or object storage, `bufio` writers for
  generated output, shrink-on-load when decoding images.
- Bound concurrency explicitly, sized from the container's CPU quota rather than the host's CPU
  count.
- Strong typing on exported APIs: explicit return types, no `any` where a type exists.
- Fix the root cause. Never silence an error, skip a test or add `//nolint` to make a check pass.
  The one exception is a linter false positive: then `//nolint:<linter> // <rule>: <why it is not
  a finding>` on that line, naming the rule and the reason. A bare `//nolint`, or one whose comment
  restates the rule instead of refuting it, is silencing.
- Measure before claiming: `go test -bench . -benchmem`, and record wall-clock and peak RSS per
  conversion kind in `docs/benchmarks.md`.
- Validate with the narrowest command (one package, one spec) while iterating, and show the command
  and its result.
- **Then run the wide command before handing back a task group: `go test -race -count=1 ./...`.**
  This is a separate, required step, not the narrow rule repeated. `./internal/...` plus
  `./app/...` *looks* like the suite and is not: `./tests/` holds the cross-cutting gates — the
  OpenAPI example gate, the generated-client coverage gate, the HTTP hardening asserts — and a
  domain author cannot see them from inside their own packages. A phase ran ten commits green on
  the narrow command with `./tests/` red the whole time.
