import { expect, test } from "bun:test";

import { diffJSON, showValue } from "./diff";
import { formatJSON, parseJSON, parseJSONExact } from "./json";

const bs = String.fromCharCode(92);

test("formatting keeps a number exactly as written", () => {
  const out = formatJSON('{"id":12345678901234567890,"price":1.10,"e":1E+3,"z":-0}', 2);
  expect(out).toMatchObject({ ok: true });
  expect(out.text).toBe(
    '{\n  "id": 12345678901234567890,\n  "price": 1.10,\n  "e": 1E+3,\n  "z": -0\n}',
  );
});

test("formatting keeps a string's escapes as written", () => {
  const text = `{"k":"a${bs}u00e9${bs}n${bs}"q"}`;
  expect(formatJSON(text, 0).text).toBe(text);
});

test("sorting keys keeps numbers and orders nested objects", () => {
  const out = formatJSON('{"b":{"y":1.0,"x":2},"a":[{"d":1,"c":2}]}', 0, true);
  expect(out.text).toBe('{"a":[{"c":2,"d":1}],"b":{"x":2,"y":1.0}}');
});

test("empty containers stay compact when indented", () => {
  expect(formatJSON('{"a":[],"b":{}}', 2).text).toBe('{\n  "a": [],\n  "b": {}\n}');
});

test("an error points at the offending character, not the end of the text", () => {
  const text = '{"a":1,\n "b":}\n' + "x".repeat(200);
  const r = parseJSON(text);
  expect(r.ok).toBe(false);
  if (!r.ok) {
    expect(r.line).toBe(2);
    expect(r.column).toBe(6);
  }
});

test("trailing commas, single quotes, leading zeros and trailing text are errors", () => {
  for (const bad of ['{"a":1,}', "{'a':1}", "[01]", '{"a":1} x', "[1,", '"abc', "", "nul"]) {
    expect(parseJSON(bad).ok).toBe(false);
  }
});

test("valid documents of every shape parse", () => {
  const unicode = `"é${bs}u00e9${bs}n"`;
  for (const good of ["0", unicode, "null", "[]", "{}", ' [1, -2.5e-3, true, false, null, {"k": []}] ']) {
    expect(parseJSON(good).ok).toBe(true);
  }
});

test("a very deep nest is an error, not a crash", () => {
  expect(parseJSON("[".repeat(100000)).ok).toBe(false);
});

test("a JSON diff tells apart integers that differ beyond 2^53", () => {
  const a = parseJSONExact('{"id":12345678901234567890,"n":1}');
  const b = parseJSONExact('{"id":12345678901234567891,"n":1}');
  if (!a.ok || !b.ok) throw new Error("both documents are valid");
  expect(diffJSON(a.value, b.value)).toEqual([
    { path: "$.id", kind: "changed", before: 12345678901234567890n, after: 12345678901234567891n },
  ]);
  expect(showValue(12345678901234567890n)).toBe("12345678901234567890");
  expect(showValue({ k: [1n, "x", null] })).toBe('{"k":[1,"x",null]}');
});
