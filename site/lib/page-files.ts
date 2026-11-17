// Where the file of a page is (SDD-010 sections 2.2 and 4.2). Shared by the site and the
// scripts, so it imports nothing through the "@/" alias. Node only.
import fs from "node:fs";
import path from "node:path";
import { navigation, type NavCategory, type NavSection } from "../content/navigation.ts";

export const CONTENT_ROOT = path.join(process.cwd(), "content");

/** A page is written by hand (`english`: content/docs/en) or generated (content/generated). */
export type PageKind = "generated" | "english";

export type PageSource = {
	/** Path under content/, with forward slashes. */
	file: string;
	kind: PageKind;
};

function exists(file: string): boolean {
	return fs.existsSync(path.join(CONTENT_ROOT, file));
}

/** The file that renders `<dir>/<slug>`, or undefined when there is none. */
export function pageSource(dir: string, slug: string): PageSource | undefined {
	const own = `docs/en/${dir}/${slug}.mdx`;
	const generated = `generated/${dir}/${slug}.mdx`;

	if (exists(own)) return { file: own, kind: "english" };
	if (exists(generated)) return { file: generated, kind: "generated" };
	return undefined;
}

/** The URL of a page, without a trailing slash: /docs/<prefix or dir>/<slug>. */
export function pagePath(cat: NavCategory, slug: string): string {
	const segs = [cat.prefix ?? cat.dir, slug].filter((s) => s !== "");
	return `/docs/${segs.join("/")}`;
}

/** Every hand-written file under content/docs/en, relative to content/. */
export function englishFiles(): string[] {
	const root = path.join(CONTENT_ROOT, "docs", "en");
	const walk = (dir: string): string[] =>
		fs
			.readdirSync(dir, { withFileTypes: true })
			.flatMap((e) => (e.isDirectory() ? walk(path.join(dir, e.name)) : [path.join(dir, e.name)]));
	return fs.existsSync(root)
		? walk(root)
				.filter((f) => f.endsWith(".mdx"))
				.map((f) => path.relative(CONTENT_ROOT, f).split(path.sep).join("/"))
				.sort()
		: [];
}

export { navigation };
export type { NavSection };
