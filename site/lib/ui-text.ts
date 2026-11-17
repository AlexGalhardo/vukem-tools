/** Interface strings of the site chrome (English only). Client-safe: no file system access here. */
export type UiText = {
	skipToContent: string;
	logoLabel: string;
	home: string;
	docs: string;
	githubTooltip: string;
	githubLabel: string;
	agents: string;
	apiReference: string;
	mcp: string;
	changelog: string;
	changelogTitle: string;
	changelogDescription: string;
	openNavigation: string;
	closeNavigation: string;
	navigation: string;
	documentation: string;
	onThisPage: string;
	releases: string;
	colorTheme: string;
	lightTheme: string;
	darkTheme: string;
	systemTheme: string;
	lastUpdated: string;
	editOnGitHub: string;
	pagination: string;
	previous: string;
	next: string;
	changelogNote: string;
	notFoundTitle: string;
	notFoundBody: string;
	goToIntroduction: string;
	searchDocs: string;
	searchPlaceholder: string;
	searchButton: string;
	noResults: string;
	searchFailed: string;
	copied: string;
};

export const ui: UiText = {
	skipToContent: "Skip to content",
	logoLabel: "Vukem Tools home",
	home: "Home",
	docs: "Docs",
	githubTooltip: "GitHub repository",
	githubLabel: "Vukem Tools on GitHub",
	agents: "Agents",
	apiReference: "API Reference",
	mcp: "MCP",
	changelog: "Changelog",
	changelogTitle: "Changelog",
	changelogDescription: "Every release of Vukem Tools, with a link to the commit behind each change.",
	openNavigation: "Open navigation",
	closeNavigation: "Close navigation",
	navigation: "Navigation",
	documentation: "Documentation",
	onThisPage: "On this page",
	releases: "Releases",
	colorTheme: "Color theme",
	lightTheme: "Light theme",
	darkTheme: "Dark theme",
	systemTheme: "System theme",
	lastUpdated: "Last updated",
	editOnGitHub: "Edit this page on GitHub",
	pagination: "Pagination",
	previous: "Previous",
	next: "Next",
	changelogNote: "Release notes are written in English.",
	notFoundTitle: "Page not found",
	notFoundBody: "The page you are looking for does not exist or has moved.",
	goToIntroduction: "Go to the introduction",
	searchDocs: "Search documentation",
	searchPlaceholder: "Search the docs…",
	searchButton: "Search…",
	noResults: "No results for",
	searchFailed: "Search index could not be loaded.",
	copied: "Copied to clipboard",
};

/** "Last updated" timestamp, printed in UTC. */
export function formatUpdated(iso: string): string {
	return new Intl.DateTimeFormat("en", {
		year: "numeric",
		month: "long",
		day: "numeric",
		hour: "2-digit",
		minute: "2-digit",
		timeZone: "UTC",
		timeZoneName: "short",
	}).format(new Date(iso));
}

/** The title and description of the changelog page. */
export function changelogMetadata(): { title: string; description: string } {
	return { title: ui.changelogTitle, description: ui.changelogDescription };
}
