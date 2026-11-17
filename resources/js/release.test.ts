import { expect, test } from "bun:test";
import pkg from "../../package.json" with { type: "json" };

import { release } from "./release";

test("the screen release is the package version", () => {
  expect(release).toBe(pkg.version);
  expect(release).toMatch(/^\d+\.\d+\.\d+$/);
});
