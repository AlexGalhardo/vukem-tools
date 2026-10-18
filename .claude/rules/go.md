# Go rules

- Load the `golang-*` skills for Go work; `golang-security` for accounts, payments, uploads,
  webhooks and API keys; `golang-performance` and `golang-benchmark` for any conversion path.
- Stream, never buffer a whole file: `io.Copy` to disk or object storage, `bufio` writers for
  generated output, shrink-on-load when decoding images.
- Bound concurrency explicitly, sized from the container's CPU quota rather than the host's CPU
  count.
- Strong typing on exported APIs: explicit return types, no `any` where a type exists.
- Fix the root cause. Never silence an error, skip a test or add `//nolint` to make a check pass.
- Measure before claiming: `go test -bench . -benchmem`, and record wall-clock and peak RSS per
  conversion kind in `docs/benchmarks.md`.
- Validate with the narrowest command (one package, one spec) before claiming done, and show the
  command and its result.
