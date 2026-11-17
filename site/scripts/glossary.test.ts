import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { test } from "node:test";
import { allTerms, entries, resolveSource, sourceRegistry, termId, topics } from "../lib/glossary-data.ts";
import { escProse, topicPages } from "./glossary-pages.ts";

const docsDir = path.join(import.meta.dirname, "..", "content", "docs", "en");

test("the glossary has the ten topics and hundreds of terms", () => {
	assert.equal(topics().length, 10);
	assert.ok(allTerms().length >= 300, `only ${allTerms().length} terms`);
});

test("every topic file exists and every term has the fields a reader needs", () => {
	for (const topic of topics()) {
		assert.ok(entries(topic.slug).length >= 20, `${topic.slug} has few terms`);
	}
	for (const t of allTerms()) {
		assert.ok(t.term.trim() && t.meaning.trim() && t.example.trim(), `${t.term}: term, meaning and example are required`);
		assert.ok(t.tip.length > 10 && t.tip.length <= 160, `${t.term}: the tip is ${t.tip.length} characters, want 11 to 160`);
		assert.ok(!/[{}]/.test(t.tip), `${t.term}: no braces in a tip`);
	}
});

test("term ids (the anchors) are unique across the glossary", () => {
	const seen = new Map<string, string>();
	for (const t of allTerms()) {
		assert.ok(!seen.has(t.id), `${t.term} and ${seen.get(t.id)} share the anchor ${t.id}`);
		seen.set(t.id, t.term);
	}
});

test("every see-also names a term, and every source resolves to a link", () => {
	const ids = new Set(allTerms().map((t) => t.id));
	const registry = sourceRegistry();
	for (const t of allTerms()) {
		for (const s of t.see ?? []) assert.ok(ids.has(termId(s)), `${t.term}: see "${s}" is not a term`);
		const keys = t.sources ?? [];
		assert.equal(new Set(keys).size, keys.length, `${t.term}: a source is listed twice`);
		for (const k of keys) assert.ok(resolveSource(k, registry), `${t.term}: "${k}" is not a source`);
	}
	for (const [key, s] of Object.entries(registry)) assert.ok(s.title && /^https:\/\//.test(s.url), `source ${key} needs a title and an https address`);
});

test("every Term of the docs names a term of the glossary", () => {
	const ids = new Set(allTerms().map((t) => t.id));
	const walk = (dir: string): string[] =>
		fs.readdirSync(dir, { withFileTypes: true }).flatMap((e) => (e.isDirectory() ? walk(path.join(dir, e.name)) : e.name.endsWith(".mdx") ? [path.join(dir, e.name)] : []));
	let used = 0;
	for (const file of walk(docsDir)) {
		for (const m of fs.readFileSync(file, "utf8").matchAll(/<Term id="([^"]+)"/g)) {
			used++;
			assert.ok(ids.has(m[1]), `${path.relative(docsDir, file)}: <Term id="${m[1]}"> is not a glossary term`);
		}
	}
});

test("MDX syntax in a term cannot leak into the generated page", () => {
	assert.equal(escProse("a {b} <c> `{d}`"), "a &#123;b&#125; &lt;c&gt; `{d}`");
});

test("the generated pages are one per topic plus the A to Z list, each with its terms as headings", () => {
	const pages = topicPages();
	assert.equal(pages.length, topics().length + 1);
	const first = pages.find((p) => p.file === "shooting-and-footage.mdx");
	assert.ok(first?.text.includes("### Footage"));
	assert.ok(first?.text.includes("**Example.**"));
	assert.ok(pages.find((p) => p.file === "a-to-z.mdx")?.text.includes("## A"));
});
