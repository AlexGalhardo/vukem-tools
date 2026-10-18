# API overview

The JSON API of Vukem Tools. This page is the hand-written orientation; the generated reference is
the source of truth for every field.

- Reference (Scalar): [`/docs`](http://localhost:3000/docs)
- Document (OpenAPI 3.1): [`/openapi.json`](http://localhost:3000/openapi.json)

Both are public and need no key: they describe a public API and contain no secret. The document is
generated from the route table on every request, so it cannot describe a route the app does not
serve, and `tests/openapi_test.go` fails CI when a route under `/api/v1` is missing from it.

Everything lives under `/api/v1`. `info.version` of the document comes from `API_VERSION`.

## Authentication

Every route except `POST /api/v1/tokens` and `GET /api/v1/abilities` needs an API key. Send it
either way; `Authorization` wins when both are present, so a stale `X-API-Key` in a shell profile
cannot override a deliberate key.

```bash
curl -H "Authorization: Bearer $VUKEM_KEY" http://localhost:3000/api/v1/me
curl -H "X-API-Key: $VUKEM_KEY"            http://localhost:3000/api/v1/me
```

Get a key by exchanging your account's credentials for one. The account's email address must be
verified: a key is a login, and an unverified address may belong to someone else.

```bash
curl -X POST http://localhost:3000/api/v1/tokens \
  -H "Content-Type: application/json" \
  -d '{"email":"you@example.com","password":"...","device_name":"cli",
       "abilities":["image:convert","pdf:compress"]}'
```

The response carries the secret **once**; only its SHA-256 hash is stored. Lose it and you issue
another. Keys expire 30 days after they are issued.

```json
{
  "id": "k_7f3c1b9e",
  "token": "vk_7f3c1b9e.replace-this-with-the-key-you-receive",
  "type": "Bearer",
  "abilities": ["image:convert", "pdf:compress"],
  "expires_at": "2026-02-04T10:00:00Z"
}
```

`GET /api/v1/tokens` lists your keys (newest first, revoked ones included, never the secrets).
`DELETE /api/v1/tokens/current` revokes the key that made the request; `DELETE /api/v1/tokens/{id}`
revokes one by id. Revoking is scoped to the caller: another account's id is answered exactly like
an id that does not exist, so the response never says whether the id was real.

## Abilities

A key is never a blanket credential. It carries exactly the abilities asked for, there is no
default and no wildcard, so revoking one tool is never a surprise. Ask for the narrowest set the
script needs.

`GET /api/v1/abilities` publishes the closed set, so a client does not hardcode it. An ability the
server does not define is a `422` on the `abilities` field naming what was wrong.

## Job lifecycle

Planned, not built yet (ROADMAP S1.6 and S1.7): this section says what clients should expect, and
the operations will appear in `/openapi.json` as they land.

1. Upload the input and submit a conversion. The call answers immediately with a job whose status
   is `pending`; it does not block for the conversion.
2. The job moves through `pending` → `running` → `finished`, or ends `error` or `cancelled`. It
   carries a progress percentage, a phase label, byte counters and, on failure, a stable
   `error_type` from one documented taxonomy (out of memory, disk full, unsupported input, input
   too large, engine failure, timeout, cancelled, quota exceeded).
3. Follow it over Server-Sent Events, one stream per job, with a heartbeat and `Last-Event-ID`
   resume so a dropped connection reattaches instead of restarting. Polling stays available for
   clients that cannot hold a stream open.
4. On `finished` the job carries a signed, expiring download URL. A signed `job.finished` webhook
   (HMAC-SHA256, retried, with a delivery log) is sent when one is registered.
5. Submitting the same input with the same options returns the existing job instead of starting a
   second one, so a retried request costs nothing.

## Rate limits

| Scope | Limit |
| --- | --- |
| Any `/api/v1` request, per IP | 120 per minute |
| Any authenticated request, per key | 60 per minute |
| `POST /api/v1/tokens`, per IP | 5 per minute |

The IP limit is checked first: a token is only trusted as a rate-limit key once it has been
verified. Over the limit is a `429` with `Retry-After`. A CPU-minutes-per-day quota per account
lands with S1.8.

## Errors

Every error is an RFC 9457 problem document, served as `application/problem+json`.

```json
{
  "type": "about:blank",
  "title": "Unauthorized",
  "status": 401,
  "detail": "This action requires authentication.",
  "instance": "/api/v1/me",
  "request_id": "01JQZ8R4T6WQ2N9K3V5Y7B0C1D"
}
```

A `422` adds an `errors` object mapping each rejected field to its messages:

```json
{
  "type": "about:blank",
  "title": "Validation failed",
  "status": 422,
  "detail": "1 field(s) are invalid.",
  "instance": "/api/v1/tokens",
  "request_id": "01JQZ8R4T6WQ2N9K3V5Y7B0C1D",
  "errors": {
    "abilities": ["Unknown abilities: image:resize. Allowed: pixels:svg, ..."]
  }
}
```

Quote `request_id` when reporting a problem: it is in the response, in the logs and in the traces.
`4xx` bodies explain the cause; `5xx` bodies never carry internals.

| Status | When |
| --- | --- |
| `400` | Malformed request |
| `401` | No key, an unknown key, a revoked key or an expired one |
| `403` | The key is valid but lacks the ability, or the email address is unverified |
| `404` | No such resource, including a key id that is not yours |
| `415` | The body is not JSON |
| `422` | Validation failed; see `errors` |
| `429` | Rate limit or quota; see `Retry-After` |

## Generated clients

Both clients are generated from the same document and committed, so a compiler catches a change
the client was not regenerated for. `tests/openapi_test.go` asserts every operation has a method in
both.

| Client | Path |
| --- | --- |
| Go | `clients/apiclient/client.go` (package `apiclient`) |
| TypeScript | `clients/typescript/client.ts` |

Regenerate them after any change to a route or to a request or response type:

```bash
OPENAPI_PREFIX=/api/v1 go run ./cmd/app codegen:client -lang go -package apiclient \
  -o clients/apiclient/client.go
OPENAPI_PREFIX=/api/v1 go run ./cmd/app codegen:client -lang ts -o clients/typescript/client.ts
```

`OPENAPI_PREFIX` is what keeps the browser routes out of the clients: without it `codegen:client`
describes the whole router, Inertia pages included. The commands run in warmup mode (no database
connection) but still need `APP_KEY` set, because the session driver derives a key at boot. In Git
Bash on Windows, prefix them with `MSYS_NO_PATHCONV=1`, or the `/api/v1` value is rewritten into a
Windows path and the document comes out empty.

Neither client authenticates by itself. Both take a per-request header hook: `Client.Header` in Go,
`headers` in `createClient` for TypeScript.

```go
c := apiclient.New("http://localhost:3000")
c.Header = func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+key) }

me, err := c.Me(ctx)

var apiErr *apiclient.Error
if errors.As(err, &apiErr) {
	log.Println(apiErr.Status, apiErr.Problem.Detail)
}
```

```ts
const api = createClient({
  baseUrl: "http://localhost:3000",
  headers: { Authorization: `Bearer ${key}` },
});
const me = await api.me();
```

## The reference page

`/docs` is Scalar, served from the bundle embedded in the binary: no CDN, no telemetry, no remote
fonts, and so nothing to fetch from another host. It runs under the app's secure headers with a
nonce-only CSP (no `unsafe-inline`); the page passes that nonce to Scalar, which needs it for the
stylesheet it injects at run time. Its default theme follows the system colour scheme, and
`vukem/apidocs` ships the contrast fix for it, which is why no theme is set.

`vukem openapi:serve` previews the same reference on `localhost:8081` without starting the app.

## Adding a route

Register it with `vukem/api` typed handlers, in `routes/api.go`. The input and output types are
what the document is generated from: `json` tags name the fields, `validate` tags become the
schema's constraints and the `422` contract, and a `doc` tag becomes the field's description. A
plain `web.HandlerFunc` would appear in the document as an undescribed blob, so the tests reject
one.

Then, in `routes/apidocs.go`, add the operation's request example to `requestExamples`, its
response example to `componentExamples`, and a `paramNote` for any path parameter — the generator
cannot invent an example, and `TestEveryRequestAndResponseHasAnExample` fails without one. Finish
by regenerating the clients.
