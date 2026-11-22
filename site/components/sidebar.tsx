"use client";

import { useEffect, useRef, useState } from "react";
import { Collapsible } from "@base-ui/react/collapsible";
import { Link } from "@/components/link";
import { methodClass } from "@/components/mdx/reference";
import { ChevronRight } from "lucide-react";
import type { NavGroup, NavItem } from "@/lib/content";
import { ui as t } from "@/lib/ui-text";
import { site } from "@/lib/site";

const STORAGE_KEY = "sidebar-open";

function Item({ item, active, onNavigate }: { item: NavItem; active: boolean; onNavigate?: () => void }) {
	return (
		<li>
			<Link
				href={item.href}
				onClick={onNavigate}
				aria-current={active ? "page" : undefined}
				className={`flex items-baseline gap-2 rounded-lg px-3 py-1.5 text-sm transition-colors ${
					active ? "bg-accent-soft font-medium text-accent" : "text-muted hover:bg-hover hover:text-fg"
				}`}
			>
				{item.badge ? (
					<span
						aria-hidden
						className={`inline-block w-11 shrink-0 rounded px-1 py-px text-center font-mono text-[0.625rem] leading-4 font-semibold ${methodClass(item.badge)}`}
					>
						{item.badge}
					</span>
				) : null}
				<span className="min-w-0">{item.title}</span>
			</Link>
		</li>
	);
}

function useBrowserPath(): string {
	const [path, setPath] = useState("");
	useEffect(() => {
		const pathname = window.location.pathname.slice(site.base.length) || "/";
		setPath(pathname.length > 1 ? pathname.replace(/\/$/, "") : pathname);
	}, []);
	return path;
}

// The title, not the label: the groups of the MCP tools share one label.
const keyOf = (group: NavGroup): string => `${group.section}:${group.title}`;

/** The groups the reader opened on earlier pages. A full page load starts the page closed, so they are kept for the tab. */
function readStored(): string[] {
	try {
		const raw = sessionStorage.getItem(STORAGE_KEY);
		const parsed: unknown = raw ? JSON.parse(raw) : [];
		return Array.isArray(parsed) ? parsed.filter((k): k is string => typeof k === "string") : [];
	} catch {
		return [];
	}
}

function writeStored(keys: string[]): void {
	try {
		sessionStorage.setItem(STORAGE_KEY, JSON.stringify(keys));
	} catch {
		// Storage can be blocked; the accordion still works, it only forgets between pages.
	}
}

/**
 * The page list of one section, as an accordion. The group that holds the current page is open and the others
 * are closed; a group the reader opens stays open on the next pages of the tab. Each group header is a button
 * with `aria-expanded`, so the keyboard works (Tab, Enter, Space). The same component is the desktop sidebar and
 * the drawer of the phone layout.
 */
export function Sidebar({
	groups,
	pathname,
	onNavigate,
}: {
	groups: NavGroup[];
	/** Page path without a trailing slash. The drawer reads the browser when this is omitted. */
	pathname?: string;
	onNavigate?: () => void;
}) {
	const live = useBrowserPath();
	// trailingSlash adds a slash in the browser; nav hrefs have none.
	const path = pathname ?? live;
	const nav = useRef<HTMLElement>(null);
	const [open, setOpen] = useState<string[]>(() => groups.filter((g) => g.items.some((i) => i.href === path)).map(keyOf));

	useEffect(() => {
		const known = new Set(groups.map(keyOf));
		const holding = groups.filter((g) => g.items.some((i) => i.href === path)).map(keyOf);
		setOpen((now) => [...new Set([...now, ...holding, ...readStored().filter((k) => known.has(k))])]);
	}, [groups, path]);

	useEffect(() => {
		nav.current?.querySelector('[aria-current="page"]')?.scrollIntoView({ block: "nearest" });
	}, [path]);

	function toggle(key: string, next: boolean): void {
		const keys = next ? [...new Set([...open, key])] : open.filter((k) => k !== key);
		setOpen(keys);
		writeStored(keys);
	}

	return (
		<nav ref={nav} aria-label={t.documentation} className="space-y-1 text-sm">
			{groups.map((group) => {
				const key = keyOf(group);
				return (
					<Collapsible.Root key={key} open={open.includes(key)} onOpenChange={(next) => toggle(key, next)}>
						<Collapsible.Trigger className="group flex w-full cursor-pointer items-center justify-between rounded-lg px-3 py-1.5 text-left text-xs font-semibold tracking-wide text-fg hover:bg-hover focus-visible:outline-2 focus-visible:outline-accent">
							{group.title}
							<ChevronRight
								aria-hidden
								className="size-3.5 shrink-0 text-faint transition-transform group-data-panel-open:rotate-90"
							/>
						</Collapsible.Trigger>
						<Collapsible.Panel keepMounted className="mt-1 mb-2">
							<ul className="space-y-0.5">
								{group.items.map((item) => (
									<Item key={item.href} item={item} active={item.href === path} onNavigate={onNavigate} />
								))}
							</ul>
						</Collapsible.Panel>
					</Collapsible.Root>
				);
			})}
		</nav>
	);
}
