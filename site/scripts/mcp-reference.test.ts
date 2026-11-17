import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { test } from "node:test";
import { loadTools, mcpPages, toolOrder, validate, type Command } from "./mcp-reference.ts";

const root = path.join(import.meta.dirname, "..", "..");
const commands = (JSON.parse(fs.readFileSync(path.join(root, "core", "schema", "commands.json"), "utf8")) as Command[]).filter((c) => !c.internal);

test("the tools are the definitions of the server, in the order of tools/list", () => {
	const order = toolOrder(root);
	const files = fs.readdirSync(path.join(root, "web", "app", "mcp", "definitions")).map((f) => f.replace(/\.json$/, ""));
	assert.deepEqual([...order].sort(), files.sort());
	assert.equal(order[0], "project_list");
	assert.equal(loadTools(root).length, order.length);
});

test("an argument that breaks the schema is reported", () => {
	const schema = { type: "object", required: ["id"], additionalProperties: false, properties: { id: { type: "string", pattern: "^a$" }, n: { type: "integer", maximum: 3 } } };
	assert.deepEqual(validate(schema, { id: "a", n: 2 }), []);
	assert.equal(validate(schema, { n: 9 }).length, 2);
	assert.equal(validate(schema, { id: "a", other: 1 }).length, 1);
});

test("every tool has a page, every operation has a page under edit, and every example fits its input schema", () => {
	const { files, tools, operations } = mcpPages(root, commands);
	for (const t of tools) assert.ok(files.has(`tools/${t}.mdx`), t);
	for (const o of operations) {
		const text = files.get(`tools/edit/${o}.mdx`);
		assert.ok(text, o);
		assert.match(text, /"op": "/);
	}
	assert.equal(operations.length, commands.length);
	assert.ok(files.has("tools.mdx"));
});
