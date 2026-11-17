import assert from "node:assert/strict";
import { test } from "node:test";
import { dockerRunCommand } from "../lib/latest-release.ts";

test("the Docker command sets APP_KEY and starts the compose stack", () => {
	const cmd = dockerRunCommand();
	assert.ok(cmd.includes('APP_KEY="base64:$(openssl rand -base64 32)"'));
	assert.ok(cmd.includes("docker compose up -d --build --wait"));
	assert.equal(cmd.split("\n").length, 2);
});
