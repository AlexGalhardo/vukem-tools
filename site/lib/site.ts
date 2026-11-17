/** The site is served from `origin + base`. https://tools.vukem.dev uses a base of `/`. */
const base = import.meta.env.BASE_URL.replace(/\/$/, "");

export const site = {
	name: "Vukem Tools",
	origin: import.meta.env.SITE as string,
	base,
	url: `${import.meta.env.SITE}${base}`,
	repo: "https://github.com/AlexGalhardo/vukem-tools",
	releases: "https://github.com/AlexGalhardo/vukem-tools/releases",
	editBase: "https://github.com/AlexGalhardo/vukem-tools/edit/main/site/content",
} as const;

/** Prefixes an internal path (`/docs/...`, `/icon.svg`) with the base path; anything else is returned as is. */
export function withBase(href: string): string {
	if (!href.startsWith("/") || href.startsWith("//") || base === "") return href;
	return `${base}${href}`;
}
