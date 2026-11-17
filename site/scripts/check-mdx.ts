// Validates docs MDX without a full Next build. Run from site/:
//   npm run check:mdx                 every .mdx under content/
//   npm run check:mdx -- <file|dir>   only those files (or folders)
// Errors (exit 1): MDX that does not compile with the site's plugins, a
// component missing from components/mdx/index.tsx, a /docs/... link without
// a page, a bare {placeholder} in prose. Warnings: fenced code lines over MAX_CODE_LINE characters
// .
// Node strips the types (no build step).
import fs from "node:fs";
import path from "node:path";
import { compile } from "@mdx-js/mdx";
import matter from "gray-matter";
import { rehypePlugins, remarkPlugins } from "../lib/mdx-plugins.ts";
import { checkFragment, pageAnchors } from "./link-anchors.ts";
import { englishFiles, navigation, pagePath, pageSource } from "../lib/page-files.ts";

const MAX_CODE_LINE = 100;
const siteDir = path.join(import.meta.dirname, "..");
const contentDir = path.join(siteDir, "content");

type Issue = { file: string; line: number; message: string };
const errors: Issue[] = [];
const warnings: Issue[] = [];

function rel(file: string): string {
	return path.relative(siteDir, file).split(path.sep).join("/");
}

/** Component names in the mdxComponents object of components/mdx/index.tsx. */
function registeredComponents(): Set<string> {
	const src = fs.readFileSync(path.join(siteDir, "components", "mdx", "index.tsx"), "utf8");
	const body = src.match(/export const mdxComponents[^=]*=\s*\{([\s\S]*?)\n\};/);
	if (!body) throw new Error("components/mdx/index.tsx: mdxComponents object not found");
	const names = body[1].split("\n").map((l) => l.trim().match(/^([A-Za-z_$][\w$]*)\s*[:,]/)?.[1]);
	return new Set(names.filter((n): n is string => Boolean(n)));
}

/** Every page URL of the English tree mapped to the file that renders it, e.g. /docs/guide/timeline. */
function pageHrefs(): Map<string, string | undefined> {
	const hrefs = new Map<string, string | undefined>([["/docs", undefined]]);
	for (const cat of navigation) {
		for (const page of cat.pages) {
			const src = pageSource(cat.dir, page);
			if (src) hrefs.set(pagePath(cat, page), path.join(contentDir, src.file));
		}
	}
	return hrefs;
}

/** Structure rules of SDD-010 sections 2.3 and 3: every category is titled and every page is listed and has a file. */
function checkStructure(): void {
	const listed = new Set<string>();
	for (const cat of navigation) {
		if (!cat.title.trim()) errors.push({ file: "content/navigation.ts", line: 0, message: `the title of ${cat.dir} is empty` });
		for (const page of cat.pages) {
			if (!pageSource(cat.dir, page))
				errors.push({ file: "content/navigation.ts", line: 0, message: `${cat.dir}/${page} is listed and has no English file` });
			listed.add(`${cat.dir}/${page}`);
		}
	}
	for (const file of englishFiles()) {
		const id = file.replace(/^docs\/en\//, "").replace(/\.mdx$/, "");
		if (!listed.has(id)) errors.push({ file: `content/${file}`, line: 0, message: "is not listed in content/navigation.ts" });
	}
}

type Point = { line: number };
type MdNode = {
	type: string;
	name?: string | null;
	url?: string;
	value?: string;
	attributes?: { type: string; name?: string; value?: unknown }[];
	position?: { start: Point };
	children?: MdNode[];
};

type Found = {
	components: { name: string; line: number }[];
	links: { href: string; line: number }[];
	code: { value: string; line: number }[];
	expressions: { value: string; line: number }[];
};

/** Remark plugin that records JSX names, links and code blocks before the site's plugins rewrite them. */
function collect(found: Found) {
	return () => (tree: MdNode) => {
		const walk = (node: MdNode): void => {
			const line = node.position?.start.line ?? 0;
			if ((node.type === "mdxJsxFlowElement" || node.type === "mdxJsxTextElement") && node.name) {
				found.components.push({ name: node.name, line });
				for (const a of node.attributes ?? []) {
					if (a.type === "mdxJsxAttribute" && a.name === "href" && typeof a.value === "string")
						found.links.push({ href: a.value, line });
				}
			}
			if ((node.type === "link" || node.type === "definition") && node.url)
				found.links.push({ href: node.url, line });
			if (node.type === "code" && typeof node.value === "string") found.code.push({ value: node.value, line });
			if (
				(node.type === "mdxFlowExpression" || node.type === "mdxTextExpression") &&
				typeof node.value === "string"
			) {
				found.expressions.push({ value: node.value, line });
			}
			node.children?.forEach(walk);
		};
		walk(tree);
	};
}

function checkLink(href: string, valid: Map<string, string | undefined>): string | undefined {
	const p = href.split(/[?#]/)[0].replace(/\/$/, "");
	if (p !== "/docs" && !p.startsWith("/docs/")) return undefined;
	if (!valid.has(p)) return `internal link ${href} has no page (see content/navigation.ts)`;
	const target = valid.get(p);
	return target ? checkFragment(href, pageAnchors(matter(fs.readFileSync(target, "utf8")).content)) : undefined;
}

async function checkFile(
	file: string,
	components: Set<string>,
	hrefs: Map<string, string | undefined>,
): Promise<void> {
	const raw = fs.readFileSync(file, "utf8");
	const { content } = matter(raw);
	const offset = raw.slice(0, raw.length - content.length).split("\n").length - 1;
	const name = rel(file);
	const found: Found = { components: [], links: [], code: [], expressions: [] };
	try {
		await compile(content, { remarkPlugins: [collect(found), ...remarkPlugins], rehypePlugins });
	} catch (err) {
		const e = err as {
			line?: number;
			place?: { line?: number; start?: { line?: number } };
			reason?: string;
			message?: string;
		};
		const text = e.reason ?? e.message ?? String(err);
		// Some errors (an unclosed tag at the end of the file) only carry the place in the message: "(4:1-4:10)".
		const inText = text.match(/\((\d+):\d+(?:-\d+:\d+)?\)$/);
		const line = e.line ?? e.place?.start?.line ?? e.place?.line ?? (inText ? Number(inText[1]) : 0);
		const reason = inText ? text.slice(0, inText.index).trimEnd() : text;
		errors.push({ file: name, line: line + offset, message: reason });
		return;
	}
	for (const c of found.components) {
		const base = c.name.split(".")[0];
		if (/^[A-Z]/.test(base) && !components.has(base)) {
			errors.push({
				file: name,
				line: c.line + offset,
				message: `<${c.name}> is not registered in components/mdx/index.tsx`,
			});
		}
	}
	for (const x of found.expressions) {
		// Compiles, but prerendering fails with "ReferenceError: field is not defined".
		if (/^\s*[A-Za-z_$][\w$.]*\s*$/.test(x.value)) {
			errors.push({
				file: name,
				line: x.line + offset,
				message: `bare {${x.value.trim()}} is JavaScript; put it in backticks or a code fence`,
			});
		}
	}
	for (const l of found.links) {
		const msg = checkLink(l.href, hrefs);
		if (msg) errors.push({ file: name, line: l.line + offset, message: msg });
	}
	for (const block of found.code) {
		block.value.split("\n").forEach((text, i) => {
			const width = text.replace(/\t/g, "    ").length;
			if (width > MAX_CODE_LINE) {
				warnings.push({
					file: name,
					line: block.line + offset + 1 + i,
					message: `code line is ${width} characters (keep it under ${MAX_CODE_LINE})`,
				});
			}
		});
	}
}

function mdxFiles(target: string): string[] {
	const stat = fs.statSync(target);
	if (!stat.isDirectory()) return [target];
	return fs
		.readdirSync(target, { withFileTypes: true })
		.flatMap((e) =>
			e.isDirectory()
				? mdxFiles(path.join(target, e.name))
				: e.name.endsWith(".mdx")
					? [path.join(target, e.name)]
					: [],
		);
}

const args = process.argv.slice(2);
const files = (args.length ? args : [path.join(contentDir, "docs"), path.join(contentDir, "generated")]).flatMap((a) => {
	const abs = path.resolve(a);
	if (!fs.existsSync(abs)) {
		errors.push({ file: a, line: 0, message: "file not found" });
		return [];
	}
	return mdxFiles(abs);
});

const components = registeredComponents();
const hrefs = pageHrefs();
if (!args.length) checkStructure();
for (const file of files) await checkFile(file, components, hrefs);

for (const w of warnings) console.warn(`warning ${w.file}:${w.line}: ${w.message}`);
for (const e of errors) console.error(`error   ${e.file}:${e.line}: ${e.message}`);
console.log(`check:mdx: ${files.length} files, ${errors.length} errors, ${warnings.length} warnings`);
process.exit(errors.length ? 1 : 0);
