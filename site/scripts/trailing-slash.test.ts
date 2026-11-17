import assert from "node:assert/strict";
import { test } from "node:test";
import { withTrailingSlash } from "../lib/trailing-slash.ts";

test("internal paths get the trailing slash, keeping query and hash", () => {
	assert.equal(withTrailingSlash("/changelog"), "/changelog/");
	assert.equal(withTrailingSlash("/docs/basics/routing"), "/docs/basics/routing/");
	assert.equal(withTrailingSlash("/docs/basics/routing#groups"), "/docs/basics/routing/#groups");
	assert.equal(withTrailingSlash("/docs?q=orm"), "/docs/?q=orm");
	assert.equal(withTrailingSlash("/docs/api/bitmaps.get"), "/docs/api/bitmaps.get/");
	assert.equal(withTrailingSlash("/docs/mcp/tools/edit/item.split#args"), "/docs/mcp/tools/edit/item.split/#args");
});

test("everything else is left alone", () => {
	for (const href of [
		"/",
		"/docs/",
		"/docs/#a",
		"#groups",
		"https://vukem.dev/docs",
		"//cdn.example/x",
		"/icon.svg",
		"/llms.txt",
		"/docs/api/bitmaps.get.md",
		"/updates/feed.json",
		"mailto:a@b.c",
		"",
	]) {
		assert.equal(withTrailingSlash(href), href);
	}
});
