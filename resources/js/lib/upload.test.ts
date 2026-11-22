import { expect, test } from "bun:test";

import { reasonFromText } from "./upload";

test("a plain-text refusal keeps the server's sentence", () => {
  expect(reasonFromText("That file is not a type this tool can read.\n")).toBe(
    "That file is not a type this tool can read.",
  );
});

test("a problem document gives its detail, then its title", () => {
  expect(reasonFromText(JSON.stringify({ title: "Bad", detail: "Too large." }))).toBe("Too large.");
  expect(reasonFromText(JSON.stringify({ title: "Bad" }))).toBe("Bad");
});

test("HTML, an empty body or a long dump is never shown", () => {
  const fallback = "The server refused the file.";
  expect(reasonFromText("<html><body>oops</body></html>")).toBe(fallback);
  expect(reasonFromText("")).toBe(fallback);
  expect(reasonFromText("x".repeat(400))).toBe(fallback);
  expect(reasonFromText("line one\nline two")).toBe(fallback);
});
