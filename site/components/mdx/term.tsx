import { useId } from "react";
import { Link } from "@/components/link";
import { findTerm, termPath } from "@/lib/glossary-data";

/**
 * Inline term with a definition. The tip shows on hover, keyboard focus
 * and tap, and stays available to assistive tech through aria-describedby.
 *
 * `<Term id="keyframe">keyframes</Term>` takes the tip from the glossary and links to the entry. `tip` writes
 * a one-off definition instead.
 */
export function Term({ id, tip, children }: { id?: string; tip?: string; children: React.ReactNode }): React.ReactElement {
	const tipId = useId();
	const entry = id === undefined ? undefined : findTerm(id);
	if (id !== undefined && !entry) throw new Error(`<Term id="${id}">: there is no such term in content/glossary`);
	const text = entry?.tip ?? tip;
	if (text === undefined) throw new Error("<Term> needs an id of the glossary or a tip");

	const body = (
		<>
			{children}
			<span id={tipId} role="tooltip" className="term-tip">
				{text}
			</span>
		</>
	);
	return entry ? (
		<Link href={termPath(entry)} className="term" aria-describedby={tipId}>
			{body}
		</Link>
	) : (
		<span className="term" tabIndex={0} aria-describedby={tipId}>
			{body}
		</span>
	);
}
