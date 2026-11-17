import { Link } from "@/components/link";
import type { MDXComponents } from "mdx/types";
import { Callout } from "@/components/mdx/callout";
import { Card, CardGroup } from "@/components/mdx/card";
import { CodeBlock } from "@/components/mdx/code-block";
import { Quote } from "@/components/mdx/quote";
import { ApiRoute, RefAside, RefMain, RefSplit } from "@/components/mdx/reference";
import { Step, Steps } from "@/components/mdx/steps";
import { CodeGroup, Tab, Tabs } from "@/components/mdx/tabs";
import { Term } from "@/components/mdx/term";

type HeadingProps = React.ComponentPropsWithoutRef<"h2">;

function anchored(Tag: "h2" | "h3" | "h4") {
	function Heading({ id, children, ...rest }: HeadingProps) {
		return (
			<Tag id={id} {...rest}>
				{children}
				{id ? (
					<a href={`#${id}`} className="heading-anchor" aria-label="Link to this section">
						#
					</a>
				) : null}
			</Tag>
		);
	}
	return Heading;
}

function A({ href = "", children, ...rest }: React.ComponentPropsWithoutRef<"a">) {
	if (href.startsWith("/")) {
		return (
			<Link href={href} {...rest}>
				{children}
			</Link>
		);
	}
	if (href.startsWith("#")) {
		return (
			<a href={href} {...rest}>
				{children}
			</a>
		);
	}
	return (
		<a href={href} target="_blank" rel="noreferrer" {...rest}>
			{children}
		</a>
	);
}

// Plain <img>: the demo GIFs are remote and animated, which next/image would
// not optimize anyway.
function Img({ alt = "", ...rest }: React.ComponentPropsWithoutRef<"img">) {
	return <img alt={alt} loading="lazy" decoding="async" {...rest} />;
}

function Table(props: React.ComponentPropsWithoutRef<"table">) {
	return (
		<div className="table-wrap">
			<table {...props} />
		</div>
	);
}

export const mdxComponents: MDXComponents = {
	h2: anchored("h2"),
	h3: anchored("h3"),
	h4: anchored("h4"),
	a: A,
	img: Img,
	table: Table,
	ApiRoute,
	Callout,
	Card,
	CardGroup,
	CodeBlock,
	CodeGroup,
	Quote,
	RefAside,
	RefMain,
	RefSplit,
	Step,
	Steps,
	Tab,
	Tabs,
	Term,
};
