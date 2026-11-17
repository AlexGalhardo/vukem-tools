import { evaluate } from "@mdx-js/mdx";
import * as runtime from "react/jsx-runtime";
import { rehypePlugins, remarkPlugins } from "@/lib/mdx-plugins";
import { mdxComponents } from "@/components/mdx";

/** Compiles and renders an MDX body on the server (build time). */
export async function renderMdx(body: string, source: string): Promise<React.ReactElement> {
	try {
		const { default: Content } = await evaluate(body, {
			...runtime,
			development: false,
			remarkPlugins,
			rehypePlugins,
		});
		return <Content components={mdxComponents} />;
	} catch (err) {
		throw new Error(`content/${source}: ${err instanceof Error ? err.message : String(err)}`, { cause: err });
	}
}
