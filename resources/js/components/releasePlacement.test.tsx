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
const { release } = await import("../release");

const label = `v${release}`;

function between(html: string, start: string, end: string): string {
	const from = html.indexOf(start);
	const to = html.indexOf(end, from);
	expect(from).toBeGreaterThanOrEqual(0);
	expect(to).toBeGreaterThan(from);
	return html.slice(from, to);
}

test("the release label sits in the phone header and under Log in", () => {
	page.props.auth = {};
	const html = renderToStaticMarkup(createElement(Sidebar));
	const header = between(html, "<header", "</header>");
	const aside = between(html, "<aside", "</aside>");

	expect(header.indexOf('aria-label="Search tools"')).toBeGreaterThanOrEqual(0);
	expect(header.indexOf(`>${label}<`)).toBeGreaterThan(header.indexOf('aria-label="Search tools"'));
	expect(aside.indexOf(">Log in<")).toBeGreaterThanOrEqual(0);
	expect(aside.indexOf(`>${label}<`)).toBeGreaterThan(aside.indexOf(">Log in<"));
	expect(html).not.toContain("bottom-3");
	expect(html).not.toContain("left-3");
});

test("the release label sits under the account row when someone is signed in", () => {
	page.props.auth = { user: { name: "QA", email: "qa@example.com" } };
	const html = renderToStaticMarkup(createElement(Sidebar));
	const aside = between(html, "<aside", "</aside>");

	expect(aside.indexOf(">qa@example.com<")).toBeGreaterThanOrEqual(0);
	expect(aside.indexOf(`>${label}<`)).toBeGreaterThan(aside.indexOf(">qa@example.com<"));
	expect(aside).not.toContain(">Log in<");
});

test("the shell does not paint a fixed release overlay", () => {
	page.props.auth = {};
	const html = renderToStaticMarkup(createElement(AppLayout, null, "page"));
	const marks = html.split(label).length - 1;

	expect(marks).toBe(2);
	expect(html).not.toContain("fixed bottom-3 left-3");
	expect(html).toContain("<header");
	expect(html).toContain(">Log in<");
});
