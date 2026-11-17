/** The sidebar a page belongs to. Previous and next links stay inside one section. */
export type NavSection = "docs" | "api" | "mcp";

export type NavCategory = {
	dir: string;
	prefix?: string;
	/** The sidebar group. */
	title: string;
	/** The name above the page title and in search results. */
	label?: string;
	section?: NavSection;
	pages: string[];
};
