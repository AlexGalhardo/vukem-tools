import type { ComponentProps } from "react";
import { withBase } from "@/lib/site";
import { withTrailingSlash } from "@/lib/trailing-slash";

/** A plain anchor. The docs site is static HTML, so there is no client router; internal paths get
 * the trailing slash the pages are served at and the base path of the deployment. */
export function Link({ href, ...rest }: ComponentProps<"a">) {
	return <a href={href === undefined ? href : withBase(withTrailingSlash(href))} {...rest} />;
}
