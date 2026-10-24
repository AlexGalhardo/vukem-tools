import { expect, test } from "bun:test";

import { diffJSON, diffLines } from "./diff";
import { checkToolDefinition, validate } from "./schema";

const schema = {
  type: "object",
  required: ["name", "age"],
  additionalProperties: false,
  properties: {
    name: { type: "string", minLength: 2 },
    age: { type: "integer", minimum: 0 },
    tags: { type: "array", items: { $ref: "#/$defs/tag" } },
  },
  $defs: { tag: { type: "string", pattern: "^[a-z]+$" } },
};

test("a valid instance has no issues", () => {
  expect(validate(schema, { name: "Al", age: 3, tags: ["a"] })).toEqual([]);
});

test("each failure names its path", () => {
  const paths = validate(schema, { name: "A", age: 1.5, tags: ["OK"], extra: 1 }).map((i) => i.path);
  expect(paths.sort()).toEqual(["$.age", "$.extra", "$.name", "$.tags[0]"]);
  expect(validate(schema, {}).map((i) => i.message)).toEqual(["is required", "is required"]);
});

test("oneOf wants exactly one match", () => {
  const s = { oneOf: [{ type: "integer" }, { type: "number" }] };
  expect(validate(s, 1)).toHaveLength(1);
  expect(validate(s, 1.5)).toEqual([]);
});

test("a self-referencing $ref stops instead of hanging", () => {
  expect(() => validate({ $ref: "#" }, 1)).toThrow();
});

test("tool definitions: the three shapes and what is wrong with them", () => {
  const anth = {
    name: "get_weather",
    description: "Get the current weather for a city. Use when asked about weather.",
    input_schema: { type: "object", properties: { city: { type: "string", description: "City" } }, required: ["city"] },
  };
  expect(checkToolDefinition(anth)).toMatchObject({ format: "anthropic", errors: [] });
  const openai = { type: "function", function: { name: "bad name!", parameters: { type: "string" } } };
  const r = checkToolDefinition(openai);
  expect(r.format).toBe("openai");
  expect(r.errors.length).toBe(2);
  expect(checkToolDefinition({ name: "x", input_schema: { type: "object", required: ["a"] } }).errors).toHaveLength(1);
});

test("line diff marks only what changed", () => {
  const d = diffLines("a\nb\nc", "a\nx\nc");
  expect(d.map((l) => l.op)).toEqual(["same", "del", "add", "same"]);
  expect(diffLines("", "a")).toEqual([{ op: "add", text: "a" }]);
  expect(diffLines("a", "a")).toEqual([{ op: "same", text: "a" }]);
});

test("JSON diff ignores key order", () => {
  expect(diffJSON({ a: 1, b: [1, 2] }, { b: [1, 2], a: 1 })).toEqual([]);
  expect(diffJSON({ a: 1, b: 2 }, { a: 2, c: 3 })).toEqual([
    { path: "$.a", kind: "changed", before: 1, after: 2 },
    { path: "$.b", kind: "removed", before: 2 },
    { path: "$.c", kind: "added", after: 3 },
  ]);
});
