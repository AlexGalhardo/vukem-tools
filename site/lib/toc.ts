import GithubSlugger from "github-slugger";

export type Heading = { depth: 2 | 3; text: string; id: string };

/** Strips inline markdown so heading text matches what is rendered. */
export function plainInline(s: string): string {
	const text = s
		.replace(/`([^`]*)`/g, "$1")
		.replace(/\[([^\]]*)\]\([^)]*\)/g, "$1")
		.replace(/(\*\*|__|\*|_)(.*?)\1/g, "$2");
	return stripTags(text).trim();
}

/** Removes HTML tags until none is left, so "<<b>script>" cannot reassemble a tag after one pass. */
function stripTags(s: string): string {
	let out = s;
	let previous: string;
	do {
		previous = out;
		out = out.replace(/<[^>]+>/g, "");
	} while (out !== previous);
	return out;
}

/**
 * Lists h2/h3 headings of an MDX body, outside code fences, with the same
 * ids rehype-slug assigns (github-slugger over the heading text).
 */
export function extractHeadings(body: string): Heading[] {
	const slugger = new GithubSlugger();
	const out: Heading[] = [];
	let fence: string | null = null;

	for (const line of body.split("\n")) {
		const f = line.match(/^\s*(`{3,}|~{3,})/);
		if (f) {
			if (fence === null) fence = f[1];
			else if (f[1].startsWith(fence)) fence = null;
			continue;
		}
		if (fence !== null) continue;

		const h = line.match(/^(#{1,6})\s+(.+?)\s*#*\s*$/);
		if (!h) continue;
		const text = plainInline(h[2]);
		const id = slugger.slug(text);
		const depth = h[1].length;
		if (depth === 2 || depth === 3) out.push({ depth, text, id });
	}

	return out;
}
