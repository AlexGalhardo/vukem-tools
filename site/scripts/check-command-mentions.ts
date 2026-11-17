// Every guide page names the command that does what it teaches, and every command
// it names exists (P2-DOC-003). Run from site/:
//   npm run check:command-mentions
// A page passes when it mentions at least one real operation, CLI command or MCP tool,
// and mentions no operation, CLI command or MCP tool that does not exist. Node strips
// the types (no build step).
import fs from "node:fs";
import path from "node:path";

const siteDir = path.join(import.meta.dirname, "..");

/** Prefixes of the command families of SDD-000 section 1: a token `<prefix>.<name>` is an operation. */
const OP_PREFIXES = ["project", "settings", "asset", "track", "item", "timeline", "clip", "audio", "text", "shape", "caption", "keyframe", "slot", "template"];
/** File extensions that look like an operation (`clip.mov`). */
const FILE_EXTENSIONS = new Set(["mov", "mp4", "mp3", "wav", "mkv", "webm", "json", "txt", "zip", "jpg", "png"]);
/** Commands that exist outside the generated CLI reference (SDD-000 section 1, "Other CLI commands"). */
const EXTRA_CLI = ["open", "serve", "user:create", "user:passwd"];

export type Names = { operations: Set<string>; cli: Set<string>; tools: Set<string> };

export type Finding = { page: string; message: string };

export function loadNames(): Names {
	const opsDir = path.join(siteDir, "content", "generated", "mcp", "tools", "edit");
	const operations = new Set(fs.readdirSync(opsDir).filter((f) => f.endsWith(".mdx")).map((f) => f.slice(0, -4)));
	const cli = JSON.parse(fs.readFileSync(path.join(siteDir, "generated", "cli.json"), "utf8")) as { commands: { name: string }[] };
	const toolsDir = path.join(siteDir, "..", "web", "app", "mcp", "definitions");
	const tools = new Set(fs.readdirSync(toolsDir).filter((f) => f.endsWith(".json")).map((f) => f.slice(0, -5)));
	return { operations, cli: new Set([...cli.commands.map((c) => c.name), ...EXTRA_CLI]), tools };
}

/** Checks the text of one page against the names; `page` labels the findings. */
export function checkPage(page: string, text: string, names: Names): Finding[] {
	const findings: Finding[] = [];
	let known = 0;
	const opRe = new RegExp(`(?<![\\w./-])(${OP_PREFIXES.join("|")})\\.([a-z][a-z_]*)(?![\\w.])`, "g");
	for (const m of text.matchAll(opRe)) {
		if (FILE_EXTENSIONS.has(m[2])) continue;
		const name = `${m[1]}.${m[2]}`;
		if (names.operations.has(name)) known++;
		else findings.push({ page, message: `\`${name}\` is not an operation of the catalogue` });
	}
	for (const m of text.matchAll(/vukem-video ([a-z]+(?::[a-z]+)?)(?=[\s`'"]|$)/g)) {
		const sub = m[1];
		if (names.cli.has(sub)) known++;
		else if (!["serve", "mcp", "version"].includes(sub)) findings.push({ page, message: `\`vukem-video ${sub}\` is not a CLI command` });
		else known++;
	}
	for (const m of text.matchAll(/`((?:project|job|media|command)_[a-z_]+)`/g)) {
		if (m[1].endsWith("_id")) continue;
		if (names.tools.has(m[1])) known++;
		else findings.push({ page, message: `\`${m[1]}\` is not an MCP tool` });
	}
	for (const tool of ["edit", "history", "lint", "view", "template"]) {
		if (new RegExp(`\`${tool}\`( tool| with)`).test(text)) known++;
	}
	if (known === 0) findings.push({ page, message: "names no operation, CLI command or MCP tool" });
	return findings;
}

function main(): void {
	const names = loadNames();
	const dir = path.join(siteDir, "content", "docs", "en", "guide");
	const findings: Finding[] = [];
	for (const file of fs.readdirSync(dir).filter((f) => f.endsWith(".mdx")).sort()) {
		findings.push(...checkPage(`guide/${file}`, fs.readFileSync(path.join(dir, file), "utf8"), names));
	}
	for (const f of findings) console.error(`${f.page}: ${f.message}`);
	if (findings.length > 0) process.exit(1);
	console.log("check-command-mentions: every guide page names commands that exist");
}

if (process.argv[1] && path.resolve(process.argv[1]) === import.meta.filename) main();
