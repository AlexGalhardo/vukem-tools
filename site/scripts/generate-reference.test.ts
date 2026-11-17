import assert from "node:assert/strict";
import { test } from "node:test";
import { esc, serviceStatuses } from "./generate-reference.ts";

test("braces, angle brackets and pipes cannot become MDX syntax or end a table cell", () => {
	assert.equal(esc("a {b} <c> | d"), String.raw`a \{b\} &lt;c&gt; \| d`);
});

test("the service status table is read from service/error.go", () => {
	const status = serviceStatuses();
	assert.equal(status.get("E_UNAUTHORIZED"), 401);
	assert.equal(status.get("E_WORKSPACE_LOCKED"), 503);
	assert.equal(status.get("E_QUEUE_FULL"), 429);
	assert.equal(status.get("E_LIMIT_EXCEEDED"), 422);
	assert.equal(status.get("E_CLIENT_CLOSED"), 499);
	assert.equal(status.get("E_EXPORT_FAILED"), undefined, "a job failure has no entry: it answers 500");
});
