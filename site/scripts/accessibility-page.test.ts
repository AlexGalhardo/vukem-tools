import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { test } from "node:test";

// The conformance page of the site keeps the criteria and the states of the table of SDD-012 section 18.6
// (P3-UI-004): a criterion added, dropped or changed there fails here until the page follows.
const root = path.join(import.meta.dirname, "..", "..");
const STATE = /^(met by exception|met|not claimed|n\/a)\b/;

/** The first-column criterion number and the state of every row of the first table that follows `heading`. */
function rows(markdown: string, heading: RegExp, stateOf: (cell: string) => string): Map<string, string> {
	const lines = markdown.replace(/\r\n/g, "\n").split("\n");
	const start = lines.findIndex((l) => heading.test(l));
	assert.ok(start >= 0, `no heading ${heading}`);
	const out = new Map<string, string>();
	let inTable = false;
	for (const line of lines.slice(start + 1)) {
		if (line.startsWith("|")) {
			inTable = true;
			const cells = line.split("|").map((c) => c.trim());
			const id = /^(\d\.\d+\.\d+)\b/.exec(cells[1] ?? "")?.[1];
			if (id) out.set(id, stateOf(cells[2] ?? ""));
		} else if (inTable) break;
	}
	return out;
}

const sdd = fs.readFileSync(path.join(root, "docs", "sdds", "012-ui-architecture.md"), "utf8");
const expected = rows(sdd, /^### 18\.6 /, (cell) => STATE.exec(cell)?.[1] ?? cell);

test("SDD-012 section 18.6 has criteria to compare", () => {
	assert.ok(expected.size >= 10);
});

test("the English page lists the criteria and the states of SDD-012 section 18.6", () => {
	const page = fs.readFileSync(path.join(root, "site", "content", "docs", "en", "guide", "accessibility.mdx"), "utf8");
	assert.deepEqual(rows(page, /^## Conformance table/, (c) => c), expected);
});

test("the screen reader rows stay pending until the manual pass of P3-QA-002", () => {
	const page = fs.readFileSync(path.join(root, "site", "content", "docs", "en", "guide", "accessibility.mdx"), "utf8");
	assert.match(page, /\| NVDA on Windows \| pending \|/);
	assert.match(page, /\| VoiceOver on macOS \| pending \|/);
});
