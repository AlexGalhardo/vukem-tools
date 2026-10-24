import { Link, router, usePage } from "@inertiajs/react";
import { useEffect, useRef, useState } from "react";

import CommandPalette from "./CommandPalette";
import Icon, { type IconName } from "./Icon";
import ThemeToggle from "./ThemeToggle";
import { footerPages, mainPages, type Page } from "../lib/nav";
import type { SharedProps, ToolGroup } from "../types";

/**
 * The app shell's navigation: a fixed column on the left from `md` up, and a
 * top bar with a hamburger that slides the same column in from the left on a
 * phone.
 *
 * Top to bottom it is the site name, the search (Ctrl+K), the tool families as
 * dropdowns — the tool a visitor picks opens in the page on the right — and,
 * at the foot, the pages that are not tools, the theme and the account.
 *
 * The column is one element for both sizes, so the two cannot drift apart.
 * Closed on a phone it is `invisible`, which also takes it out of the tab
 * order and the accessibility tree; a transform alone would leave its links
 * focusable off-screen.
 */
export default function Sidebar(): React.ReactElement {
  const { auth, toolGroups = [] } = usePage<SharedProps>().props;
  const url = usePage().url;
  const [open, setOpen] = useState(false);
  const [searching, setSearching] = useState(false);
  const menuButton = useRef<HTMLButtonElement>(null);

  // Any visit closes the drawer, so choosing a destination never leaves the
  // panel covering the page it just loaded.
  useEffect(() => router.on("navigate", () => setOpen(false)), []);

  // Ctrl+K / Cmd+K from anywhere. Taken even inside a text field: a developer
  // tool full of text areas is exactly where the shortcut is wanted.
  useEffect(() => {
    const onKey = (event: KeyboardEvent): void => {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        setSearching((v) => !v);
      }
    };

    document.addEventListener("keydown", onKey);

    return () => document.removeEventListener("keydown", onKey);
  }, []);

  useEffect(() => {
    if (!open) {
      return;
    }

    const onKey = (event: KeyboardEvent): void => {
      if (event.key === "Escape" && !searching) {
        setOpen(false);
        menuButton.current?.focus();
      }
    };

    document.addEventListener("keydown", onKey);
    document.body.style.overflow = "hidden";

    return () => {
      document.removeEventListener("keydown", onKey);
      document.body.style.overflow = "";
    };
  }, [open, searching]);

  const signedIn = Boolean(auth?.user);
  const path = url.split("?")[0] ?? url;
  const current = path.startsWith("/tools/") ? path.slice("/tools/".length) : "";
  const isMac = typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform);

  return (
    <>
      <header className="sticky top-0 z-30 flex h-14 items-center gap-2 border-b border-edge bg-surface/85 px-2 backdrop-blur-sm md:hidden">
        <button
          ref={menuButton}
          type="button"
          aria-label="Open menu"
          aria-expanded={open}
          aria-controls="sidebar"
          onClick={() => setOpen(true)}
          className="flex size-11 items-center justify-center rounded-field text-ink-soft transition-colors duration-100 hover:bg-sunken hover:text-ink"
        >
          <Icon name="menu" className="size-5" />
        </button>
        <Brand />
        <div className="flex-1" />
        <button
          type="button"
          aria-label="Search tools"
          onClick={() => setSearching(true)}
          className="flex size-11 items-center justify-center rounded-field text-ink-soft hover:bg-sunken hover:text-ink"
        >
          <Icon name="search" className="size-5" />
        </button>
      </header>

      {open && (
        <div
          aria-hidden="true"
          onClick={() => setOpen(false)}
          className="fixed inset-0 z-30 bg-ink/40 md:hidden"
        />
      )}

      <aside
        id="sidebar"
        aria-label="Sidebar"
        className={`fixed inset-y-0 left-0 z-40 flex w-64 flex-col border-r border-edge bg-surface
          transition-[transform,visibility] duration-150 md:visible md:translate-x-0
          ${open ? "translate-x-0" : "invisible -translate-x-full"}`}
      >
        <div className="flex h-14 shrink-0 items-center justify-between px-3">
          <Brand />
          <button
            type="button"
            aria-label="Close menu"
            onClick={() => setOpen(false)}
            className="flex size-11 items-center justify-center rounded-field text-ink-soft hover:bg-sunken hover:text-ink md:hidden"
          >
            <Icon name="close" className="size-5" />
          </button>
        </div>

        <div className="px-3 pb-2">
          <button
            type="button"
            onClick={() => {
              setOpen(false);
              setSearching(true);
            }}
            className="flex min-h-11 w-full items-center gap-2 rounded-field border border-edge bg-raised px-3 text-sm text-ink-faint transition-colors duration-100 hover:border-ink-faint hover:text-ink-soft"
          >
            <Icon name="search" className="size-4 shrink-0" />
            <span className="flex-1 text-left">Search</span>
            <kbd className="rounded border border-edge px-1.5 py-0.5 text-[0.6875rem]">
              {isMac ? "⌘" : "Ctrl"} K
            </kbd>
          </button>
        </div>

        <nav aria-label="Main" className="flex-1 overflow-y-auto px-3 py-1">
          <NavLink item={mainPages[0]!} path={path} exact />

          <div className="mt-3 space-y-0.5">
            <p className="label px-3 pb-1">Tools</p>
            {toolGroups.map((group) => (
              <Group key={group.name} group={group} current={current} />
            ))}
          </div>
        </nav>

        <div className="space-y-1 border-t border-edge px-3 py-3">
          {mainPages.slice(1).map((item) => (
            <NavLink key={item.href} item={item} path={path} compact />
          ))}
          {footerPages.map((item) => (
            <NavLink key={item.href} item={item} path={path} compact />
          ))}

          {/* Words on a phone, where a 28px icon is too small to hit; icons on
              a wide screen, where the column's height is better spent on the
              tools. */}
          <div className="px-1 pt-2 md:hidden">
            <ThemeToggle roomy />
          </div>
          <div className="hidden px-3 pt-1 md:block">
            <ThemeToggle />
          </div>

          {signedIn && auth?.user ? (
            <div className="mt-2 flex items-center gap-2 border-t border-edge px-1 pt-3">
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm font-medium text-ink">{auth.user.name}</p>
                <p className="truncate text-xs text-ink-faint">{auth.user.email}</p>
              </div>
              <Link
                href="/logout"
                method="post"
                as="button"
                aria-label="Log out"
                className="flex size-11 shrink-0 items-center justify-center rounded-field text-ink-soft hover:bg-sunken hover:text-ink"
              >
                <Icon name="signout" className="size-5" />
              </Link>
            </div>
          ) : null}

          {!signedIn && auth ? (
            <Link
              href="/login"
              className="mt-2 flex min-h-11 items-center justify-center rounded-field bg-signal px-4 text-sm font-medium text-raised hover:opacity-90 dark:text-surface"
            >
              Log in
            </Link>
          ) : null}
        </div>
      </aside>

      <CommandPalette open={searching} onClose={() => setSearching(false)} groups={toolGroups} />
    </>
  );
}

/** The framework's own mark, the same as its favicon, so the site and the
 *  tab agree. Drawn inline because it keeps its colours in both themes. */
function Mark(): React.ReactElement {
  return (
    <svg viewBox="0 0 32 32" fill="none" aria-hidden="true" className="size-6 shrink-0">
      <rect width="32" height="32" rx="8" fill="#00ADD8" />
      <path d="M6.5 8h5.25l5.5 16h-5.25z" fill="#fff" />
      <path d="M20.25 8h5.25l-5.5 16h-5.25z" fill="#fff" fillOpacity=".72" />
      <path d="M15.25 24h1.75l1.2-3.5-.88-2.55z" fill="#007D9C" />
    </svg>
  );
}

/** Brand is the site's title: just "Tools", on every screen size. */
function Brand(): React.ReactElement {
  return (
    <Link
      href="/"
      aria-label="Tools home"
      className="flex min-h-11 items-center gap-2.5 rounded-field px-2 font-semibold tracking-tight text-ink"
    >
      <Mark />
      Tools
    </Link>
  );
}

/**
 * Group is one family of tools as a dropdown.
 *
 * It opens by itself when the tool on the right belongs to it, so the sidebar
 * always shows where the visitor is; the rest stay closed unless asked, which
 * keeps seven families from becoming fifty links.
 */
function Group({ group, current }: { group: ToolGroup; current: string }): React.ReactElement {
  const holdsCurrent = group.tools.some((t) => t.name === current);
  const [open, setOpen] = useState(holdsCurrent);
  const id = `group-${group.name.toLowerCase().replace(/[^a-z0-9]+/g, "-")}`;

  // Following a link into another family opens that family; closing one is
  // always the visitor's choice.
  useEffect(() => {
    if (holdsCurrent) setOpen(true);
  }, [holdsCurrent]);

  return (
    <div>
      <button
        type="button"
        aria-expanded={open}
        aria-controls={id}
        onClick={() => setOpen(!open)}
        className={`flex min-h-11 w-full items-center gap-3 rounded-field px-3 text-left text-sm transition-colors duration-100 hover:bg-sunken hover:text-ink ${
          holdsCurrent ? "font-medium text-ink" : "text-ink-soft"
        }`}
      >
        <span className="flex-1">{group.name}</span>
        <span className="text-xs text-ink-faint">{group.tools.length}</span>
        <Icon name="chevron" className={`size-4 shrink-0 transition-transform duration-100 ${open ? "" : "-rotate-90"}`} />
      </button>

      {open ? (
        <ul id={id} className="mb-1 ml-3 space-y-0.5 border-l border-edge pl-2">
          {group.tools.map((tool) => {
            const active = tool.name === current;

            return (
              <li key={tool.name}>
                <Link
                  href={`/tools/${tool.name}`}
                  aria-current={active ? "page" : undefined}
                  className={`flex min-h-10 items-center gap-2.5 rounded-field px-2.5 text-sm transition-colors duration-100 hover:bg-sunken hover:text-ink ${
                    active ? "bg-sunken font-medium text-ink" : "text-ink-soft"
                  }`}
                >
                  <Icon name={tool.icon as IconName} className="size-4 shrink-0" />
                  <span className="min-w-0 flex-1 truncate">{tool.title}</span>
                </Link>
              </li>
            );
          })}
        </ul>
      ) : null}
    </div>
  );
}

const linkClass =
  "flex min-h-11 items-center gap-3 rounded-field px-3 text-sm transition-colors duration-100 " +
  "hover:bg-sunken hover:text-ink";

/** NavLink is one destination. A page the app does not render itself is a
 *  plain anchor, because an Inertia visit to it renders an error iframe (see
 *  lib/nav.ts). */
function NavLink({
  item,
  path,
  exact = false,
  compact = false,
}: {
  item: Page;
  path: string;
  exact?: boolean;
  compact?: boolean;
}): React.ReactElement {
  const active = exact || item.href === "/" ? path === item.href : path.startsWith(item.href);
  const className = `${linkClass} ${compact ? "md:min-h-9" : ""} ${active ? "bg-sunken font-medium text-ink" : "text-ink-soft"}`;
  const body = (
    <>
      <Icon name={item.icon as IconName} className="size-4 shrink-0" />
      {item.label}
    </>
  );

  if (item.document) {
    return (
      <a href={item.href} className={className}>
        {body}
      </a>
    );
  }

  return (
    <Link href={item.href} className={className} aria-current={active ? "page" : undefined}>
      {body}
    </Link>
  );
}
