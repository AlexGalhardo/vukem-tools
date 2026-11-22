/**
 * Layout of the API and MCP reference pages. On a wide screen the text is on the left and the samples on the
 * right, which stay in view while the text scrolls; on a phone the two stack.
 */
export function RefSplit({ children }: { children: React.ReactNode }): React.ReactElement {
	return <div className="grid grid-cols-[minmax(0,1fr)] gap-6 2xl:grid-cols-[minmax(0,1fr)_minmax(0,28rem)] xl:grid-cols-[minmax(0,1fr)_minmax(0,24rem)]">{children}</div>;
}

export function RefMain({ children }: { children: React.ReactNode }): React.ReactElement {
	return <div className="prose min-w-0">{children}</div>;
}

export function RefAside({ children }: { children: React.ReactNode }): React.ReactElement {
	return (
		<div className="prose min-w-0 max-xl:order-first xl:sticky xl:top-[calc(var(--header-h)+1rem)] xl:max-h-[calc(100dvh-var(--header-h)-2rem)] xl:self-start xl:overflow-y-auto xl:pr-1 scroll-slim">
			{children}
		</div>
	);
}

const METHOD_STYLES: Record<string, string> = {
	GET: "bg-emerald-100 text-emerald-800 dark:bg-emerald-950/60 dark:text-emerald-300",
	POST: "bg-sky-100 text-sky-800 dark:bg-sky-950/60 dark:text-sky-300",
	PUT: "bg-amber-100 text-amber-800 dark:bg-amber-950/60 dark:text-amber-300",
	PATCH: "bg-violet-100 text-violet-800 dark:bg-violet-950/60 dark:text-violet-300",
	DELETE: "bg-red-100 text-red-800 dark:bg-red-950/60 dark:text-red-300",
};

export function methodClass(method: string): string {
	return METHOD_STYLES[method] ?? "bg-subtle text-muted";
}

/** The method and the path of an operation, as one line. */
export function ApiRoute({ method, path }: { method: string; path: string }): React.ReactElement {
	return (
		<div className="flex flex-wrap items-center gap-3 rounded-xl border border-border bg-subtle px-4 py-3 font-mono text-sm">
			<span className={`rounded-md px-2 py-0.5 text-xs font-semibold ${methodClass(method)}`}>{method}</span>
			<span className="min-w-0 break-all text-fg">{path}</span>
		</div>
	);
}
