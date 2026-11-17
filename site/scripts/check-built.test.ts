import assert from "node:assert/strict";
import { test } from "node:test";
import { basePathProblems, foreignOriginProblems, pageProblems, searchProblems, twinProblems } from "./check-built.ts";

const own = "video.vukem.dev";

test("a script, stylesheet, font or image from another origin is refused", () => {
	const html = [
		'<script src="https://cdn.example.com/a.js"></script>',
		'<link rel="stylesheet" href="https://fonts.googleapis.com/css?family=Inter">',
		'<img src="//img.example.com/x.png">',
	].join("");
	// Whole messages, compared for equality: no substring test on a host name.
	assert.deepEqual(foreignOriginProblems("index.html", html, own).sort(), [
		"index.html: loads //img.example.com/x.png",
		"index.html: loads https://cdn.example.com/a.js",
		"index.html: loads https://fonts.googleapis.com/css?family=Inter",
		"index.html: refers to cdn.example.com",
		"index.html: refers to fonts.googleapis.com",
	]);
});

test("own files, inert links and the SVG namespace are accepted", () => {
	const html =
		'<script src="/vukem-video/_astro/a.js"></script><a href="https://github.com/AlexGalhardo/vukem-video">repo</a><svg xmlns="http://www.w3.org/2000/svg"></svg>';
	assert.deepEqual(foreignOriginProblems("index.html", html, own), []);
});

test("the example hosts of the self-hosting pages are text, other example hosts are not", () => {
	const ok = "<p>https://video.example.com/up and http://localhost:7878 and https://caddyserver.com</p>";
	assert.deepEqual(foreignOriginProblems("index.html", ok, own), []);
	assert.deepEqual(foreignOriginProblems("index.html", "<p>https://other.example.com/x</p>", own), [
		"index.html: refers to other.example.com",
	]);
});

test("a remote @import or url() in CSS is refused", () => {
	assert.ok(
		foreignOriginProblems("a.css", '@import url("https://x.example/a.css");', own).some((p) =>
			p.includes("@import"),
		),
	);
	assert.ok(foreignOriginProblems("a.css", "a{background:url(//cdn.example/x.png)}", own).length > 0);
});

test("a root-relative link without the base path is refused", () => {
	assert.deepEqual(
		basePathProblems("a.html", '<a href="/vukem-video/docs/">x</a><img src="/icon.svg">', "/vukem-video"),
		["a.html: /icon.svg does not start with /vukem-video"],
	);
	assert.deepEqual(basePathProblems("a.html", '<a href="/docs/">x</a>', ""), []);
});

test("every page needs a clean twin and a line in llms.txt, and a listed URL must exist", () => {
	const url = "https://example.org/base";
	const llms = [`- [A](${url}/docs/a/): x`, `- [B](${url}/docs/b/): x`, `- [C](${url}/docs/c/): x`].join("\n");
	const files = ["llms.txt", "docs/a/index.html", "docs/a.md", "docs/b/index.html", "docs/d/index.html", "docs/d.md"];
	const read = (rel: string) => (rel === "llms.txt" ? llms : rel === "docs/a.md" ? "# A\n\n<Callout>x</Callout>" : "# D");

	assert.deepEqual(twinProblems(files, read, url).sort(), [
		"docs/a.md: has JSX or an import",
		"docs/b: no Markdown twin (docs/b.md)",
		"llms.txt does not list docs/d",
		"llms.txt lists docs/c, which is not in out/",
		"llms.txt lists docs/c, which is not a page",
	].sort());
});

test("a complete site has no twin problems", () => {
	const url = "https://example.org";
	const files = ["llms.txt", "docs/a/index.html", "docs/a.md"];
	const read = (rel: string) => (rel === "llms.txt" ? `- [A](${url}/docs/a/): x` : "# A");
	assert.deepEqual(twinProblems(files, read, url), []);
});

test("a page that moved is a redirect, not a page, and code in a twin is not JSX", () => {
	const url = "https://example.org";
	const redirect = '<!doctype html><meta http-equiv="refresh" content="0;url=/docs/new/"><link rel="canonical" href="https://example.org/docs/new/">';
	const files = ["llms.txt", "docs/new/index.html", "docs/new.md", "docs/old/index.html"];
	const java = ["# New", "", "```java", "import java.net.URI;", "HttpResponse<String> r;", "```"].join("\n");
	const read = (rel: string) => (rel === "llms.txt" ? `- [New](${url}/docs/new/): x` : rel === "docs/old/index.html" ? redirect : rel === "docs/new.md" ? java : "<html></html>");
	assert.deepEqual(twinProblems(files, read, url), []);
});

const SITE = "https://example.org";

function page(opts: { lang: string; canonical: string; alternates?: [string, string][] }): string {
	const alt = (opts.alternates ?? []).map(([l, p]) => `<link rel="alternate" hreflang="${l}" href="${SITE}/${p}/">`).join("");
	return `<html lang="${opts.lang}"><head><link rel="canonical" href="${SITE}/${opts.canonical}/">${alt}</head></html>`;
}

function site(): { files: string[]; read: (rel: string) => string } {
	const pages: Record<string, string> = {
		"docs/a/index.html": page({ lang: "en", canonical: "docs/a" }),
		"docs/a.md": "# A",
		"404.html": "<p>404</p>",
		"sitemap.xml": `<loc>${SITE}/docs/a/</loc>`,
	};
	return { files: Object.keys(pages), read: (rel) => pages[rel] ?? "" };
}

test("an English page with its own canonical and a sitemap line has no problems", () => {
	const good = site();
	assert.deepEqual(pageProblems(good.files, good.read, SITE), []);
});

test("a wrong canonical, an hreflang alternate, a pt-br sitemap line and a missing 404 are refused", () => {
	const good = site();
	const readWith = (html: string, sitemap = `<loc>${SITE}/docs/a/</loc>`) => (rel: string) =>
		rel === "docs/a/index.html" ? html : rel === "sitemap.xml" ? sitemap : good.read(rel);

	assert.ok(pageProblems(good.files, readWith(page({ lang: "en", canonical: "docs/b" })), SITE).some((p) => p.includes("is not the page itself")));
	assert.ok(
		pageProblems(good.files, readWith(page({ lang: "en", canonical: "docs/a", alternates: [["pt-BR", "pt-br/docs/a"]] })), SITE).some((p) =>
			p.includes("has hreflang alternates"),
		),
	);
	assert.ok(
		pageProblems(good.files, readWith(page({ lang: "en", canonical: "docs/a" }), `<loc>${SITE}/docs/a/</loc><loc>${SITE}/pt-br/docs/a/</loc>`), SITE).some((p) =>
			p.includes("lists a pt-br page"),
		),
	);
	assert.ok(pageProblems(good.files.filter((f) => f !== "404.html"), good.read, SITE).includes("404.html is missing"));
	assert.ok(pageProblems(good.files, readWith(page({ lang: "pt-BR", canonical: "docs/a" })), SITE).some((p) => p.includes("lang is not en")));
});

test("the search index finds the editing tools page for trim and the keyframes page for keyframes", () => {
	const index = [
		{ title: "Trim, split and move clips", href: "/docs/guide/editing-tools", text: "Cut a clip." },
		{ title: "Trim, split and move clips", heading: "Trim a clip", href: "/docs/guide/editing-tools#trim-a-clip", text: "item.trim" },
		{ title: "Animate with keyframes", href: "/docs/guide/keyframes", text: "Animate a value." },
		{ title: "Exporting", href: "/docs/guide/exporting", text: "Trim the range, then keyframes are rendered." },
	];
	assert.deepEqual(searchProblems(() => JSON.stringify(index)), []);
});

test("a query whose first result is on another page, or an unreadable index, is refused", () => {
	const index = [{ title: "Exporting", href: "/docs/guide/exporting", text: "trim split and move clips keyframes" }];
	assert.deepEqual(searchProblems(() => JSON.stringify(index)), [
		'search "trim split and move clips": the first result is /docs/guide/exporting, want /docs/guide/editing-tools',
		'search "keyframes": the first result is /docs/guide/exporting, want /docs/guide/keyframes',
	]);
	assert.deepEqual(searchProblems(() => "not json"), ["search-index.json is missing or is not JSON"]);
});
