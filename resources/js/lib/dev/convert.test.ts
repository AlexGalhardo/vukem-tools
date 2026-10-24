import { expect, test } from "bun:test";

import { fromCSV, fromXML, fromYaml, toCSV, toXML, toYaml } from "./convert";
import { formatJSON, parseJSON } from "./json";

const sample = {
  name: "vukem",
  count: 3,
  ok: true,
  none: null,
  tags: ["a", "b c", "true"],
  nested: { list: [{ id: 1, label: "x: y" }, { id: 2, label: "" }], empty: [] as unknown[] },
};

test("YAML round-trips the JSON shapes config files use", () => {
  expect(fromYaml(toYaml(sample))).toEqual(sample);
});

test("YAML reads comments, quotes, flow values and a sequence at its key's indent", () => {
  const text = `# top
name: demo  # trailing
items:
- 1
- "two"
flow: [1, 2]
obj: {"a": 1}
`;
  expect(fromYaml(text)).toEqual({ name: "demo", items: [1, "two"], flow: [1, 2], obj: { a: 1 } });
});

test("YAML refuses what it cannot read faithfully", () => {
  expect(() => fromYaml("a: &x 1\nb: *x")).toThrow();
  expect(() => fromYaml("a: |\n  text")).toThrow();
});

test("CSV quotes commas, quotes and newlines and reads them back", () => {
  const rows = [
    { a: "x,y", b: 'say "hi"', c: "line\nbreak" },
    { a: "1", b: "", c: "z" },
  ];
  expect(fromCSV(toCSV(rows))).toEqual(rows);
});

test("CSV reports an unclosed quote", () => {
  expect(() => fromCSV('a,b\n"1,2')).toThrow("never closed");
});

test("XML round-trips attributes, text and repeated siblings", () => {
  const value = { "@id": "7", item: [{ "#text": "one", "@k": "a" }, "two"], other: "x & y" };
  expect(fromXML(toXML(value, "root"))).toEqual(value);
});

test("XML refuses a DOCTYPE and mismatched tags", () => {
  expect(() => fromXML('<!DOCTYPE a [<!ENTITY x "y">]><a/>')).toThrow();
  expect(() => fromXML("<a><b></a></b>")).toThrow();
});

test("JSON errors carry a line and column", () => {
  const r = parseJSON('{\n  "a": 1,\n  "b": }');
  expect(r.ok).toBe(false);
  if (!r.ok) expect(r.line).toBe(3);
});

test("JSON minifies and indents", () => {
  expect(formatJSON('{ "a": [1, 2] }', 0)).toMatchObject({ ok: true, text: '{"a":[1,2]}' });
  expect(formatJSON('{"a":1}', 2)).toMatchObject({ text: '{\n  "a": 1\n}' });
});
