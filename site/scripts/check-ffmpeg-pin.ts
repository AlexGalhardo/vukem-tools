// The FFmpeg page states the version and the release tag recorded in .config/ffmpeg.env
// and links the source URL of the same file (P2-DOC-004). Run from site/:
//   npm run check:ffmpeg-pin
// Node strips the types (no build step).
import fs from "node:fs";
import path from "node:path";

const siteDir = path.join(import.meta.dirname, "..");

/** Reads KEY="value" lines of a shell env file. */
export function parseEnv(text: string): Map<string, string> {
	const out = new Map<string, string>();
	for (const line of text.split(/\r?\n/)) {
		const m = line.match(/^([A-Z0-9_]+)="([^"]*)"\s*$/);
		if (m) out.set(m[1], m[2]);
	}
	return out;
}

/** The problems of `page` against the pin; empty when it matches. */
export function checkPin(page: string, env: Map<string, string>): string[] {
	const problems: string[] = [];
	for (const key of ["FFMPEG_VERSION", "FFMPEG_RELEASE_TAG", "FFMPEG_SOURCE_URL"]) {
		const value = env.get(key);
		if (!value) problems.push(`${key} is missing from .config/ffmpeg.env`);
		else if (!page.includes(value)) problems.push(`the page does not contain ${key}=${value}`);
	}
	const url = env.get("FFMPEG_SOURCE_URL");
	if (url && !page.includes(`<${url}>`) && !page.includes(`](${url})`)) problems.push("the source URL is not a link");
	return problems;
}

function main(): void {
	const env = parseEnv(fs.readFileSync(path.join(siteDir, "..", ".config", "ffmpeg.env"), "utf8"));
	const page = fs.readFileSync(path.join(siteDir, "content", "docs", "en", "self-hosting", "ffmpeg-and-licences.mdx"), "utf8");
	const problems = checkPin(page, env);
	for (const p of problems) console.error(`self-hosting/ffmpeg-and-licences: ${p}`);
	if (problems.length > 0) process.exit(1);
	console.log("check-ffmpeg-pin: the page matches .config/ffmpeg.env");
}

if (process.argv[1] && path.resolve(process.argv[1]) === import.meta.filename) main();
