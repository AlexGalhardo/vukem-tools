import type { APIRoute } from "astro";
import { getAllDocs } from "@/lib/content";
import { twinOf } from "@/lib/twin";

export const prerender = true;

export function getStaticPaths() {
	return getAllDocs().map((doc) => ({ params: { slug: doc.slug.join("/") }, props: { href: doc.href } }));
}

export const GET: APIRoute = ({ props }) => {
	const doc = getAllDocs().find((d) => d.href === props.href);
	if (!doc) return new Response("not found", { status: 404 });

	return new Response(twinOf(doc), { headers: { "Content-Type": "text/markdown; charset=utf-8" } });
};
