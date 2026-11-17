// The API reference pages (docs/api/): one page per operation of site/generated/openapi.json, an endpoint index
// and a schema page. Everything on a page is read from the OpenAPI document, from the route table of
// web/app/http/endpoints.go (the scope of each route) and from the command registry (the example of an edit);
// nothing is written by hand per operation. Node strips the types.
import fs from "node:fs";
import path from "node:path";
import GithubSlugger from "github-slugger";
import { samples, SECRET_VARIABLE, type SampleRequest } from "./code-samples.ts";
import { code, esc, fence, page, sentence, table } from "./reference-md.ts";

export type Schema = {
	type?: string | string[];
	description?: string;
	properties?: Record<string, Schema>;
	required?: string[];
	enum?: unknown[];
	items?: Schema;
	minimum?: number;
	maximum?: number;
	minItems?: number;
	maxItems?: number;
	minLength?: number;
	maxLength?: number;
	pattern?: string;
	format?: string;
	default?: unknown;
	example?: unknown;
	$ref?: string;
	oneOf?: Schema[];
	anyOf?: Schema[];
	additionalProperties?: Schema | boolean;
};

type Param = { name: string; in: string; required?: boolean; description?: string; schema?: Schema };
type Response = { description?: string; $ref?: string; content?: Record<string, { schema?: Schema }> };
type Operation = {
	operationId: string;
	summary?: string;
	description?: string;
	parameters?: Param[];
	requestBody?: { required?: boolean; content?: Record<string, { schema?: Schema }> };
	responses?: Record<string, Response>;
	security?: unknown[];
};

export type OpenApi = {
	info: { title: string; description: string; version: string };
	paths: Record<string, Record<string, Operation>>;
	components: { schemas: Record<string, Schema>; responses: Record<string, Response> };
};

/** The address of a server started with its defaults (`vukem-video serve`, VUKEM_VIDEO_ADDR unset). */
export const BASE_URL = "http://127.0.0.1:7878";

const METHOD_ORDER = ["get", "post", "put", "patch", "delete"];

/** The sidebar groups of the API reference, in order. The OpenAPI document has no tags, so the route decides. */
export const TAGS = ["Projects", "Media", "Jobs", "Events", "Commands", "Templates", "Fonts", "Brand kit", "System", "API keys", "Users", "Audit"] as const;
export type Tag = (typeof TAGS)[number];

export function tagOf(route: string): Tag {
	const shown = route.replace(/^\/api\/v1/, "");
	const parts = shown.split("/").filter(Boolean);
	if (parts[0] === "projects" && parts[2] === "media") return "Media";
	if ((parts[0] === "projects" && parts[2] === "jobs") || parts[0] === "jobs") return "Jobs";
	if (parts[0] === "projects" && parts[2] === "events") return "Events";
	if (parts[0] === "projects" || parts[0] === "trash") return "Projects";
	const byRoot: Record<string, Tag> = {
		assets: "Media",
		commands: "Commands",
		templates: "Templates",
		fonts: "Fonts",
		"brand-kit": "Brand kit",
		system: "System",
		handshake: "System",
		keys: "API keys",
		users: "Users",
		audit: "Audit",
	};
	const tag = byRoot[parts[0] ?? ""];
	if (!tag) throw new Error(`api-reference: no tag for ${route}; add it to tagOf`);
	return tag;
}

export type ApiOp = {
	id: string;
	method: string;
	/** The route as a client requests it, such as /api/v1/projects/{id}. */
	route: string;
	tag: Tag;
	op: Operation;
	scope?: string;
	alsoScope?: string;
};

/** Scope of each route, from the route table of the HTTP adapter. */
export function routeScopes(root: string): Map<string, { scope?: string; also?: string }> {
	const src = fs.readFileSync(path.join(root, "web", "app", "http", "endpoints.go"), "utf8").replace(/\r\n/g, "\n");
	const out = new Map<string, { scope?: string; also?: string }>();
	// An entry is a literal that starts at its `ID:` member; its scope members follow before the next `ID:`.
	for (const block of src.split(/\bID:\s*"/).slice(1)) {
		const id = /^([^"]+)"/.exec(block)?.[1];
		if (!id) continue;
		const scope = /(?<![A-Za-z])Scope:\s*service\.Scope([A-Za-z]+)/.exec(block)?.[1];
		const also = /\bAlsoScope:\s*service\.Scope([A-Za-z]+)/.exec(block)?.[1];
		out.set(id, { scope: scope?.toLowerCase(), also: also?.toLowerCase() });
	}
	return out;
}

/** Every operation, grouped by tag in the order of TAGS, then by route and method. */
export function operations(doc: OpenApi, scopes: Map<string, { scope?: string; also?: string }>): ApiOp[] {
	const all: ApiOp[] = [];
	for (const [route, ops] of Object.entries(doc.paths)) {
		for (const [method, op] of Object.entries(ops)) {
			const s = scopes.get(op.operationId);
			all.push({ id: op.operationId, method: method.toUpperCase(), route, tag: tagOf(route), op, scope: s?.scope, alsoScope: s?.also });
		}
	}
	return all.sort(
		(a, b) =>
			TAGS.indexOf(a.tag) - TAGS.indexOf(b.tag) ||
			a.route.localeCompare(b.route) ||
			METHOD_ORDER.indexOf(a.method.toLowerCase()) - METHOD_ORDER.indexOf(b.method.toLowerCase()),
	);
}

const refName = (ref: string): string => ref.replace("#/components/schemas/", "");
const anchor = (name: string): string => new GithubSlugger().slug(name);

/** The command an `args.<name>` schema describes: its page is the operation page of the edit tool. */
const commandHref = (name: string): string => `/docs/mcp/tools/edit/${name.replace(/^args\./, "")}`;

/** A schema as the text of a table cell, with links to the schema page and to the edit operations. */
export function apiType(s: Schema | undefined): string {
	if (!s) return "";
	if (s.$ref) {
		const name = refName(s.$ref);
		return name.startsWith("args.") ? `[${code(name)}](${commandHref(name)})` : `[${code(name)}](/docs/api/schemas#${anchor(name)})`;
	}
	const variants = s.oneOf ?? s.anyOf;
	if (variants) {
		const parts = variants.map(apiType).filter((t) => t !== code("null"));
		const nullable = variants.some((v) => v.type === "null");
		return parts.join(" or ") + (nullable ? " or `null`" : "");
	}
	if (s.enum) {
		if (s.enum.length > 6) return `${code("string")}, one of ${s.enum.length} values`;
		return s.enum.map((v) => code(String(v))).join(", ");
	}
	const types = Array.isArray(s.type) ? s.type : [s.type ?? "any"];
	const main = types.filter((t) => t !== "null");
	const nullable = types.includes("null");
	let text: string;
	if (main[0] === "array") text = `array of ${s.items ? apiType(s.items) : code("any")}`;
	else if (main[0] === "object" && typeof s.additionalProperties === "object" && !s.properties) text = `map of ${apiType(s.additionalProperties)}`;
	else text = main.map((t) => code(t + (s.format ? ` (${s.format})` : ""))).join(" or ") || code("any");
	return text + (nullable ? " or `null`" : "");
}

function constraints(s: Schema): string {
	const out: string[] = [];
	if (s.minimum !== undefined) out.push(`min ${s.minimum}`);
	if (s.maximum !== undefined) out.push(`max ${s.maximum}`);
	if (s.minLength !== undefined) out.push(`min length ${s.minLength}`);
	if (s.maxLength !== undefined) out.push(`max length ${s.maxLength}`);
	if (s.minItems !== undefined) out.push(`at least ${s.minItems} items`);
	if (s.maxItems !== undefined) out.push(`at most ${s.maxItems} items`);
	if (s.pattern) out.push(`pattern ${code(s.pattern)}`);
	if (s.default !== undefined) out.push(`default ${code(JSON.stringify(s.default))}`);
	return out.join("; ");
}

function describe(s: Schema): string {
	const note = constraints(s);
	return [s.description ? esc(s.description) : "", note].filter(Boolean).join(note && s.description ? ". " : "");
}

function propertyRows(s: Schema): string[][] {
	const required = new Set(s.required ?? []);
	return Object.entries(s.properties ?? {}).map(([name, p]) => [code(name), apiType(p), required.has(name) ? "yes" : "no", describe(p)]);
}

export const propertyTable = (s: Schema): string => table(["Name", "Type", "Required", "Description"], propertyRows(s));

/** Builds a sample value from a schema: `example`, then `default`, then the first enum value, else a placeholder of the type. */
export function exampleOf(s: Schema | undefined, schemas: Record<string, Schema>, depth = 0, seen: string[] = []): unknown {
	if (!s) return null;
	if (s.example !== undefined) return s.example;
	if (s.$ref) {
		const name = refName(s.$ref);
		if (seen.includes(name)) return {};
		return exampleOf(schemas[name], schemas, depth, [...seen, name]);
	}
	if (s.default !== undefined) return s.default;
	if (s.enum) return s.enum[0];
	const variants = s.oneOf ?? s.anyOf;
	if (variants) {
		const first = variants.find((v) => v.type !== "null") ?? variants[0];
		return exampleOf(first, schemas, depth, seen);
	}
	const type = (Array.isArray(s.type) ? s.type.find((t) => t !== "null") : s.type) ?? (s.properties ? "object" : "string");
	switch (type) {
		case "object": {
			if (depth >= 3) return {};
			return Object.fromEntries(Object.entries(s.properties ?? {}).map(([k, v]) => [k, exampleOf(v, schemas, depth + 1, seen)]));
		}
		case "array":
			return depth >= 3 ? [] : [exampleOf(s.items, schemas, depth + 1, seen)];
		case "integer":
		case "number":
			return 0;
		case "boolean":
			return false;
		default:
			return s.format === "date-time" ? "2006-01-02T15:04:05Z" : "string";
	}
}

/** The members of a body that must be sent, with an example of each. */
function bodyExample(s: Schema, schemas: Record<string, Schema>): unknown {
	const resolved = s.$ref ? schemas[refName(s.$ref)] : s;
	const required = new Set(resolved?.required ?? []);
	return Object.fromEntries(
		Object.entries(resolved?.properties ?? {})
			.filter(([k]) => required.has(k))
			.map(([k, v]) => [k, exampleOf(v, schemas, 1)]),
	);
}

const STATUS_TEXT: Record<string, string> = { "200": "OK", "201": "Created", "202": "Accepted", "401": "Unauthorized", "422": "Unprocessable Content" };

export type Context = {
	doc: OpenApi;
	/** Example of the arguments of each command, for the body of an edit. */
	commandExamples: Map<string, unknown>;
};

const resolveResponse = (r: Response, doc: OpenApi): Response =>
	r.$ref ? (doc.components.responses[r.$ref.replace("#/components/responses/", "")] ?? r) : r;

const placeholder = (name: string): string => (name === "id" ? "PROJECT_ID" : name.toUpperCase());

/** Hand-checked bodies where a placeholder would teach nothing: a real command, a real job kind. */
function bodyOverride(op: ApiOp, ctx: Context): unknown {
	switch (op.id) {
		case "projects.create":
			return { name: "Launch teaser", preset: "9:16" };
		case "projects.edit":
			return {
				base_revision: 0,
				operations: [{ op: "project.rename", args: ctx.commandExamples.get("project.rename") ?? { name: "Launch teaser v2" } }],
			};
		case "jobs.start":
			return { kind: "export", args: { format: "mp4" }, wait_ms: 0 };
		case "keys.create":
			return { name: "ci", scopes: ["read"] };
		default:
			return undefined;
	}
}

export function sampleRequest(op: ApiOp, ctx: Context): SampleRequest {
	const schemas = ctx.doc.components.schemas;
	const params = op.op.parameters ?? [];
	let url = BASE_URL + op.route;
	for (const p of params.filter((q) => q.in === "path")) url = url.replace(`{${p.name}}`, placeholder(p.name));
	const query = params.filter((q) => q.in === "query" && q.required).map((q) => ({ name: q.name, value: String(exampleOf(q.schema, schemas)) }));
	const bodySchema = op.op.requestBody?.content?.["application/json"]?.schema;
	const body = bodySchema ? (bodyOverride(op, ctx) ?? bodyExample(bodySchema, schemas)) : undefined;
	const ok = Object.entries(op.op.responses ?? {}).find(([status]) => status.startsWith("2"));
	const json = Boolean(ok && resolveResponse(ok[1], ctx.doc).content?.["application/json"]);
	return { method: op.method, url, query, body, json };
}

const methodBadge = (m: string): string => m;

function problemExample(status: string, schemas: Record<string, Schema>): unknown {
	const base = exampleOf({ $ref: "#/components/schemas/Problem" }, schemas) as Record<string, unknown>;
	const real: Record<string, unknown> =
		status === "401"
			? {
					status: 401,
					code: "E_UNAUTHORIZED",
					message: "the request carries no credentials.",
					hint: "send the local token as Authorization: Bearer <token>, or run `vukem-video open` to open the editor in a browser",
					retryable: false,
				}
			: status === "422"
				? { status: 422, code: "E_VALIDATION", retryable: false }
				: {};
	return { ...base, ...real };
}

// The summary of the two operations the local token cannot call says so (trash.empty, system.tools.install).
function browserOnly(op: ApiOp): boolean {
	return (op.op.summary ?? "").includes("the local token gets 403");
}

function operationPage(op: ApiOp, ctx: Context): string {
	const { doc } = ctx;
	const schemas = doc.components.schemas;
	const shown = op.route;
	const params = op.op.parameters ?? [];
	const route = `<ApiRoute method="${methodBadge(op.method)}" path="${shown}" />`;
	const main: string[] = [];
	if (op.op.description) main.push(esc(sentence(op.op.description)));

	main.push(
		"## Authentication",
		[
			`Send the secret in the header ${code("Authorization: Bearer <secret>")}: the local token in local mode, an [API key](/docs/self-hosting/api-keys) in network mode.`,
			op.scope
				? `An API key needs the ${code(op.scope)} scope${op.alsoScope ? ` (or ${code(op.alsoScope)} where the call concerns media)` : ""}; ${browserOnly(op) ? "the local token has every scope but this operation refuses it with 403 E_FORBIDDEN: it needs a browser session or an admin key." : "the local token has every scope."}`
				: "",
			// The route table carries no scope for the handshake, and the adapter lets it through without a credential.
			op.id === "handshake.get"
				? "In local mode this route needs no credential: a client uses it to check that the server holds the local token."
				: "",
			"See the [API overview](/docs/api/overview) for both modes.",
		]
			.filter(Boolean)
			.join(" "),
	);

	for (const [place, heading] of [
		["path", "Path parameters"],
		["query", "Query parameters"],
		["header", "Header parameters"],
	] as const) {
		const list = params.filter((p) => p.in === place);
		if (!list.length) continue;
		main.push(
			`## ${heading}`,
			table(
				["Name", "Type", "Required", "Description"],
				list.map((p) => [code(p.name), apiType(p.schema), p.required ? "yes" : "no", p.schema ? describe({ ...p.schema, description: p.description ?? p.schema.description }) : esc(p.description ?? "")]),
			),
		);
	}

	const bodySchema = op.op.requestBody?.content?.["application/json"]?.schema;
	if (bodySchema) {
		const resolved = bodySchema.$ref ? schemas[refName(bodySchema.$ref)] : bodySchema;
		main.push("## Request body", `Content type ${code("application/json")}${op.op.requestBody?.required ? "." : ", optional."}`);
		main.push(resolved?.properties && Object.keys(resolved.properties).length ? propertyTable(resolved) : "The body has no members.");
	}

	main.push("## Responses");
	const aside: string[] = [];
	aside.push("<CodeGroup>", ...samples(sampleRequest(op, ctx)).map((s) => fence(s.lang, s.code, s.label)), "</CodeGroup>");

	const exampleBlocks: string[] = [];
	for (const [status, raw] of Object.entries(op.op.responses ?? {})) {
		const r = resolveResponse(raw, doc);
		const label = `${status}${STATUS_TEXT[status] ? ` ${STATUS_TEXT[status]}` : ""}`;
		const media = r.content ? Object.entries(r.content)[0] : undefined;
		main.push(`### ${status === "default" ? "Any other status" : label}`);
		const said = esc(sentence(r.description ?? ""));
		if (said && said !== STATUS_TEXT[status]) main.push(said);
		if (!media) {
			if (status.startsWith("2")) main.push("The answer is not a JSON document; see the description of the operation.");
			continue;
		}
		const [type, body] = media;
		const resolved = body.schema?.$ref ? schemas[refName(body.schema.$ref)] : body.schema;
		main.push(`Content type ${code(type)}.${body.schema?.$ref ? ` Schema: ${apiType(body.schema)}.` : ""}`);
		if (resolved?.properties && Object.keys(resolved.properties).length && type === "application/json") main.push(propertyTable(resolved));
		const value = type === "application/problem+json" ? problemExample(status, schemas) : exampleOf(body.schema, schemas);
		if (status !== "default") exampleBlocks.push(fence("json", JSON.stringify(value, null, 2), status));
	}

	if (exampleBlocks.length) {
		aside.push(
			"**Response examples**",
			"<CodeGroup>",
			...exampleBlocks,
			"</CodeGroup>",
			"The examples are built from the schemas of the OpenAPI document, so a value is a placeholder of its type. The fields that carry the real values are in the tables.",
		);
	}

	const description = `${op.method} ${shown}${op.op.summary ? `: ${op.op.summary}` : ""}`;
	return page(
		op.op.summary ?? op.id,
		description,
		[route, "<RefSplit>", "<RefMain>", main.join("\n\n"), "</RefMain>", "<RefAside>", aside.join("\n\n"), "</RefAside>", "</RefSplit>"].join("\n\n"),
		{ badge: op.method, wide: true },
	);
}

function endpointIndex(ops: ApiOp[]): string {
	const sections = TAGS.map((tag) => {
		const list = ops.filter((o) => o.tag === tag);
		if (!list.length) return "";
		return [`## ${tag}`, table(["Method", "Path", "Operation", "Summary"], list.map((o) => [code(o.method), code(o.route), `[${code(o.id)}](/docs/api/${o.id})`, esc(o.op.summary ?? "")]))].join("\n\n");
	}).filter(Boolean);
	return page(
		"Endpoints",
		"Every route of the HTTP API, grouped by resource.",
		["Every operation has its own page with the parameters, the body, the responses and a request in eight languages.", ...sections].join("\n\n"),
	);
}

function schemaPage(doc: OpenApi): string {
	const names = Object.keys(doc.components.schemas)
		.filter((n) => !n.startsWith("args."))
		.sort((a, b) => a.localeCompare(b));
	const sections = names.map((name) => {
		const s = doc.components.schemas[name];
		const body = s.properties && Object.keys(s.properties).length ? propertyTable(s) : `A value of type ${apiType({ ...s, $ref: undefined })}.`;
		return [`### ${name}`, s.description ? esc(s.description) : "", body].filter(Boolean).join("\n\n");
	});
	return page(
		"Schemas",
		"The objects the HTTP API takes and returns, from its OpenAPI document.",
		[
			"The types named in the tables of the operations. The arguments of each editing command are on the pages of the [edit tool](/docs/mcp/tools/edit), where the `args` of an operation of a batch are described.",
			...sections,
		].join("\n\n"),
	);
}

/** The pages of the API reference, keyed by their path under content/generated/api/, and the order of the sidebar. */
export function apiPages(doc: OpenApi, root: string, commandExamples: Map<string, unknown>): { files: Map<string, string>; nav: { tag: Tag; pages: string[] }[] } {
	const ops = operations(doc, routeScopes(root));
	const ctx: Context = { doc, commandExamples };
	const files = new Map<string, string>();
	for (const op of ops) files.set(`${op.id}.mdx`, operationPage(op, ctx));
	files.set("endpoints.mdx", endpointIndex(ops));
	files.set("schemas.mdx", schemaPage(doc));
	const nav = TAGS.map((tag) => ({ tag, pages: ops.filter((o) => o.tag === tag).map((o) => o.id) })).filter((g) => g.pages.length > 0);
	return { files, nav };
}

export { SECRET_VARIABLE };
