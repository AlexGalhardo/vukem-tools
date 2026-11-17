// The data of the glossary section (TASK-0267): `content/glossary/topics.json` lists the topics, one file per topic
// holds its terms, and `sources.json` holds the authoritative references that terms cite by key. Shared by the site
// (the Term tooltip) and the generator, so it imports nothing through the "@/" alias. Node only.
import fs from "node:fs";
import path from "node:path";
import GithubSlugger from "github-slugger";

export type Topic = { slug: string; title: string; description: string; intro: string };

export type Entry = {
	/** The term as it is printed in its heading. The anchor is the GitHub slug of it. */
	term: string;
	/** One plain sentence, at most 160 characters: the tooltip. */
	tip: string;
	/** What it is, in words a ten-year-old knows. */
	meaning: string;
	/** An everyday example. */
	example: string;
	/** Where the term shows up in Vukem Tools (Markdown, may link to a docs page). */
	vukem?: string;
	/** Other names for the same thing. */
	aka?: string[];
	/** Terms to read next, as the text of their heading. */
	see?: string[];
	/** `wiki:<Article_title>` for English Wikipedia, or `ref:<key>` of `sources.json`. */
	sources?: string[];
};

export type Source = { title: string; url: string };

const dir = path.join(process.cwd(), "content", "glossary");

const readJson = <T>(file: string): T => JSON.parse(fs.readFileSync(path.join(dir, file), "utf8")) as T;

export function topics(): Topic[] {
	return readJson<Topic[]>("topics.json");
}

/** The id (the anchor) of a term. */
export function termId(term: string): string {
	return new GithubSlugger().slug(term);
}

/** The entries of a topic, sorted for reading: alphabetical, ignoring case. */
export function entries(slug: string): Entry[] {
	return readJson<Entry[]>(`${slug}.json`).sort((a, b) => a.term.localeCompare(b.term, "en", { sensitivity: "base" }));
}

export type Located = Entry & { id: string; topic: Topic };

let cache: Located[] | undefined;

/** Every term of every topic, in topic order. */
export function allTerms(): Located[] {
	if (cache) return cache;
	cache = topics().flatMap((topic) => entries(topic.slug).map((e): Located => ({ ...e, id: termId(e.term), topic })));
	return cache;
}

/** A term by its id (the slug of its heading). */
export function findTerm(id: string): Located | undefined {
	return allTerms().find((t) => t.id === id);
}

/** The path of the heading of a term, without the base path. */
export function termPath(t: Located): string {
	return `/docs/glossary/${t.topic.slug}#${t.id}`;
}

export function sourceRegistry(): Record<string, Source> {
	return readJson<Record<string, Source>>("sources.json");
}

/** The title and the address of a source key; undefined for a key that is not one. */
export function resolveSource(key: string, registry: Record<string, Source> = sourceRegistry()): Source | undefined {
	if (key.startsWith("wiki:")) {
		const article = key.slice("wiki:".length);
		if (!/^[^\s/?#]+$/.test(article)) return undefined;
		return { title: `Wikipedia: ${decodeURIComponent(article).replace(/_/g, " ")}`, url: `https://en.wikipedia.org/wiki/${article}` };
	}
	if (key.startsWith("ref:")) return registry[key.slice("ref:".length)];
	return undefined;
}
