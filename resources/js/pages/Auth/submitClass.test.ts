import { expect, test } from "bun:test";

import { authSubmitClass } from "./submitClass";

test("the auth submit control uses the signal primary", () => {
	expect(authSubmitClass).toContain("bg-signal");
	expect(authSubmitClass).toContain("text-raised");
	expect(authSubmitClass).not.toContain("indigo");
});
