import type { Doc } from "@/lib/content";
import { site } from "@/lib/site";
import { mdxToMarkdown } from "@/lib/text";
import { withTrailingSlash } from "@/lib/trailing-slash";

/** The Markdown twin of a page (SDD-010 section 9). */
export function twinOf(doc: Doc): string {
	return [
		`# ${doc.title}`,
		"",
		`> ${doc.description}`,
		"",
		`Source: ${site.url}${withTrailingSlash(doc.href)}`,
		"",
		mdxToMarkdown(doc.body),
		"",
	].join("\n");
}
