import { prerender } from "react-dom/static";
import { ChangelogPage } from "@/components/changelog-page";
import { DocArticle } from "@/components/doc-article";
import type { Doc } from "@/lib/content";

async function markup(node: React.ReactNode): Promise<string> {
	const { prelude } = await prerender(node);
	return new Response(prelude).text();
}

/** The article as HTML. Interactive tabs and copy are plain DOM; the sidebar is an island of the page. */
export function renderDoc(doc: Doc): Promise<string> {
	return markup(<DocArticle doc={doc} />);
}

export function renderChangelog(): Promise<string> {
	return markup(<ChangelogPage />);
}
