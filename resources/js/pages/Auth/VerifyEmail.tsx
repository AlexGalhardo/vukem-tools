import { Head, Link, usePage } from "@inertiajs/react";

import type { SharedProps } from "../../types";
import { authSubmitClass } from "./submitClass";
import { verifyPrompt } from "./verifyCopy";

export default function VerifyEmail() {
	const page = usePage<SharedProps>();
	const { auth } = page.props;
	const sent =
		typeof page.flash?.status === "string" && page.flash.status !== "";
	const { lead, action } = verifyPrompt(auth?.user?.email ?? "", sent);

	return (
		<>
			<Head title="Verify your email" />
			<h1 className="mb-2 text-2xl font-semibold">Verify your email</h1>
			<p className="mb-4">{lead}</p>
			<Link
				href="/email/verification-notification"
				method="post"
				as="button"
				className={authSubmitClass}
			>
				{action}
			</Link>
		</>
	);
}
