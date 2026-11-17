// Link check over a deployed site (SDD-010 section 14, P4-DOC-001):
//   npm run check:links-live [-- <site url>]
// Starts at the documentation entry, follows every internal link of the pages under /docs/, and reports
// each link that does not answer 200 and each #fragment with no matching id.
// Links to pages outside the doc tree (home, changelog) are fetched but not crawled further.
// External links are listed, never fetched: the site must not depend on them. Node strips the types.
const DEFAULT_SITE = "https://tools.vukem.dev";

export interface PageLink {
	href: string;
	/** Absolute URL without the fragment. */
	url: string;
	fragment: string;
}

/** Hrefs of `<a>` elements in a built page, resolved against the page URL. */
export function extractLinks(html: string, pageUrl: string): { internal: PageLink[]; external: string[] } {
	const origin = new URL(pageUrl).origin;
	const internal: PageLink[] = [];
	const external: string[] = [];
	for (const m of html.matchAll(/<a\s[^>]*?\bhref="([^"]*)"/g)) {
		const href = m[1].replace(/&amp;/g, "&");
		if (/^(mailto:|tel:|javascript:)/.test(href)) continue;
		let target: URL;
		try {
			target = new URL(href, pageUrl);
		} catch {
			continue;
		}
		if (target.origin !== origin) {
			external.push(target.href);
			continue;
		}
		const fragment = decodeURIComponent(target.hash.slice(1));
		target.hash = "";
		internal.push({ href, url: target.href, fragment });
	}
	return { internal, external };
}

export function pageIds(html: string): Set<string> {
	return new Set([...html.matchAll(/\bid="([^"]+)"/g)].map((m) => m[1]));
}

async function main(): Promise<number> {
	const site = (process.argv[2] ?? DEFAULT_SITE).replace(/\/$/, "");
	const base = new URL(site).pathname.replace(/\/$/, "");
	const inTree = (url: string): boolean => {
		const p = new URL(url).pathname.slice(base.length);
		return p.startsWith("/docs/");
	};

	const pages = new Map<string, { status: number; ids: Set<string> }>();
	const problems: string[] = [];
	const externals = new Set<string>();
	const queue = [`${site}/docs/introduction/`];
	const fragments: { from: string; link: PageLink }[] = [];

	async function load(url: string): Promise<{ status: number; ids: Set<string>; html: string }> {
		const res = await fetch(url, { redirect: "follow" });
		const html = res.headers.get("content-type")?.includes("text/html") ? await res.text() : "";
		return { status: res.status, ids: pageIds(html), html };
	}

	while (queue.length > 0) {
		const url = queue.shift() as string;
		if (pages.has(url)) continue;
		const page = await load(url);
		pages.set(url, { status: page.status, ids: page.ids });
		if (page.status !== 200) continue;
		if (!inTree(url)) continue;
		const { internal, external } = extractLinks(page.html, url);
		for (const e of external) externals.add(e);
		for (const link of internal) {
			if (link.fragment) fragments.push({ from: url, link });
			if (!pages.has(link.url)) queue.push(link.url);
		}
	}

	for (const [url, page] of pages) {
		if (page.status !== 200) problems.push(`${page.status} ${url}`);
	}
	for (const { from, link } of fragments) {
		const page = pages.get(link.url);
		if (page?.status === 200 && page.ids.size > 0 && !page.ids.has(link.fragment)) {
			problems.push(`missing anchor #${link.fragment} in ${link.url} (linked from ${from})`);
		}
	}

	for (const p of problems) console.log(p);
	console.log(`external links, not fetched: ${externals.size}`);
	console.log(`check:links-live: ${pages.size} pages, ${problems.length} broken (${site})`);
	return problems.length === 0 ? 0 : 1;
}

if (import.meta.main) process.exit(await main());
