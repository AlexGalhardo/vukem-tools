package controllers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/AlexGalhardo/vukem/web"

	"github.com/AlexGalhardo/vukem-tools/app/catalog"
)

const (
	llmsSummary = "An open-source file conversion toolbox. Images, video, audio, PDF, archives, " +
		"sprite sheets and developer utilities, all converted on the server and reachable through a " +
		"web form or a JSON API."
	llmsRepository = "https://github.com/AlexGalhardo/vukem-tools"
	llmsSignIn     = "Every tool requires an account: sign in before submitting a file."
)

// llmsAPIGuide is the part of docs/api.md an LLM needs to call the API
// correctly. It is kept here, not read from the repository at run time, so the
// deployed binary has no dependency on files outside itself.
const llmsAPIGuide = `The JSON API lives under /api/v1. Every route except POST /api/v1/tokens, GET /api/v1/abilities, GET /api/v1/tools, GET /api/v1/conversion-errors and GET /api/v1/upload-types needs an API key.

### Authentication

Send the key either as "Authorization: Bearer <key>" or as "X-API-Key: <key>". When both are present, Authorization wins.

Exchange your account credentials for a key with POST /api/v1/tokens, a JSON body with email, password, device_name and abilities. The account's email address must be verified. The response carries the secret once; only its SHA-256 hash is stored. Keys expire 30 days after they are issued.

GET /api/v1/tokens lists your keys, newest first, without their secrets. DELETE /api/v1/tokens/current revokes the key that made the request. DELETE /api/v1/tokens/{id} revokes one key by id. Another account's id is answered like an id that does not exist.

### Abilities

A key carries exactly the abilities it was issued with. There is no default and no wildcard, so request the narrowest set you need. GET /api/v1/abilities returns the closed set. An unknown ability is a 422 on the abilities field.

### Converting a file

1. Upload the input with the resumable tus protocol at /uploads/tus/. The upload gets an id; GET /api/v1/upload-types lists the file types accepted. The type is read from the file's bytes, never from its name.
2. Submit a conversion with POST /api/v1/tools/{tool}, a JSON body with file_id (or file_ids for tools that take several files), options and an optional idempotency_key. GET /api/v1/tools lists the tool names that can be submitted. The call answers at once with the conversion in the pending status.
3. Poll GET /api/v1/conversions/{id} until the status is finished, error or cancelled. It reports progress, a phase and, on failure, a stable error_type from the list at GET /api/v1/conversion-errors.
4. Read the result with GET /api/v1/conversions/{id}/result: signed, expiring download links, one per file. Results are deleted 5 minutes after they are made, so download them straight away.
5. DELETE /api/v1/conversions/{id} cancels a conversion that has not finished. Repeating a submission with the same idempotency_key returns the same conversion instead of starting a second one.

### Rate limits

- Any /api/v1 request, per IP: 120 per minute.
- Any authenticated request, per key: 60 per minute.
- POST /api/v1/tokens, per IP: 5 per minute.

The IP limit is checked first. Over a limit the API answers 429 with a Retry-After header. A CPU-minutes-per-day quota per account is not yet enforced.

### Errors

Every error is an RFC 9457 problem document, served as application/problem+json, with the fields type, title, status, detail, instance and request_id. A 422 adds an errors object that maps each rejected field to its messages. Quote request_id when reporting a problem: it appears in the response, the logs and the traces. 4xx bodies explain the cause; 5xx bodies never carry internal details.

| Status | When |
| --- | --- |
| 400 | Malformed request |
| 401 | No key, or an unknown, revoked or expired one |
| 403 | The key lacks the ability, or the email address is unverified |
| 404 | No such resource, including a key id that is not yours |
| 415 | The body is not JSON |
| 422 | Validation failed; see errors |
| 429 | Rate limit or quota; see Retry-After |`

// LLMs serves /llms.txt and /llms-full.txt, the llmstxt.org convention: a
// Markdown file an LLM can read in one request, instead of crawling the site.
type LLMs struct {
	name string
	// base is APP_URL without its trailing slash, for the same reason as
	// Sitemap: the request's Host is attacker-controlled.
	base string
	live []string
}

// NewLLMs returns the handler. live is the set of tools registered in the
// pipeline, the same fact the index page uses, so the file cannot list a tool
// that is not usable.
func NewLLMs(name, base string, live []string) *LLMs {
	return &LLMs{name: name, base: strings.TrimSuffix(base, "/"), live: live}
}

// Index serves GET /llms.txt: links only.
func (l *LLMs) Index(c *web.Ctx) error {
	return l.serve(c, l.index())
}

// Full serves GET /llms-full.txt: the same header, with the content inline.
func (l *LLMs) Full(c *web.Ctx) error {
	return l.serve(c, l.full())
}

func (l *LLMs) serve(c *web.Ctx, body string) error {
	c.SetHeader("Content-Type", "text/plain; charset=utf-8")
	// Same reasoning as the sitemap: the file changes only on deploy.
	c.SetHeader("Cache-Control", "public, max-age=3600")
	c.Writer().WriteHeader(http.StatusOK)

	_, err := c.Writer().WriteString(body)

	return err
}

func (l *LLMs) index() string {
	var b strings.Builder

	l.header(&b)

	b.WriteString("## Tools\n\n")

	for _, t := range l.liveTools() {
		fmt.Fprintf(&b, "- [%s](%s): %s Requires an account.\n", t.Title, l.toolURL(t), t.Summary)
	}

	b.WriteString("\n## Docs\n\n")
	fmt.Fprintf(
		&b,
		"- [API reference](%s/docs): Scalar reference of the JSON API, generated from the routes.\n",
		l.base,
	)
	fmt.Fprintf(
		&b,
		"- [OpenAPI document](%s/openapi.json): the OpenAPI 3.1 document the reference is rendered from.\n",
		l.base,
	)
	fmt.Fprintf(&b, "- [Licenses](%s/licenses): every engine and library with its exact licence.\n", l.base)

	b.WriteString("\n## Optional\n\n")
	fmt.Fprintf(&b, "- [Full content](%s/llms-full.txt): this index with each tool and the API guide inline.\n", l.base)
	fmt.Fprintf(&b, "- [Source code](%s): the repository, MIT licensed.\n", llmsRepository)

	return b.String()
}

func (l *LLMs) full() string {
	var b strings.Builder

	l.header(&b)

	b.WriteString("## Tools\n\n")

	for _, t := range l.liveTools() {
		fmt.Fprintf(&b, "### %s\n\n", t.Title)
		fmt.Fprintf(&b, "%s\n\n", t.Summary)
		fmt.Fprintf(&b, "- Accepts: %s\n", strings.Join(t.In, ", "))
		fmt.Fprintf(&b, "- Produces: %s\n", strings.Join(t.Out, ", "))
		fmt.Fprintf(&b, "- Page: %s (requires an account)\n\n", l.toolURL(t))
	}

	b.WriteString("## API\n\n")
	b.WriteString(llmsAPIGuide)
	b.WriteString("\n\n")

	b.WriteString("## Docs\n\n")
	fmt.Fprintf(&b, "- [API reference](%s/docs)\n", l.base)
	fmt.Fprintf(&b, "- [Licenses](%s/licenses)\n", l.base)
	fmt.Fprintf(&b, "- [Source code](%s)\n", llmsRepository)

	return b.String()
}

func (l *LLMs) header(b *strings.Builder) {
	fmt.Fprintf(b, "# %s\n\n", l.name)
	fmt.Fprintf(b, "> %s\n\n", llmsSummary)
	b.WriteString(llmsSignIn + "\n\n")
}

// liveTools keeps only tools a visitor can use now. Planned and skipped
// entries are in the catalogue on purpose, but a model asked to convert a file
// must not be sent to a page that does not exist.
func (l *LLMs) liveTools() []*catalog.Tool {
	all := catalog.All(l.live)
	out := make([]*catalog.Tool, 0, len(all))

	for i := range all {
		if all[i].Status == catalog.Live {
			out = append(out, &all[i])
		}
	}

	return out
}

func (l *LLMs) toolURL(t *catalog.Tool) string {
	return l.base + "/tools/" + t.Name
}
