import { expect, test } from "bun:test";

import { settledNumber, typedNumber } from "./number";

test("typing 24 into a field with a minimum of 4 reaches 24", () => {
  let value = 20;
  for (const draft of ["", "2", "24"]) {
    const accepted = typedNumber(draft, 4, 128);
    if (accepted !== null) value = accepted;
  }
  expect(value).toBe(24);
});

test("a draft outside the range is not passed on and settles into it on blur", () => {
  expect(typedNumber("-5", 0)).toBeNull();
  expect(typedNumber("500", 4, 128)).toBeNull();
  expect(typedNumber("abc", 4, 128)).toBeNull();
  expect(settledNumber("-5", 0)).toBe(0);
  expect(settledNumber("500", 4, 128)).toBe(128);
  expect(settledNumber("", 1, 200)).toBe(1);
  expect(settledNumber("12")).toBe(12);
});
