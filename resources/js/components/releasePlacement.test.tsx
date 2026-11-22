import { expect, mock, test } from "bun:test";
import { createElement, type ReactNode } from "react";
import { renderToStaticMarkup } from "react-dom/server";

interface Account {
	user?: { name: string; email: string };
}

const page: {
	url: string;
	props: { auth: Account; toolGroups: [] };
	flash: Record<string, string>;
} = {
	url: "/",
	props: { auth: {}, toolGroups: [] },
	flash: {},
};

mock.module("@inertiajs/react", () => ({
	usePage: () => page,
	router: { on: () => () => undefined },
	Link: ({
		children,
		href,
		className,
	}: {
		children?: ReactNode;
		href?: string;
		className?: string;
	}) => createElement("a", { href, className }, children),
}));

const { default: AppLayout } = await import("../layouts/AppLayout");
const { default: Sidebar } = await import("./Sidebar");
const { default: ReleaseLabel } = await import("./ReleaseLabel");
const { release } = await import("../release");

const label = `v${release}`;

test("the release label is fixed in the bottom-right corner and takes no clicks", () => {
	const html = renderToStaticMarkup(createElement(ReleaseLabel));

	expect(html).toContain(`>${label}<`);
	expect(html).toContain("fixed");
	expect(html).toContain("right-0");
	expect(html).toContain("bottom-0");
	expect(html).toContain("pointer-events-none");
	expect(html).toContain("print:hidden");
});

test("the label keeps clear of a home bar and a notch", () => {
	const html = renderToStaticMarkup(createElement(ReleaseLabel));

	expect(html).toContain("env(safe-area-inset-bottom");
	expect(html).toContain("env(safe-area-inset-right");
});

test("a screen reader hears the word release before the version", () => {
	const html = renderToStaticMarkup(createElement(ReleaseLabel));

	expect(html).toContain('<span class="sr-only">Release </span>');
});

test("every page shows the label once, signed in or not", () => {
	const accounts: Account[] = [{}, { user: { name: "QA", email: "qa@example.com" } }];

	for (const auth of accounts) {
		page.props.auth = auth;
		const html = renderToStaticMarkup(createElement(AppLayout, null, "page"));

		expect(html.split(label).length - 1).toBe(1);
		expect(html).toContain("data-release-label");
	}
});

test("the sidebar no longer carries a copy of the release", () => {
	page.props.auth = {};
	const html = renderToStaticMarkup(createElement(Sidebar));

	expect(html).not.toContain(label);
});

test("the label sits below the menu, the drawer and the search", () => {
	const html = renderToStaticMarkup(createElement(ReleaseLabel));

	// The sticky header and the drawer backdrop are z-30, the drawer z-40 and the search z-50.
	expect(html).toContain("z-20");
});
