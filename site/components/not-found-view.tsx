import { ui } from "@/lib/ui-text";
import { withBase } from "@/lib/site";

export function NotFoundView(): React.ReactElement {
	return (
		<main id="content" className="mx-auto max-w-xl px-4 py-24 text-center">
			<p className="text-sm font-medium text-accent">404</p>
			<h1 className="mt-2 text-3xl font-semibold tracking-tight">{ui.notFoundTitle}</h1>
			<p className="mt-3 text-muted">{ui.notFoundBody}</p>
			<a
				href={withBase("/docs/introduction/")}
				className="mt-8 inline-flex h-10 items-center rounded-lg border border-border px-4 text-sm font-medium hover:bg-hover"
			>
				{ui.goToIntroduction}
			</a>
		</main>
	);
}
