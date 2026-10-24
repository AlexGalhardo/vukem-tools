import { router } from "@inertiajs/react";
import { useEffect, useId, useMemo, useRef, useState } from "react";

import Icon, { type IconName } from "./Icon";
import type { ToolGroup } from "../types";

interface Entry {
  key: string;
  title: string;
  hint: string;
  icon: IconName;
  href: string;
  /** document asks for a real navigation, for a page this app does not render. */
  document?: boolean;
  search: string;
}

const pages: Entry[] = [
  { key: "page:index", title: "All tools", hint: "Page", icon: "grid", href: "/", search: "all tools index home list" },
  { key: "page:account", title: "Account", hint: "Page", icon: "user", href: "/account", search: "account profile api keys settings" },
  { key: "page:docs", title: "API reference", hint: "Page", icon: "document", href: "/docs", document: true, search: "api reference docs openapi" },
  { key: "page:licenses", title: "Licences", hint: "Page", icon: "key", href: "/licenses", search: "licences licenses legal" },
];

/**
 * The search over every tool, opened with Ctrl+K (or Cmd+K) from anywhere and
 * from the button in the sidebar.
 *
 * It is a combobox: the input keeps focus and the arrow keys move a highlighted
 * option, so a screen reader announces the option and the keyboard never has to
 * leave the field. Enter opens it; Escape closes it and puts focus back where
 * it was.
 */
export default function CommandPalette({
  open,
  onClose,
  groups,
}: {
  open: boolean;
  onClose: () => void;
  groups: ToolGroup[];
}): React.ReactElement | null {
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  const input = useRef<HTMLInputElement>(null);
  const list = useRef<HTMLUListElement>(null);
  const listId = useId();

  const entries = useMemo<Entry[]>(
    () => [
      ...groups.flatMap((g) =>
        g.tools.map((t) => ({
          key: `tool:${t.name}`,
          title: t.title,
          hint: g.name,
          icon: t.icon as IconName,
          href: `/tools/${t.name}`,
          search: t.search,
        })),
      ),
      ...pages,
    ],
    [groups],
  );

  const results = useMemo(() => {
    const terms = query.trim().toLowerCase().split(/\s+/).filter(Boolean);
    if (terms.length === 0) return entries;

    return entries.filter((e) => terms.every((t) => `${e.title} ${e.search}`.toLowerCase().includes(t)));
  }, [entries, query]);

  useEffect(() => {
    if (open) {
      setQuery("");
      setActive(0);
      // After the dialog is in the DOM.
      requestAnimationFrame(() => input.current?.focus());
    }
  }, [open]);

  useEffect(() => setActive(0), [query]);

  useEffect(() => {
    list.current?.querySelector(`[data-index="${active}"]`)?.scrollIntoView({ block: "nearest" });
  }, [active]);

  if (!open) return null;

  const go = (e: Entry | undefined): void => {
    if (!e) return;
    onClose();
    if (e.document) window.location.assign(e.href);
    else router.visit(e.href);
  };

  const onKey = (event: React.KeyboardEvent): void => {
    if (event.key === "ArrowDown") {
      event.preventDefault();
      setActive((i) => Math.min(i + 1, results.length - 1));
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      setActive((i) => Math.max(i - 1, 0));
    } else if (event.key === "Enter") {
      event.preventDefault();
      go(results[active]);
    } else if (event.key === "Escape") {
      event.preventDefault();
      onClose();
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center px-4 pt-[12vh]" onKeyDown={onKey}>
      <div aria-hidden="true" className="absolute inset-0 bg-ink/40" onClick={onClose} />
      <div
        role="dialog"
        aria-modal="true"
        aria-label="Search tools"
        className="relative w-full max-w-lg overflow-hidden rounded-panel border border-edge bg-raised shadow-lifted"
      >
        <div className="flex items-center gap-3 border-b border-edge px-4">
          <Icon name="search" className="size-4 shrink-0 text-ink-faint" />
          <input
            ref={input}
            role="combobox"
            aria-expanded="true"
            aria-controls={listId}
            aria-activedescendant={results[active] ? `${listId}-${active}` : undefined}
            aria-label="Search tools"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search tools, formats, pages…"
            autoComplete="off"
            spellCheck={false}
            className="min-h-12 w-full bg-transparent py-3 text-ink placeholder:text-ink-faint focus:outline-none"
          />
          <kbd className="hidden shrink-0 rounded border border-edge px-1.5 py-0.5 text-[0.6875rem] text-ink-faint sm:block">
            Esc
          </kbd>
        </div>

        <ul ref={list} id={listId} role="listbox" aria-label="Results" className="max-h-[50vh] overflow-y-auto p-2">
          {results.map((e, i) => (
            <li
              key={e.key}
              id={`${listId}-${i}`}
              data-index={i}
              role="option"
              aria-selected={i === active}
              onMouseMove={() => setActive(i)}
              onClick={() => go(e)}
              className={`flex min-h-11 cursor-pointer items-center gap-3 rounded-field px-3 text-sm ${
                i === active ? "bg-sunken text-ink" : "text-ink-soft"
              }`}
            >
              <Icon name={e.icon} className="size-4 shrink-0" />
              <span className="min-w-0 flex-1 truncate">{e.title}</span>
              <span className="shrink-0 text-xs text-ink-faint">{e.hint}</span>
            </li>
          ))}
          {results.length === 0 ? (
            <li role="presentation" className="px-3 py-6 text-center text-sm text-ink-faint">
              Nothing matches &ldquo;{query.trim()}&rdquo;.
            </li>
          ) : null}
        </ul>
      </div>
    </div>
  );
}
