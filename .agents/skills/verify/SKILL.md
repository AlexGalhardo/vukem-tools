---
name: verify
description: Validate a change before a hand-off - the narrow command while iterating, then the wide one that reaches the cross-cutting gates - and report the command and its result. Routes to vukem-tools-verification for the full checklist.
---

# Verify

The full checklist is the `vukem-tools-verification` skill: load it. This is the short form.

## 1. While iterating

The narrowest command that covers the change, and show its result:

```bash
go test -race -count=1 ./internal/<pkg>/...
bun test resources/js/lib/dev          # browser tools' logic
bunx markdownlint-cli2 "<changed>.md"  # a document
```

## 2. Before handing back

The wide command is a separate, required step. `./internal/...` plus `./app/...` looks like the
suite and is not: `./tests/` holds the OpenAPI example gate, the generated-client coverage gate and
the HTTP hardening asserts.

```bash
go build ./... && go vet ./... && golangci-lint run ./...
go test -race -count=1 ./...
bun run typecheck && bun run build     # when the frontend changed
bun run lint:md                         # when a document changed
```

## 3. Reporting

State each command and its result in one line. If a step was skipped, say so and why. If a test
skipped because an engine is absent, say which one. Never write "verified" for something you could
not run (a real window, a real network, a real account).

## 4. Never

Edit a check, lower a threshold, add `//nolint`, skip a test or silence an error to make a command
pass.
