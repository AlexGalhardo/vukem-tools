/** Copy for the verify-email page. Registration does not send mail. */
export function verifyPrompt(
	email: string,
	alreadySent: boolean,
): { lead: string; action: string } {
	if (alreadySent) {
		return {
			lead: `We sent a link to ${email}. Open it to verify your address.`,
			action: "Send the link again",
		};
	}

	return {
		lead: `No link has been sent yet to ${email}. An API key needs a verified address.`,
		action: "Send a verification link",
	};
}
