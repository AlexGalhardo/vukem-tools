import assert from "node:assert/strict";
import path from "node:path";
import { test } from "node:test";
import { readKeymap, shortcutRows, shortcutsPage } from "./shortcuts-reference.ts";

const root = path.join(import.meta.dirname, "..", "..");

test("every row of the editor keymap is on the page, with Mod spelled out per platform", async () => {
	const { keymap, labels } = await readKeymap(root);
	const page = shortcutsPage(keymap, labels);
	for (const row of shortcutRows(keymap, labels)) {
		const escapedLabel = row.label.replace(/\\/g, "\\\\").replace(/\|/g, "\\|");
		assert.ok(page.includes(escapedLabel), `the page lacks "${row.label}"`);
	}
	assert.match(page, /`Ctrl\+B`.*`Cmd\+B`/);
	const cells = page
		.split("\n")
		.filter((l) => l.startsWith("|"))
		.join("\n");
	assert.doesNotMatch(cells, /`Mod\+/, "Mod is always replaced by Ctrl or Cmd in a key cell");
});

test("a row without a label in en.json fails the generation", async () => {
	const { keymap } = await readKeymap(root);
	assert.throws(() => shortcutRows(keymap, {}), /no ui\.editor\.shortcuts\.rows/);
});
