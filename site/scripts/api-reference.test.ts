import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import { apiPages, exampleOf, operations, routeScopes, tagOf, type OpenApi } from "./api-reference.ts";
import { samples, type SampleRequest } from "./code-samples.ts";

const root = path.join(import.meta.dirname, "..", "..");
const doc = JSON.parse(fs.readFileSync(path.join(root, "site", "generated", "openapi.json"), "utf8")) as OpenApi;

const post: SampleRequest = {
	method: "POST",
	url: "http://127.0.0.1:7878/api/v1/projects",
	query: [],
	body: { name: "Launch teaser", note: "it's \"quoted\"", ok: true, none: null, list: [1, 2] },
	json: true,
};
const get: SampleRequest = {
	method: "GET",
	url: "http://127.0.0.1:7878/api/v1/projects/PROJECT_ID/frame",
	query: [{ name: "at", value: "5s" }],
	json: false,
};

test("every sample sends the bearer secret from the environment", () => {
	for (const request of [post, get]) {
		const list = samples(request);
		assert.deepEqual(
			list.map((s) => s.label),
			["curl", "Python", "JavaScript", "Go", "PHP", "Java", "C#", "Ruby"],
		);
		for (const s of list) {
			assert.match(s.code, /Bearer/, `${s.label} has no bearer header`);
			assert.match(s.code, /VUKEM_VIDEO_KEY/, `${s.label} does not read the secret from the environment`);
			assert.ok(s.code.includes(request.url), `${s.label} lacks the url`);
		}
	}
});

test("a body goes out as JSON with its content type, and a request without one sends none", () => {
	for (const s of samples(post)) if (s.label !== "Python") assert.match(s.code, /application\/json/, s.label);
	for (const s of samples(get)) assert.doesNotMatch(s.code, /Content-Type/, s.label);
});

function run(command: string, args: string[]): { status: number | null; found: boolean } {
	const r = spawnSync(command, args, { encoding: "utf8" });
	return { status: r.status, found: !r.error };
}

test("the JavaScript samples are valid modules", () => {
	const dir = fs.mkdtempSync(path.join(os.tmpdir(), "samples-"));
	try {
		for (const [i, request] of [post, get].entries()) {
			const file = path.join(dir, `s${i}.mjs`);
			fs.writeFileSync(file, samples(request)[2].code);
			assert.equal(run(process.execPath, ["--check", file]).status, 0);
		}
	} finally {
		fs.rmSync(dir, { recursive: true, force: true });
	}
});

test("the Python and Go samples parse when the tools are installed", (t) => {
	const dir = fs.mkdtempSync(path.join(os.tmpdir(), "samples-"));
	try {
		for (const [i, request] of [post, get].entries()) {
			const [, py, , go] = samples(request);
			const pyFile = path.join(dir, `s${i}.py`);
			fs.writeFileSync(pyFile, py.code);
			const python = ["python3", "python"].map((c) => ({ c, r: run(c, ["-m", "py_compile", pyFile]) })).find((x) => x.r.found && x.r.status !== null);
			if (python) assert.equal(python.r.status, 0, "python");
			else t.diagnostic("python is not installed");

			const goFile = path.join(dir, `s${i}.go`);
			fs.writeFileSync(goFile, go.code);
			const gofmt = run("gofmt", ["-e", goFile]);
			if (gofmt.found) assert.equal(gofmt.status, 0, "go");
			else t.diagnostic("gofmt is not installed");
		}
	} finally {
		fs.rmSync(dir, { recursive: true, force: true });
	}
});

test("a route belongs to the group of its resource", () => {
	assert.equal(tagOf("/api/v1/projects"), "Projects");
	assert.equal(tagOf("/api/v1/projects/{id}/media/{asset_id}/peaks"), "Media");
	assert.equal(tagOf("/api/v1/projects/{id}/jobs"), "Jobs");
	assert.equal(tagOf("/api/v1/jobs/{job_id}"), "Jobs");
	assert.equal(tagOf("/api/v1/projects/{id}/events"), "Events");
	assert.equal(tagOf("/api/v1/handshake"), "System");
	assert.equal(tagOf("/api/v1/keys/{key_id}"), "API keys");
	assert.throws(() => tagOf("/api/v1/unknown"));
});

test("every operation of the document has a page, a tag, an id of its own and a scope", () => {
	const ops = operations(doc, routeScopes(root));
	const count = Object.values(doc.paths).reduce((n, methods) => n + Object.keys(methods).length, 0);
	assert.equal(ops.length, count);
	assert.equal(new Set(ops.map((o) => o.id)).size, ops.length);
	for (const op of ops) if (op.id !== "handshake.get" && op.id !== "system.launch") assert.ok(op.scope, `${op.id} has no scope in endpoints.go`);

	const { files, nav } = apiPages(doc, root, new Map());
	for (const op of ops) {
		const text = files.get(`${op.id}.mdx`);
		assert.ok(text, `${op.id} has no page`);
		assert.equal((text.match(/^```\w+ title="(curl|Python|JavaScript|Go|PHP|Java|C#|Ruby)"$/gm) ?? []).length, 8, `${op.id}: eight samples`);
		assert.match(text, /^## Authentication$/m);
	}
	assert.equal(nav.reduce((n, g) => n + g.pages.length, 0), ops.length);
});

test("an example is built from the schema, and a default or an enum wins over a placeholder", () => {
	const schemas = { Thing: { type: "object", required: ["a"], properties: { a: { type: "string", enum: ["x", "y"] }, b: { type: "integer" }, c: { $ref: "#/components/schemas/Thing" } } } };
	assert.deepEqual(exampleOf({ $ref: "#/components/schemas/Thing" }, schemas), { a: "x", b: 0, c: {} });
	assert.equal(exampleOf({ type: ["string", "null"], format: "date-time" }, schemas), "2006-01-02T15:04:05Z");
	assert.equal(exampleOf({ type: "boolean", default: true }, schemas), true);
});
