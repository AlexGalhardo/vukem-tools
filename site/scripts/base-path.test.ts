import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import { basePathProblems } from "./check-built.ts";

const siteDir = path.join(import.meta.dirname, "..");
const astroBin = path.join(siteDir, "node_modules", "astro", "bin", "astro.mjs");

function htmlFiles(dir: string): string[] {
	return fs.readdirSync(dir, { withFileTypes: true }).flatMap((e) => {
		const full = path.join(dir, e.name);
		if (e.isDirectory()) return htmlFiles(full);
		return e.name.endsWith(".html") ? [full] : [];
	});
}

/** Builds the site for one SITE_URL into a scratch folder and returns the HTML files found there. */
function build(siteUrl: string): { dir: string; pages: string[] } {
	const dir = fs.mkdtempSync(path.join(os.tmpdir(), "vukem-base-path-"));
	execFileSync(process.execPath, [astroBin, "build", "--outDir", dir], {
		cwd: siteDir,
		env: { ...process.env, SITE_URL: siteUrl },
		stdio: "pipe",
	});
	return { dir, pages: htmlFiles(dir) };
}

function problems(dir: string, pages: string[], base: string): string[] {
	return pages.flatMap((f) =>
		basePathProblems(path.relative(dir, f).split(path.sep).join("/"), fs.readFileSync(f, "utf8"), base),
	);
}

// The production site is on a custom domain (base "/"); a project-site address keeps the base-path code honest.
test("under a base path, every root-relative href and src starts with it (SDD-010 section 14 check 15)", () => {
	const { dir, pages } = build("https://example.org/vukem-video");
	try {
		assert.ok(pages.length > 50, `only ${pages.length} pages were built`);
		assert.deepEqual(problems(dir, pages, "/vukem-video"), []);
	} finally {
		fs.rmSync(dir, { recursive: true, force: true });
	}
});

test("on a custom domain the base is the root and no page carries the project base path", () => {
	const { dir, pages } = build("https://video.vukem.dev");
	try {
		assert.ok(pages.length > 50, `only ${pages.length} pages were built`);
		const leaks = pages.filter((f) => /\b(?:href|src)\s*=\s*"\/vukem-video[/"]/.test(fs.readFileSync(f, "utf8")));
		assert.deepEqual(leaks, []);
		assert.deepEqual(problems(dir, pages, ""), []);
	} finally {
		fs.rmSync(dir, { recursive: true, force: true });
	}
});
