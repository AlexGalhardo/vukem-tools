import { expect, test } from "bun:test";

import { englishDateTime, englishInteger } from "./english";

const sample = "2026-11-18T15:00:00.000Z";

test("a timestamp uses English words in the reader's zone", () => {
  const rendered = englishDateTime(sample);

  expect(rendered).toContain("Nov");
  expect(rendered).not.toContain(" de ");
  expect(rendered).toBe(
    new Date(sample).toLocaleString("en", {
      dateStyle: "medium",
      timeStyle: "short",
    }),
  );
});

test("a missing timestamp is never and an unparseable one is unknown", () => {
  expect(englishDateTime("")).toBe("never");
  expect(englishDateTime(null)).toBe("never");
  expect(englishDateTime("not-a-date")).toBe("unknown");
});

test("an integer uses English grouping", () => {
  expect(englishInteger(44100)).toBe((44100).toLocaleString("en"));
  expect(englishInteger(44100)).toBe("44,100");
});
