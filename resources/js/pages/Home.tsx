import { Head, Link } from "@inertiajs/react";
import { useDeferredValue, useId, useMemo, useState } from "react";

import Icon, { type IconName } from "../components/Icon";

/** Status mirrors app/catalog.Status. */
type Status = "live" | "planned" | "skipped";

interface Tool {
  name: string;
  title: string;
  summary: string;
  in: string[];
  out: string[];
  group: string;
  icon: string;
  status: Status;
  reason?: string;
  keywords?: string[];
}

interface Group {
  name: string;
  tools: Tool[];
}

interface Props {
  groups: Group[];
  counts: { live: number; planned: number; skipped: number };
}

/** searchText mirrors catalog.Tool.SearchText, so typing a format finds the
 *  tool that reads it and the two cannot disagree about what is searchable. */
function searchText(tool: Tool): string {
  return [tool.name, tool.title, tool.summary, tool.group, ...tool.in, ...tool.out, ...(tool.keywords ?? [])]
    .join(" ")
    .toLowerCase();
}

/**
 * The tool index.
 *
 * It is a list, not a grid of cards. Someone arriving here already has a file
 * and a conversion in mind, so what they need to scan is the formats — which
 * is why those sit inline on every row, and why there is no hero above them.
 * The planned and skipped entries are in the same list rather than hidden:
 * a visitor comparing this against another service has to be able to find the
 * answer, and a tool missing on purpose is information.
 */
export default function Home({ groups, counts }: Props): React.ReactElement {
  const [query, setQuery] = useState("");
  // The list is a few hundred nodes and the filter is synchronous, so a
  // deferred value keeps typing smooth without a debounce that would make the
  // results feel laggy on a fast machine.
  const deferred = useDeferredValue(query);
  const searchId = useId();

  const needle = deferred.trim().toLowerCase();

  const filtered = useMemo(() => {
    if (!needle) {
      return groups;
    }

    const terms = needle.split(/\s+/);

    return groups
      .map((group) => ({
        ...group,
        tools: group.tools.filter((tool) => {
          const haystack = searchText(tool);

          return terms.every((term) => haystack.includes(term));
        }),
      }))
      .filter((group) => group.tools.length > 0);
  }, [groups, needle]);

  const matches = filtered.reduce((n, group) => n + group.tools.length, 0);

  return (
    <>
      <Head title="Convert, compress and trace files" />

      <div className="max-w-[70ch]">
        <h1 className="text-4xl font-semibold tracking-tight">
          Convert, compress and trace files.
        </h1>
        <p className="mt-4 text-lg text-ink-soft">
          Self-hosted and open source, running on its own Go framework. {counts.live} tool
          {counts.live === 1 ? "" : "s"} work today; {counts.planned} more are on the way. Results
          are deleted <strong className="font-semibold text-ink">5 minutes</strong> after they are
          made, so download yours before you walk away.
        </p>
      </div>

      <div className="mt-10">
        <label htmlFor={searchId} className="label">
          Find a tool
        </label>
        <div className="relative mt-2">
          <Icon
            name="search"
            className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-ink-faint"
          />
          <input
            id={searchId}
            type="search"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="png, mp4, compress, svg…"
            autoComplete="off"
            className="w-full rounded-field border border-edge bg-raised py-2.5 pl-9 pr-3 text-ink placeholder:text-ink-faint focus:border-signal focus:outline-none"
          />
        </div>
        {/* Announced, not just rendered: a screen-reader user filtering a list
            needs to hear how many results there are. */}
        <p aria-live="polite" className="mt-2 text-sm text-ink-faint">
          {needle
            ? `${matches} tool${matches === 1 ? "" : "s"} match "${deferred.trim()}"`
            : " "}
        </p>
      </div>

      {filtered.length === 0 ? (
        <div className="mt-8 rounded-panel border border-edge bg-raised px-5 py-8 text-center">
          <p className="font-medium">Nothing matches that.</p>
          <p className="mt-1 text-sm text-ink-soft">
            Try a format instead of a name &mdash; <code className="tabular">webp</code>,{" "}
            <code className="tabular">mp4</code>, <code className="tabular">pdf</code>.
          </p>
        </div>
      ) : (
        <div className="mt-6 space-y-10">
          {filtered.map((group) => (
            <section key={group.name}>
              <h2 className="label border-b border-edge pb-2">{group.name}</h2>
              <ul className="divide-y divide-edge">
                {group.tools.map((tool) => (
                  <li key={tool.name}>
                    <ToolRow tool={tool} />
                  </li>
                ))}
              </ul>
            </section>
          ))}
        </div>
      )}
    </>
  );
}

/** ToolRow is one entry. A live tool is a link; a planned or skipped one is
 *  not, because a link that goes nowhere is worse than plain text. */
function ToolRow({ tool }: { tool: Tool }): React.ReactElement {
  const body = (
    <div className="flex items-start gap-4 py-4">
      <Icon
        name={tool.icon as IconName}
        className={`mt-0.5 size-5 shrink-0 ${tool.status === "live" ? "text-signal" : "text-ink-faint"}`}
      />
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
          <h3 className="font-medium text-ink">{tool.title}</h3>
          <StatusTag status={tool.status} />
        </div>
        <p className="mt-1 text-sm text-ink-soft">{tool.summary}</p>
        {tool.reason && (
          <p className="mt-2 max-w-[70ch] border-l border-edge pl-3 text-sm text-ink-faint">
            {tool.reason}
          </p>
        )}
        <p className="tabular mt-2 text-xs text-ink-faint">
          {tool.in.join(" · ")} <span aria-hidden="true">&rarr;</span>
          <span className="sr-only">to</span> {tool.out.join(" · ")}
        </p>
      </div>
      {tool.status === "live" && (
        <Icon name="trace" className="mt-1 size-4 shrink-0 text-ink-faint" />
      )}
    </div>
  );

  if (tool.status !== "live") {
    return <div className="opacity-80">{body}</div>;
  }

  return (
    <Link
      href={`/tools/${tool.name}`}
      className="-mx-3 block rounded-panel px-3 transition-colors duration-100 hover:bg-raised"
    >
      {body}
    </Link>
  );
}

const tags: Record<Status, { text: string; className: string }> = {
  live: { text: "Ready", className: "border-ok/30 bg-ok-soft text-ok" },
  planned: { text: "Planned", className: "border-edge bg-sunken text-ink-faint" },
  skipped: { text: "Not planned", className: "border-warn/30 bg-warn-soft text-warn" },
};

function StatusTag({ status }: { status: Status }): React.ReactElement {
  const tag = tags[status];

  return (
    <span
      className={`rounded-full border px-2 py-0.5 text-[0.6875rem] font-semibold uppercase tracking-[0.06em] ${tag.className}`}
    >
      {tag.text}
    </span>
  );
}
