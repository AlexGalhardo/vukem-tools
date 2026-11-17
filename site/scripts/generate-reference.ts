// Generates the reference pages under content/generated/reference/ from the sources that cannot drift
// (SDD-010 section 8): the command registry (core/schema/commands.json), the error registry
// (core/schema/errors.json and SDD-000 section 8), the timeline schema, the CLI declarations of the
// binary (go run ./internal/tools/clidoc -> generated/cli.json) and the configuration table of SDD-000
// section 10. The shortcuts page (content/generated/guide/) is read from the editor keymap (shortcuts-reference.ts). Run from site/: `npm run reference`. `npm run reference:check` fails on any difference.
// The API reference (content/generated/api/) comes from site/generated/openapi.json, which `UPDATE_OPENAPI=1 go test
// ./web/contract/ -run OpenAPI` writes from the running API. The MCP reference (content/generated/mcp/) comes from
// web/app/mcp/definitions/ and from the command registry. Both write generated/reference-nav.json, the order of
// their sidebar groups. Node strips the types.
import fs from "node:fs";
import path from "node:path";
import { apiPages, type OpenApi } from "./api-reference.ts";
import { topicPages } from "./glossary-pages.ts";
import { mcpPages } from "./mcp-reference.ts";
import { readKeymap, shortcutsPage } from "./shortcuts-reference.ts";
import { code, esc, page, sentence, table } from "./reference-md.ts";

export { esc };

const siteDir = path.join(import.meta.dirname, "..");
const root = path.join(siteDir, "..");
const outDir = path.join(siteDir, "content", "generated", "reference");
const agentsDir = path.join(siteDir, "content", "generated", "agents");
const glossaryDir = path.join(siteDir, "content", "generated", "glossary");
const apiDir = path.join(siteDir, "content", "generated", "api");
const mcpDir = path.join(siteDir, "content", "generated", "mcp");
const guideDir = path.join(siteDir, "content", "generated", "guide");

type Json = Record<string, unknown>;

const readJson = <T = Json>(rel: string): T => JSON.parse(fs.readFileSync(path.join(root, rel), "utf8")) as T;

type Command = {
	name: string;
	summary: string;
	description: string;
	errors: string[];
	example: unknown;
	internal: boolean;
	phase: string;
	scope: string;
	args_schema: Schema;
};

type Schema = {
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
	pattern?: string;
	default?: unknown;
	$ref?: string;
	oneOf?: Schema[];
	anyOf?: Schema[];
	"x-status"?: string;
};

function typeOf(s: Schema): string {
	if (s.$ref) return code(s.$ref.replace("#/$defs/", ""));
	if (s.enum) return s.enum.map((v) => code(String(v))).join(", ");
	if (s.oneOf || s.anyOf) return (s.oneOf ?? s.anyOf ?? []).map(typeOf).join(" or ");
	const t = Array.isArray(s.type) ? s.type.join(" or ") : (s.type ?? "any");
	return t === "array" && s.items ? `array of ${typeOf(s.items)}` : code(t);
}

function constraints(s: Schema): string {
	const out: string[] = [];
	if (s.minimum !== undefined) out.push(`min ${s.minimum}`);
	if (s.maximum !== undefined) out.push(`max ${s.maximum}`);
	if (s.minItems !== undefined) out.push(`at least ${s.minItems} items`);
	if (s.maxItems !== undefined) out.push(`at most ${s.maxItems} items`);
	if (s.pattern) out.push(`pattern ${code(s.pattern)}`);
	if (s.default !== undefined) out.push(`default ${code(JSON.stringify(s.default))}`);
	if (s["x-status"]) out.push(s["x-status"]);
	return out.join("; ");
}

function propertyRows(s: Schema): string[][] {
	const required = new Set(s.required ?? []);
	return Object.entries(s.properties ?? {}).map(([name, p]) => [
		code(name),
		typeOf(p),
		required.has(name) ? "yes" : "no",
		esc([p.description ?? "", constraints(p)].filter(Boolean).join(" ")),
	]);
}

type ErrorEntry = { code: string; http: number; retryable: boolean };

function sddTable(rel: string, heading: RegExp): string[][] {
	const text = fs.readFileSync(path.join(root, rel), "utf8").replace(/\r\n/g, "\n");
	const start = text.search(heading);
	if (start < 0) throw new Error(`${rel}: no section matching ${heading}`);
	const rest = text.slice(start).split("\n").slice(1);
	const rows: string[][] = [];
	let seen = false;
	for (const line of rest) {
		if (line.startsWith("|")) {
			seen = true;
			const cells = line.replace(/^\||\|$/g, "").split(/(?<!\\)\|/).map((c) => c.trim());
			if (!/^-+$/.test(cells[0] ?? "")) rows.push(cells);
		} else if (seen) break;
	}
	return rows.slice(1);
}

function errorCodes(): string {
	const registry = readJson<ErrorEntry[]>("core/schema/errors.json");
	const meaning = new Map<string, string>();
	for (const [codes, text] of sddTable("docs/sdds/000-shared-definitions.md", /^## 8\. /m)) {
		for (const c of codes.match(/E_[A-Z_]+/g) ?? []) meaning.set(c, text ?? "");
	}
	const rows = [...registry]
		.sort((a, b) => a.code.localeCompare(b.code))
		.map((e) => [code(e.code), String(e.http), e.retryable ? "yes" : "no", esc(meaning.get(e.code) ?? "")]);

	const known = new Set(registry.map((e) => e.code));
	const status = serviceStatuses();
	const serviceRows: string[][] = [];
	for (const [codes, text, retry] of sddTable("docs/sdds/000-shared-definitions.md", /^## 8\. /m)) {
		for (const c of codes.match(/E_[A-Z_]+/g) ?? []) {
			if (known.has(c)) continue;
			serviceRows.push([code(c), String(status.get(c) ?? 500), retry === "yes" ? "yes" : "no", esc(text ?? "")]);
		}
	}
	serviceRows.sort((a, b) => a[0].localeCompare(b[0]));

	const anchors = [...rows, ...serviceRows].map((r) => `<a id="${r[0].replace(/`/g, "").toLowerCase()}"></a>`).join("");
	return page(
		"Error codes",
		"Every error code, its HTTP status and whether retrying can help.",
		[
			"An error carries `code`, `message`, `hint` and `retryable`. The CLI exits with `2` for a retryable code and `1` otherwise.",
			anchors,
			"## Errors of the editing commands",
			"Raised by the command core when an operation of a batch is refused.",
			table(["Code", "HTTP", "Retryable", "Meaning"], rows),
			"## Errors of the service",
			"Raised around the commands: sign-in, imports, exports, jobs and limits. They appear in the results of the calls that raise them. A code that is not in the status table of the service answers with HTTP 500, which is the status of the failures of a job.",
			table(["Code", "HTTP", "Retryable", "Meaning"], serviceRows),
		].join("\n\n"),
	);
}

const STATUS_NUMBERS: Record<string, number> = {
	StatusBadRequest: 400,
	StatusUnauthorized: 401,
	StatusForbidden: 403,
	StatusNotFound: 404,
	StatusMethodNotAllowed: 405,
	StatusConflict: 409,
	StatusRequestEntityTooLarge: 413,
	StatusUnprocessableEntity: 422,
	StatusTooManyRequests: 429,
	StatusClientClosed: 499,
	StatusInternalServerError: 500,
	StatusServiceUnavailable: 503,
};

/** The HTTP status of each error code of service/error.go, read from the `HTTPStatus` switch of the service. */
export function serviceStatuses(): Map<string, number> {
	const src = fs.readFileSync(path.join(root, "service", "error.go"), "utf8").replace(/\r\n/g, "\n");
	const consts = new Map<string, string>();
	for (const m of src.matchAll(/(Code[A-Za-z]+)\s*=\s*"(E_[A-Z_]+)"/g)) consts.set(m[1], m[2]);
	const out = new Map<string, number>();
	for (const m of src.matchAll(/case ([^:]+?):\s*return (?:http\.)?(Status[A-Za-z]+)/gs)) {
		const num = STATUS_NUMBERS[m[2]];
		if (num === undefined) throw new Error(`service/error.go: status ${m[2]} is not in STATUS_NUMBERS`);
		for (const token of m[1].matchAll(/"(E_[A-Z_]+)"|(?<![A-Za-z.])(Code[A-Za-z]+)/g)) {
			const c = token[1] ?? consts.get(token[2]);
			if (c) out.set(c, num);
		}
	}
	return out;
}

type CliCommand = {
	name: string;
	summary: string;
	usage: string[];
	example?: string;
	flags?: { name: string; type: string; default?: string; usage: string; repeat?: boolean }[];
	globals: string[];
	pending?: string;
};

type CliDoc = {
	globals: { name: string; type: string; default?: string; usage: string }[];
	exit_codes: { code: number; meaning: string }[];
	commands: CliCommand[];
	process_commands: CliCommand[];
};

function cliReference(cli: CliDoc): string {
	const flagRows = (flags: NonNullable<CliDoc["commands"][number]["flags"]>) =>
		flags.map((f) => [code(`--${f.name}`), f.type + (f.repeat ? ", repeatable" : ""), f.default ? code(f.default) : "", esc(f.usage)]);
	const sections = [...cli.commands, ...cli.process_commands].map((c) => {
		const out = [`## ${code(c.name)}`, esc(sentence(c.summary)) + (c.pending ? ` Not available yet: arrives with ${c.pending}.` : "")];
		out.push("```text\n" + c.usage.join("\n") + "\n```");
		if (c.flags?.length) out.push(table(["Flag", "Type", "Default", "Meaning"], flagRows(c.flags)));
		if (c.globals.length) out.push(`Global flags: ${c.globals.map((g) => code(`--${g}`)).join(", ")}.`);
		if (c.example) out.push("Example:\n\n```bash\n" + c.example + "\n```");
		return out.join("\n\n");
	});
	return page(
		"CLI reference",
		"Every vukem-video command, its flags and the exit codes.",
		[
			"The CLI is a thin adapter over the same service as the editor, the MCP server and the HTTP API. Add `--json` to any command for one JSON document on standard output.",
			"## Global flags",
			table(["Flag", "Type", "Default", "Meaning"], flagRows(cli.globals)),
			"## Exit codes",
			table(["Code", "Meaning"], cli.exit_codes.map((e) => [String(e.code), esc(e.meaning)])),
			...sections,
		].join("\n\n"),
	);
}

type Timeline = { $defs: Record<string, Schema>; properties: Record<string, Schema>; required?: string[]; title?: string };

function timelineSchema(): string {
	const t = readJson<Timeline>("core/schema/timeline.v1.schema.json");
	const sections = Object.entries(t.$defs)
		.sort(([a], [b]) => a.localeCompare(b))
		.map(([name, s]) =>
			[`## ${name}`, s.description ? esc(s.description) : "", propertyRows(s).length ? table(["Property", "Type", "Required", "Description"], propertyRows(s)) : ""]
				.filter(Boolean)
				.join("\n\n"),
		);
	return page(
		"Timeline schema",
		"The JSON Schema of the project document, version 1.",
		[
			"A project is one JSON document. Times are whole frames at the frame rate of the project. The root has these properties:",
			table(["Property", "Type", "Required", "Description"], propertyRows({ properties: t.properties, required: t.required })),
			...sections,
		].join("\n\n"),
	);
}

function configuration(): string {
	const rows = sddTable("docs/sdds/000-shared-definitions.md", /^## 10\. /m).map(([name, def, meaning]) => [
		(name ?? "").replace(/\\/g, "\\\\").replace(/\|/g, "\\|"),
		esc(def ?? ""),
		esc(meaning ?? ""),
	]);
	return page(
		"Configuration variables",
		"Every environment variable of vukem-video, with its default.",
		["All variables are optional.", table(["Variable", "Default", "Meaning"], rows)].join("\n\n"),
	);
}


type Term = { id: string; en: string; note: string };

function glossary(): string {
	const terms = JSON.parse(fs.readFileSync(path.join(siteDir, "content", "glossary.json"), "utf8")) as Term[];
	return page(
		"Product terms",
		"The product terms the documentation and the editor use.",
		[
			"Command, field and error code names are written exactly as the program prints them. This table is for prose and labels.",
			table(["Term", "Note"], terms.map((t) => [esc(t.en), esc(t.note)])),
		].join("\n\n"),
	);
}

/** Escapes the text of a Markdown line outside inline code for MDX (no pipe: the skill has no tables). */
function escProse(text: string): string {
	const map: Record<string, string> = { "\\": "\\\\", "{": "\\{", "}": "\\}", "<": "&lt;", ">": "&gt;" };
	return text
		.split(/(`[^`]*`)/)
		.map((part, i) => (i % 2 === 1 ? part : part.replace(/[\\{}<>]/g, (c) => map[c] as string)))
		.join("");
}

/**
 * The agent skill page (SDD-010 section 7.4, SDD-003 section 8.3): the body of web/app/agentdocs/out/SKILL.md
 * without its front matter, its generated comment and its H1 (the page title is the H1), so the text is
 * written once, by the Go generator, and never committed twice by hand.
 */
function skillPage(): string {
	const raw = fs.readFileSync(path.join(root, "web", "app", "agentdocs", "out", "SKILL.md"), "utf8").replace(/\r\n/g, "\n");
	const body = raw
		.replace(/^---\n[\s\S]*?\n---\n/, "")
		.replace(/^<!--.*-->\n/m, "")
		.replace(/^# .*\n/m, "");
	let fence = false;
	const lines = body.split("\n").map((line) => {
		if (/^\s*```/.test(line)) {
			fence = !fence;
			return line;
		}
		return fence ? line : escProse(line);
	});
	return page(
		"The agent skill",
		"The skill file that teaches an agent the edit loop, the rules, the commands and the errors, as shipped in the release archives.",
		[
			"This page is the skill file `agent/skills/vukem-video/SKILL.md` of every release archive. Write it into an agent's skills folder with `vukem-video agent:docs --write <dir>`, or print it with `vukem-video agent:docs --print skill`.",
			lines.join("\n"),
		].join("\n\n"),
	);
}

function write(file: string, text: string, expected: Set<string>, base = outDir) {
	const target = path.join(base, file);
	fs.mkdirSync(path.dirname(target), { recursive: true });
	fs.writeFileSync(target, text);
	expected.add(path.resolve(target));
}

async function main() {
	console.error(
		"vukem-tools docs are written by hand under content/docs/en. This generator is the vukem-video shell and does not run here.",
	);
	process.exitCode = 1;
}

if (import.meta.main) await main();
