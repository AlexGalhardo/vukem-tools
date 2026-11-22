import { Link } from "@/components/link";
import { withBase } from "@/lib/site";
import { ui as t } from "@/lib/ui-text";

/** The product logo. The theme is a class on <html>, so CSS picks the file before first paint. */
export function Logo() {
	return (
		<Link href="/" className="flex shrink-0 items-center rounded-md" aria-label={t.logoLabel}>
			<img
				src={withBase("/logo-light-theme.png")}
				alt=""
				width={480}
				height={96}
				className="h-6 w-auto dark:hidden"
			/>
			<img
				src={withBase("/logo-dark-theme.png")}
				alt=""
				width={480}
				height={96}
				className="hidden h-6 w-auto dark:block"
			/>
		</Link>
	);
}
