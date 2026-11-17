import type { APIRoute } from "astro";
import { getAllDocs } from "@/lib/content";
import { site } from "@/lib/site";

export const prerender = true;

function escapeXml(value: string): string {
	return value
		.replace(/&/g, "&amp;")
		.replace(/</g, "&lt;")
		.replace(/>/g, "&gt;")
		.replace(/"/g, "&quot;")
		.replace(/'/g, "&apos;");
}

type Entry = { path: string; lastmod?: string; priority: string };

const at = (path: string) => `${site.url}${path.endsWith("/") ? path : `${path}/`}`;

/** Every HTML page: the landing page, the docs and the changelog. */
export const GET: APIRoute = () => {
	const entries: Entry[] = [{ path: "/", priority: "1.0" }];

	for (const doc of getAllDocs()) {
		entries.push({ path: doc.href, lastmod: doc.updated, priority: "0.8" });
	}

	entries.push({ path: "/changelog/", priority: "0.5" });

	const body = [
		`<?xml version="1.0" encoding="UTF-8"?>`,
		`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`,
		...entries.map((e) => {
			const lastmod = e.lastmod ? `<lastmod>${escapeXml(e.lastmod)}</lastmod>` : "";
			return `<url><loc>${escapeXml(at(e.path))}</loc>${lastmod}<changefreq>weekly</changefreq><priority>${e.priority}</priority></url>`;
		}),
		`</urlset>`,
	].join("");

	return new Response(body, { headers: { "Content-Type": "application/xml; charset=utf-8" } });
};
