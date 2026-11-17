import { fold } from "./fold.ts";

/** The fields of a search entry that the ranking reads. */
export type Rankable = { title: string; heading?: string; text: string };

export const MAX_RESULTS = 24;

/** Characters of `needle` appear in order in `hay` (fuzzy fallback). */
function subsequence(needle: string, hay: string): boolean {
	let i = 0;
	for (const ch of hay) if (ch === needle[i] && ++i === needle.length) return true;
	return needle.length === 0;
}

function score(entry: Rankable, tokens: string[], raw: string, fuzzy: boolean): number {
	const title = fold(entry.title);
	const heading = fold(entry.heading ?? "");
	const text = fold(entry.text);
	let total = 0;

	for (const t of tokens) {
		let s = 0;
		if (heading.startsWith(t) || title.startsWith(t)) s += 12;
		if (heading.includes(t)) s += 8;
		if (title.includes(t)) s += entry.heading ? 3 : 10;
		if (text.includes(t)) s += 2;
		if (s === 0 && fuzzy && t.length > 2 && subsequence(t, entry.heading ? heading : title)) s += 1;
		if (s === 0) return 0;
		total += s;
	}

	if (fold(entry.heading ?? entry.title) === raw) total += 20;
	return total;
}

/**
 * The results of a query, best first: exact substring matches, and the subsequence (fuzzy) matches only
 * when there is no exact one. `raw` is the folded query and `tokens` its words.
 */
export function rankEntries<T extends Rankable>(index: T[], tokens: string[], raw: string): (T & { score: number })[] {
	const rank = (fuzzy: boolean) =>
		index
			.map((e) => ({ ...e, score: score(e, tokens, raw, fuzzy) }))
			.filter((r) => r.score > 0)
			.sort((a, b) => b.score - a.score)
			.slice(0, MAX_RESULTS);
	const exact = rank(false);
	return exact.length > 0 ? exact : rank(true);
}

/** Ranks the entries for a typed query, the way the search dialog does. */
export function search<T extends Rankable>(index: T[], query: string): (T & { score: number })[] {
	const raw = fold(query.trim());
	const tokens = raw.split(/\s+/).filter(Boolean);
	return tokens.length === 0 ? [] : rankEntries(index, tokens, raw);
}
