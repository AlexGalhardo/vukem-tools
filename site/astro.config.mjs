import path from "node:path";
import { fileURLToPath } from "node:url";
import react from "@astrojs/react";
import tailwindcss from "@tailwindcss/vite";
import { defineConfig } from "astro/config";
import { navigation, pagePath } from "./lib/page-files.ts";

const root = path.dirname(fileURLToPath(import.meta.url));

// SITE_URL is the public address. The default is the custom domain on GitHub Pages, so the base is "/".
// An address with a path (a project site, https://<user>.github.io/<repo>) sets that path as the base
// (SDD-010 section 12.4).
const siteUrl = new URL(process.env.SITE_URL || "https://tools.vukem.dev");
const prefix = siteUrl.pathname.replace(/\/$/, "");
const base = prefix || "/";

// The Portuguese docs were removed (English only): every old /pt-br/docs/... address sends to the English page.
// A dynamic redirect cannot carry the base path, so each page is listed.
const legacyPortuguese = Object.fromEntries(
	navigation.flatMap((cat) =>
		cat.pages.map((page) => {
			const href = pagePath(cat, page);
			return [`/pt-br${href}`, `${prefix}${href}/`];
		}),
	),
);

// Static HTML for GitHub Pages. The docs root opens the introduction.
export default defineConfig({
	site: siteUrl.origin,
	base,
	output: "static",
	outDir: "dist",
	trailingSlash: "always",
	build: { format: "directory" },
	server: { port: 4321 },
	integrations: [react()],
	vite: {
		plugins: [tailwindcss()],
		resolve: {
			alias: { "@": root },
		},
	},
	redirects: {
		// The destination is not rewritten with the base path by Astro, so it is written in full.
		"/docs": `${prefix}/docs/introduction/`,
		"/docs/mcp": `${prefix}/docs/mcp/overview/`,
		"/docs/api": `${prefix}/docs/api/overview/`,
		"/pt-br": `${prefix}/`,
		"/pt-br/docs": `${prefix}/docs/introduction/`,
		"/pt-br/changelog": `${prefix}/changelog/`,
		// The HTTP API moved to its own section (docs/api).
		"/docs/reference/http-api": `${prefix}/docs/api/overview/`,
		...legacyPortuguese,
	},
});
