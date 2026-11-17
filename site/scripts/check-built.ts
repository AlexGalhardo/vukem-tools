// Checks the built site in dist/ (run `bun run build` first, then `bun run check:built`):
//   1. only static files: no server entry, no adapter output (D-015);
//   2. no external script, font, stylesheet, image or analytics: nothing the page loads comes from another
//      origin, and no other host appears in the built files except the inert ones listed below (D-015, D-016);
//   3. every root-relative href and src starts with the base path (SDD-010 section 12.4);
//   4. every docs page has a clean Markdown twin and llms.txt lists them, and what it lists exists;
//   5. the search index answers `pdf crop` with the crop guide and `device links` with the API endpoints page.
// Node strips the types (no build step).
import fs from "node:fs";
import path from "node:path";
import { sourceRegistry } from "../lib/glossary-data.ts";
import { search } from "../lib/search-rank.ts";

const outDir = path.join(import.meta.dirname, "..", "dist");

/** Hosts that may appear as text: namespaces, error-message links and the places the site links to. */
const INERT_HOSTS = new Set([
	"www.w3.org",
	"www.sitemaps.org",
	"react.dev",
	"base-ui.com",
	"semver.org",
	"modelcontextprotocol.io",
	// Official documentation of the agents that the Prompts pages cite; text, never loaded.
	"code.claude.com",
	"learn.chatgpt.com",
	"opencode.ai",
	"cursor.com",
	"geminicli.com",
	"keepachangelog.com",
	"github.com",
	"tools.vukem.dev",
	"app.tools.vukem.dev",
	"ghcr.io",
	"127.0.0.1",
	"localhost",
	// Example names of the self-hosting pages (a domain you would own); text, never loaded.
	"video.example.com",
	"collector.example.com",
	"www.conventionalcommits.org",
	"caddyserver.com",
	"opentimelineio.org",
	"taskfile.dev",
	// The OIDC issuer that the cosign commands of the verification page pin; it is text, never loaded.
	"token.actions.githubusercontent.com",
]);


/** Attributes whose value the browser fetches by itself when the page loads. */
const LOADING =
	/<(script|img|source|iframe|video|audio|embed|track|input)\b[^>]*?\b(?:src|srcset|poster)\s*=\s*"([^"]*)"|<link\b[^>]*?\bhref\s*=\s*"([^"]*)"[^>]*>/gi;

/** The host of every http(s) URL in text, read with the URL parser rather than a pattern on the host. */
export function hostsIn(text: string): string[] {
	const hosts = new Set<string>();
	for (const [candidate] of text.matchAll(/\bhttps?:\/\/[^\s"'<>()`${}\\]+/gi)) {
		try {
			// A sentence period is not part of the host. new URL("https://tools.vukem.dev.") keeps the dot.
			hosts.add(new URL(candidate.replace(/\.+$/, "")).hostname.toLowerCase());
		} catch {
			// Not a URL (a template fragment such as "https://${host}"): nothing to compare.
		}
	}
	return [...hosts];
}

/** Problems of the "no external origin" property for one built file. */
// The glossary cites authorities by link (text, never loaded): the hosts of its source registry and Wikipedia.
const CITED_HOSTS = new Set([
	"en.wikipedia.org",
	...Object.values(sourceRegistry()).map((s) => new URL(s.url).hostname),
]);

export function foreignOriginProblems(file: string, text: string, ownHost: string): string[] {
	const problems: string[] = [];
	for (const host of hostsIn(text)) {
		if (host !== ownHost && !INERT_HOSTS.has(host) && !CITED_HOSTS.has(host)) problems.push(`${file}: refers to ${host}`);
	}
	if (file.endsWith(".html")) {
		for (const m of text.matchAll(LOADING)) {
			const value = m[2] ?? m[3] ?? "";
			if (/^(?:https?:)?\/\//i.test(value) && hostsIn(value.replace(/^\/\//, "https://"))[0] !== ownHost) {
				problems.push(`${file}: loads ${value}`);
			}
		}
	}
	if (/@import\s+(?:url\()?["']?(?:https?:)?\/\//i.test(text) && file.endsWith(".css"))
		problems.push(`${file}: @import of a remote stylesheet`);
	if (/\burl\(\s*["']?(?:https?:)?\/\/(?!(?:www\.w3\.org)\b)/i.test(text) && file.endsWith(".css"))
		problems.push(`${file}: url() of a remote resource`);
	return problems;
}

/** Root-relative href and src values that miss the base path. */
export function basePathProblems(file: string, html: string, base: string): string[] {
	if (base === "") return [];
	const problems: string[] = [];
	for (const m of html.matchAll(/\b(?:href|src)\s*=\s*"(\/[^"]*)"/g)) {
		const value = m[1];
		if (value.startsWith("//")) continue;
		if (value !== base && !value.startsWith(`${base}/`))
			problems.push(`${file}: ${value} does not start with ${base}`);
	}
	return problems;
}

/**
 * The docs pages of a build: every `docs/<path>/index.html` that holds a page. A redirect (a page that moved, which
 * Astro writes as a refresh to the new address) is not a page: it has no twin, no canonical of its own and is not
 * listed in the sitemap or in llms.txt.
 */
export function docPages(files: string[], read: (rel: string) => string): string[] {
	return files
		.filter((f) => f.startsWith("docs/") && f.endsWith("/index.html") && f !== "docs/index.html")
		.filter((f) => !/http-equiv="refresh"/.test(read(f)))
		.map((f) => f.slice(0, -"/index.html".length));
}

/**
 * Twins and llms.txt (SDD-010 section 9, properties 1 to 3): every docs page has a Markdown twin
 * without JSX or imports, llms.txt lists every page, and every URL it lists resolves to a file.
 * `files` are paths relative to out/ with forward slashes.
 */
export function twinProblems(files: string[], read: (rel: string) => string, siteUrl: string): string[] {
	const problems: string[] = [];
	const set = new Set(files);
	const pages = docPages(files, read);
	if (!set.has("llms.txt")) return ["llms.txt is missing"];
	const listed = new Set(
		[...read("llms.txt").matchAll(/\]\((https?:\/\/[^)\s]+)\)/g)].map((m) =>
			m[1].replace(siteUrl, "").replace(/^\//, "").replace(/\/$/, ""),
		),
	);

	for (const page of pages) {
		if (!set.has(`${page}.md`)) problems.push(`${page}: no Markdown twin (${page}.md)`);
		else {
			const md = read(`${page}.md`);
			const prose = md.replace(/^```[\s\S]*?^```/gm, "");
			if (/^import\s/m.test(prose) || /<[A-Z][A-Za-z]*[\s/>]/.test(prose)) problems.push(`${page}.md: has JSX or an import`);
		}
		if (!listed.has(page)) problems.push(`llms.txt does not list ${page}`);
	}
	for (const url of listed) {
		if (!set.has(`${url}/index.html`) && !set.has(url)) problems.push(`llms.txt lists ${url}, which is not in out/`);
		if (url.startsWith("docs/") && !pages.includes(url)) problems.push(`llms.txt lists ${url}, which is not a page`);
	}
	return problems;
}

const attr = (html: string, rel: string): { hreflang?: string; href: string }[] =>
	[...html.matchAll(/<link\b[^>]*>/g)]
		.map((m) => m[0])
		.filter((tag) => new RegExp(`\\brel="${rel}"`).test(tag))
		.map((tag) => ({
			hreflang: /\bhreflang="([^"]*)"/.exec(tag)?.[1],
			href: /\bhref="([^"]*)"/.exec(tag)?.[1] ?? "",
		}));

/**
 * The page properties of SDD-010 sections 4 and 14 on the built pages (English only): every docs page has
 * `lang="en"`, no hreflang alternate and one canonical that points to itself and exists; the sitemap lists
 * every page and no twin; the 404 page exists.
 */
export function pageProblems(files: string[], read: (rel: string) => string, siteUrl: string): string[] {
	const problems: string[] = [];
	const set = new Set(files);
	const at = (url: string) => url.replace(siteUrl, "").replace(/^\//, "").replace(/\/$/, "");
	const pages = docPages(files, read);
	const sitemap = set.has("sitemap.xml") ? read("sitemap.xml") : "";

	for (const page of pages) {
		const html = read(`${page}/index.html`);
		const canonical = attr(html, "canonical");
		if (!/<html[^>]*\blang="en"/.test(html)) problems.push(`${page}: html lang is not en`);
		if (attr(html, "alternate").some((a) => a.hreflang)) problems.push(`${page}: has hreflang alternates, the site is English only`);
		if (canonical.length !== 1) problems.push(`${page}: ${canonical.length} canonical links, want 1`);
		else if (at(canonical[0].href) !== page) problems.push(`${page}: canonical ${canonical[0].href} is not the page itself`);
		if (sitemap && !sitemap.includes(`/${page}/`)) problems.push(`sitemap.xml does not list ${page}`);
	}

	if (/\.md</.test(sitemap)) problems.push("sitemap.xml lists a Markdown twin");
	if (/\/pt-br\//.test(sitemap)) problems.push("sitemap.xml lists a pt-br page");
	if (!set.has("404.html")) problems.push("404.html is missing");

	return problems;
}

function walk(dir: string): string[] {
	return fs
		.readdirSync(dir, { withFileTypes: true })
		.flatMap((e) => (e.isDirectory() ? walk(path.join(dir, e.name)) : [path.join(dir, e.name)]));
}

/** The queries that must find a page: the first result of each is on that page (SDD-010 section 14 check 8). The index holds paths without the base path. */
export const SEARCH_EXPECTATIONS: { query: string; page: string }[] = [
	{ query: "pdf crop", page: "/docs/guide/pdf-crop" },
	{ query: "device links", page: "/docs/api/endpoints" },
];

type IndexEntry = { title: string; heading?: string; href: string; text: string };

/** Runs the expected queries against the search index of the build, ranked as the search dialog ranks. */
export function searchProblems(read: (rel: string) => string, expectations = SEARCH_EXPECTATIONS): string[] {
	let index: IndexEntry[];
	try {
		index = JSON.parse(read("search-index.json")) as IndexEntry[];
	} catch {
		return ["search-index.json is missing or is not JSON"];
	}
	const problems: string[] = [];
	for (const { query, page } of expectations) {
		const top = search(index, query)[0];
		const where = top ? top.href.split("#")[0] : undefined;
		if (where !== page) problems.push(`search "${query}": the first result is ${where ?? "nothing"}, want ${page}`);
	}
	return problems;
}

function main(): number {
	if (!fs.existsSync(outDir)) {
		console.error("dist/ does not exist: run `bun run build` first");
		return 1;
	}
	const siteUrl = new URL(process.env.SITE_URL || "https://tools.vukem.dev");
	const base = siteUrl.pathname.replace(/\/$/, "");
	const problems: string[] = [];
	const files = walk(outDir);

	for (const file of files) {
		const rel = path.relative(outDir, file).split(path.sep).join("/");
		if (
			/(^|\/)(server|_worker\.js|entry\.mjs|\.vercel|\.netlify)(\/|$)/.test(rel) ||
			(/\.(mjs|cjs)$/.test(rel) && !rel.startsWith("_astro/"))
		) {
			problems.push(`${rel}: server output in a static site`);
		}
		if (!/\.(html|js|css|svg|json|txt|xml|md)$/.test(rel)) continue;
		const text = fs.readFileSync(file, "utf8");
		problems.push(...foreignOriginProblems(rel, text, siteUrl.hostname));
		if (rel.endsWith(".html")) problems.push(...basePathProblems(rel, text, base));
	}

	const rels = files.map((f) => path.relative(outDir, f).split(path.sep).join("/"));
	const read = (rel: string) => fs.readFileSync(path.join(outDir, rel), "utf8");
	problems.push(...twinProblems(rels, read, siteUrl.origin + base));
	problems.push(...pageProblems(rels, read, siteUrl.origin + base));
	problems.push(...searchProblems(read));

	for (const p of problems) console.error(p);
	console.log(`check:built: ${files.length} files, ${problems.length} problems`);
	return problems.length > 0 ? 1 : 0;
}

if (import.meta.main) process.exit(main());
