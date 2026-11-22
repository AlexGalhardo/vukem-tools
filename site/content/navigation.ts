import fs from "node:fs";
import path from "node:path";
import type { NavCategory } from "./nav-types.ts";

export type { NavCategory, NavSection } from "./nav-types.ts";

type GeneratedNav = Record<"api" | "mcp", { title: string; pages: string[] }[]>;

/** The groups of the API and MCP sidebars, from the OpenAPI snapshot and the tool table. */
const generated = JSON.parse(
	fs.readFileSync(path.join(process.cwd(), "generated", "api-nav.json"), "utf8"),
) as GeneratedNav;

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
		pages: ["overview", "endpoints", "schemas"],
	},
	// One group per resource, one page per operation: written by scripts/generate-api-docs.ts.
	...generated.api.map(
		(g): NavCategory => ({ dir: "api", prefix: "api", title: g.title, label: `API: ${g.title}`, section: "api", pages: g.pages }),
	),
	{
		dir: "mcp",
		prefix: "mcp",
		title: "MCP server",
		section: "mcp",
		pages: ["overview", "connect", "tools"],
	},
	// One group per kind of tool, one page per tool: written by scripts/generate-api-docs.ts.
	...generated.mcp.map(
		(g): NavCategory => ({ dir: "mcp", prefix: "mcp", title: g.title, label: "MCP tools", section: "mcp", pages: g.pages }),
	),
];
