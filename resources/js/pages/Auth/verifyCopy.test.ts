import { expect, test } from "bun:test";

import { verifyPrompt } from "./verifyCopy";

const email = "ada@example.com";

test("the first view does not claim a link was sent", () => {
	const copy = verifyPrompt(email, false);

	expect(copy.lead).toContain(email);
	expect(copy.lead).toContain("No link has been sent yet");
	expect(copy.lead).toContain("An API key needs a verified address");
	expect(copy.lead).not.toContain("We sent a link");
	expect(copy.action).toBe("Send a verification link");
	expect(copy.action).not.toContain("again");
});

test("after a resend the page says the link was sent", () => {
	const copy = verifyPrompt(email, true);

	expect(copy.lead).toBe(
		`We sent a link to ${email}. Open it to verify your address.`,
	);
	expect(copy.action).toBe("Send the link again");
});
