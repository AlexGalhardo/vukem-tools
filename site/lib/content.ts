import fs from "node:fs";
import path from "node:path";
import matter from "gray-matter";
import { CONTENT_ROOT, navigation, pagePath, pageSource, type NavSection, type PageKind } from "@/lib/page-files";
import { lastUpdated } from "@/lib/last-updated";
import { extractHeadings, type Heading } from "@/lib/toc";

export type DocMeta = {
	/** URL segments after /docs, e.g. ["getting-started"]. */
	slug: string[];
	/** Path under site/content, e.g. "docs/en/getting-started/introduction.mdx". */
	source: string;
	/** The path the page is served at, without the base path and the trailing slash. */
	href: string;
	/** `<dir>/<slug>`: the identity of the page. */
	id: string;
	kind: PageKind;
	title: string;
	description: string;
	/** The name above the title and in search results. */
	category: string;
	/** The sidebar the page belongs to. */
	section: NavSection;
	/** The label of the sidebar item, such as the method of an API operation. */
	badge?: string;
	/** A reference page: two columns and no table of contents. */
	wide: boolean;
	/** The sidebar group that holds the page: the index of its category in the navigation. */
	group: number;
	/** ISO 8601 date of the source file's last commit, if known. */
	updated?: string;
};

export type Doc = DocMeta & {
	body: string;
	headings: Heading[];
};

function readDoc(source: string): { title: string; description: string; body: string; badge?: string; wide: boolean } {
	const raw = fs.readFileSync(path.join(CONTENT_ROOT, source), "utf8");
	const { data, content } = matter(raw);
	if (typeof data.title !== "string" || typeof data.description !== "string") {
		throw new Error(`content/${source}: front matter needs "title" and "description"`);
	}
	return {
		title: data.title,
		description: data.description,
		body: content,
		badge: typeof data.badge === "string" ? data.badge : undefined,
		wide: data.wide === true,
	};
}

let cache: Doc[] | undefined;

/** Every page in navigation order. */
export function getAllDocs(): Doc[] {
	if (cache) return cache;

	cache = navigation.flatMap((cat, group) =>
		cat.pages.map((page): Doc => {
			const src = pageSource(cat.dir, page);
			if (!src) throw new Error(`content/navigation.ts lists ${cat.dir}/${page}, which has no file`);
			const { title, description, body, badge, wide } = readDoc(src.file);
			const href = pagePath(cat, page);
			return {
				slug: href.slice("/docs/".length).split("/"),
				source: src.file,
				href,
				id: `${cat.dir}/${page}`,
				kind: src.kind,
				title,
				description,
				category: cat.label ?? cat.title,
				section: cat.section ?? "docs",
				badge,
				wide,
				group,
				updated: lastUpdated(src.file),
				body,
				headings: extractHeadings(body),
			};
		}),
	);
	return cache;
}

export function getDoc(slug: string[]): Doc | undefined {
	const key = slug.join("/");
	return getAllDocs().find((d) => d.slug.join("/") === key);
}

/** The previous and next page of the same section: the last API page does not lead into the documentation. */
export function getNeighbours(doc: DocMeta): { prev?: DocMeta; next?: DocMeta } {
	const all = getAllDocs().filter((d) => d.section === doc.section);
	const i = all.findIndex((d) => d.href === doc.href);
	return { prev: all[i - 1], next: all[i + 1] };
}

export type NavItem = { title: string; href: string; badge?: string };
export type NavGroup = { title: string; label: string; section: NavSection; items: NavItem[] };

/** The sidebar groups of one section, or of every section when none is given. */
export function getNavGroups(section?: NavSection): NavGroup[] {
	const all = getAllDocs();
	return navigation
		.map((cat, index): NavGroup => ({
			title: cat.title,
			label: cat.label ?? cat.title,
			section: cat.section ?? "docs",
			items: all.filter((d) => d.group === index).map((d) => ({ title: d.title, href: d.href, badge: d.badge })),
		}))
		.filter((g) => section === undefined || g.section === section);
}
