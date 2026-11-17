import assert from "node:assert/strict";
import { test } from "node:test";
import { checkPage, type Names } from "./check-command-mentions.ts";
import { checkPin, parseEnv } from "./check-ffmpeg-pin.ts";

const names: Names = {
	operations: new Set(["shape.add", "item.trim"]),
	cli: new Set(["lint", "edit", "user:create"]),
	tools: new Set(["job_start", "edit", "lint"]),
};

test("a page that names real commands passes", () => {
	const text = 'Run `vukem-video lint` or the `edit` tool with {"op": "shape.add"}. `job_start` exports.';
	assert.deepEqual(checkPage("guide/x.mdx", text, names), []);
});

test("a page that names an operation that does not exist fails", () => {
	const found = checkPage("guide/x.mdx", "Use `shape.explode` and `vukem-video lint`.", names);
	assert.equal(found.length, 1);
	assert.match(found[0].message, /shape\.explode/);
});

test("a page that names an unknown CLI command or MCP tool fails", () => {
	const found = checkPage("guide/x.mdx", "`vukem-video frobnicate` then `job_teleport`.", names);
	assert.equal(found.length, 3);
});

test("a page that names nothing fails", () => {
	const found = checkPage("guide/x.mdx", "Plain prose, a file named vukem-video.log and 1.5x.", names);
	assert.deepEqual(found.map((f) => f.message), ["names no operation, CLI command or MCP tool"]);
});

const env = parseEnv('FFMPEG_VERSION="n9.0.2-1"\nFFMPEG_RELEASE_TAG="autobuild-1"\nFFMPEG_SOURCE_URL="https://x.test/a.tar.gz"\n');

test("a page with the pin and a linked source passes", () => {
	assert.deepEqual(checkPin("n9.0.2-1 autobuild-1 <https://x.test/a.tar.gz>", env), []);
});

test("a stale version or an unlinked source fails", () => {
	const problems = checkPin("n9.0.1 autobuild-1 https://x.test/a.tar.gz", env);
	assert.equal(problems.length, 2);
});
