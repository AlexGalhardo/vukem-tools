import type { NavCategory } from "./nav-types.ts";

export type { NavCategory, NavSection } from "./nav-types.ts";

/**
 * Sidebar of the documentation. Each category lists pages in display order.
 * A page is content/docs/en/<dir>/<slug>.mdx and is served at
 * /docs/<prefix or dir>/<slug>. Getting started lives at the root of /docs.
 */
export const navigation: NavCategory[] = [
	{
		dir: "getting-started",
		prefix: "",
		title: "Getting started",
		pages: ["introduction", "installation", "quick-start"],
	},
	{
		dir: "concepts",
		title: "Concepts",
		pages: ["catalogue", "interfaces"],
	},
	{
		dir: "guide",
		title: "Guides",
		pages: ["web", "pdf-crop", "desktop"],
	},
	{
		dir: "agents",
		title: "Agents",
		pages: ["overview", "cli", "mcp"],
	},
	{
		dir: "prompts",
		title: "Prompts",
		pages: ["overview"],
	},
	{
		dir: "self-hosting",
		title: "Self-hosting",
		pages: ["docker", "desktop-downloads", "licences"],
	},
	{
		dir: "reference",
		title: "Reference",
		pages: ["cli", "configuration"],
	},
	{
		dir: "glossary",
		title: "Glossary",
		pages: ["how-to-use"],
	},
	{
		dir: "troubleshooting",
		title: "Troubleshooting",
		pages: ["common-problems", "desktop-window"],
	},
	{
		dir: "contributing",
		title: "Contributing",
		pages: ["guide", "releases"],
	},
	{
		dir: "api",
		prefix: "api",
		title: "API reference",
		section: "api",
		pages: [
			"overview",
			"authentication",
			"workflow",
			"errors",
			"endpoints",
			"account",
			"catalog",
			"conversions",
			"pdf-tools",
			"media-tools",
			"file-tools",
		],
	},
	{
		dir: "mcp",
		prefix: "mcp",
		title: "MCP server",
		section: "mcp",
		pages: ["overview", "connect", "protocol", "tools", "tools-pdf", "tools-image", "tools-media", "tools-archives"],
	},
];
